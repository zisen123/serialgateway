package tunnel

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/zisen123/serialgateway/internal/config"
)

type ReverseTunnel struct {
	cfg    *config.ReverseTunnelConfig
	logger *slog.Logger
	stopCh chan struct{}
	once   sync.Once
	wg     sync.WaitGroup
}

func New(cfg *config.ReverseTunnelConfig, logger *slog.Logger) *ReverseTunnel {
	return &ReverseTunnel{
		cfg:    cfg,
		logger: logger.With("component", "tunnel"),
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
			return
		}

		select {
		case <-t.stopCh:
			return
		default:
			t.logger.Error("reconnect failed", "error", err, "retry_interval", interval)
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

	t.logger.Info("connected", "addr", addr)

	// Pre-emptively kill any residual process holding the remote port
	if err := t.killRemotePortHolder(client); err != nil {
		t.logger.Warn("failed to clear remote port", "port", t.cfg.RemotePort, "error", err)
	}

	remoteAddr := fmt.Sprintf("%s:%d", t.cfg.BindAddress, t.cfg.RemotePort)
	listener, err := client.Listen("tcp", remoteAddr)
	if err != nil {
		// Port still occupied — try killing once more and retry
		t.logger.Warn("listen failed, retrying after clearing port", "error", err)
		time.Sleep(500 * time.Millisecond)
		_ = t.killRemotePortHolder(client)
		time.Sleep(500 * time.Millisecond)
		listener, err = client.Listen("tcp", remoteAddr)
		if err != nil {
			return fmt.Errorf("remote listen on %s: %w", remoteAddr, err)
		}
	}
	defer listener.Close()

	t.logger.Info("forwarding", "remote", remoteAddr, "local_port", t.cfg.LocalPort)

	acceptCh := make(chan net.Conn, 8)
	go func() {
		for {
			remote, err := listener.Accept()
			if err != nil {
				select {
				case <-t.stopCh:
				default:
					t.logger.Error("accept error", "error", err)
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
			t.logger.Info("stopping")
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

// killRemotePortHolder runs a command on the remote to kill any process
// listening on the configured remote port.  It is a best-effort operation:
// if no process is found or the command fails the error is returned but
// the caller may choose to ignore it.
func (t *ReverseTunnel) killRemotePortHolder(client *ssh.Client) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	port := fmt.Sprintf("%d", t.cfg.RemotePort)

	// 1. Find PIDs whose listening sockets include :<port>
	//    ss -tlnp outputs lines like:
	//      LISTEN 0 128  0.0.0.0:18999 0.0.0.0:*  users:(("ssh",pid=123,fd=5))
	//    We extract PIDs from the users column using portable awk (mawk/gawk).
	findCmd := fmt.Sprintf(
		`ss -tlnp 2>/dev/null | grep ':%s ' | grep -oP 'pid=\K[0-9]+' | sort -u`,
		port,
	)

	out, err := session.CombinedOutput(findCmd)
	if err != nil {
		return fmt.Errorf("find remote port holder: %w", err)
	}

	pids := strings.Fields(string(out))
	if len(pids) == 0 {
		return nil // port is free
	}

	for _, pid := range pids {
		t.logger.Info("killing residual remote process", "port", port, "pid", pid)
		// Try graceful kill first, then SIGKILL as fallback
		killSession, err := client.NewSession()
		if err != nil {
			t.logger.Warn("open session to kill failed", "pid", pid, "error", err)
			continue
		}
		_ = killSession.Run(fmt.Sprintf("kill %s 2>/dev/null || true", pid))
		killSession.Close()

		// Wait briefly and force-kill if still alive
		time.Sleep(200 * time.Millisecond)
		killSession, err = client.NewSession()
		if err != nil {
			continue
		}
		_ = killSession.Run(fmt.Sprintf("kill -0 %s 2>/dev/null && kill -9 %s 2>/dev/null || true", pid, pid))
		killSession.Close()
	}

	return nil
}

func (t *ReverseTunnel) forward(remote net.Conn) {
	defer remote.Close()

	localAddr := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", t.cfg.LocalPort))
	local, err := net.DialTimeout("tcp", localAddr, 5*time.Second)
	if err != nil {
		t.logger.Error("dial local failed", "addr", localAddr, "error", err)
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