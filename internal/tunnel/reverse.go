package tunnel

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/zisen123/serialgateway/internal/config"
)

type ReverseTunnel struct {
	cfg    *config.ReverseTunnelConfig
	stopCh chan struct{}
	once   sync.Once
	wg     sync.WaitGroup
}

func New(cfg *config.ReverseTunnelConfig) *ReverseTunnel {
	return &ReverseTunnel{
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}
}

func (t *ReverseTunnel) Start() {
	t.wg.Add(1)
	go t.run()
}

func (t *ReverseTunnel) Stop() {
	t.once.Do(func() {
		close(t.stopCh)
	})
	t.wg.Wait()
}

func (t *ReverseTunnel) run() {
	defer t.wg.Done()

	interval := 1 * time.Second
	const maxInterval = 30 * time.Second

	for {
		err := t.connect()
		if err == nil {
			// clean shutdown via stopCh
			return
		}

		select {
		case <-t.stopCh:
			return
		default:
			log.Printf("reverse tunnel: reconnect error: %v (retrying in %v)", err, interval)
			time.Sleep(interval)
			interval *= 2
			if interval > maxInterval {
				interval = maxInterval
			}
		}
	}
}

func (t *ReverseTunnel) connect() error {
	addr := net.JoinHostPort(t.cfg.Host, fmt.Sprintf("%d", t.cfg.Port))

	auths := []ssh.AuthMethod{}

	if t.cfg.PrivateKeyFile != "" {
		signer, err := loadPrivateKey(t.cfg.PrivateKeyFile)
		if err != nil {
			return fmt.Errorf("load private key: %w", err)
		}
		auths = append(auths, ssh.PublicKeys(signer))
	}

	if t.cfg.Password != "" {
		auths = append(auths, ssh.Password(t.cfg.Password))
	}

	if len(auths) == 0 {
		return fmt.Errorf("no authentication method configured (set password or private_key_file)")
	}

	config := &ssh.ClientConfig{
		User:            t.cfg.User,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer client.Close()

	log.Printf("reverse tunnel: connected to %s", addr)

	remoteAddr := fmt.Sprintf("0.0.0.0:%d", t.cfg.RemotePort)
	listener, err := client.Listen("tcp", remoteAddr)
	if err != nil {
		return fmt.Errorf("remote listen on %s: %w", remoteAddr, err)
	}
	defer listener.Close()

	log.Printf("reverse tunnel: forwarding %s -> localhost:%d", remoteAddr, t.cfg.LocalPort)

	acceptCh := make(chan net.Conn, 8)
	go func() {
		for {
			remote, err := listener.Accept()
			if err != nil {
				select {
				case <-t.stopCh:
				default:
					log.Printf("reverse tunnel: accept error: %v", err)
				}
				close(acceptCh)
				return
			}
			acceptCh <- remote
		}
	}()

	for {
		select {
		case <-t.stopCh:
			log.Printf("reverse tunnel: stopping")
			return nil
		case remote, ok := <-acceptCh:
			if !ok {
				return fmt.Errorf("listener closed unexpectedly")
			}
			t.wg.Add(1)
			go func() {
				defer t.wg.Done()
				t.forward(remote)
			}()
		}
	}
}

func (t *ReverseTunnel) forward(remote net.Conn) {
	defer remote.Close()

	localAddr := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", t.cfg.LocalPort))
	local, err := net.DialTimeout("tcp", localAddr, 5*time.Second)
	if err != nil {
		log.Printf("reverse tunnel: dial local %s: %v", localAddr, err)
		return
	}
	defer local.Close()

	done := make(chan struct{}, 2)
	go func() {
		io.Copy(local, remote)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(remote, local)
		done <- struct{}{}
	}()

	<-done
}

func loadPrivateKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	return ssh.ParsePrivateKey(data)
}