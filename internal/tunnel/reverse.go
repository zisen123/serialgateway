package tunnel

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/zisen123/serialgateway/internal/config"
	gatewayssh "github.com/zisen123/serialgateway/internal/ssh"
)

type ReverseTunnel struct {
	cfg    *config.ReverseTunnelConfig
	sshCfg *config.SSHConfig
	logger *slog.Logger
	stopCh chan struct{}
	once   sync.Once
	wg     sync.WaitGroup
}

func New(cfg *config.ReverseTunnelConfig, sshCfg *config.SSHConfig, logger *slog.Logger) *ReverseTunnel {
	return &ReverseTunnel{
		cfg:    cfg,
		sshCfg: sshCfg,
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

	auths := []gossh.AuthMethod{}
	if t.cfg.PrivateKeyFile != "" {
		signer, err := loadPrivateKey(t.cfg.PrivateKeyFile)
		if err != nil {
			return fmt.Errorf("load private key: %w", err)
		}
		auths = append(auths, gossh.PublicKeys(signer))
	}
	if t.cfg.Password != "" {
		auths = append(auths, gossh.Password(t.cfg.Password))
	}
	if len(auths) == 0 {
		return fmt.Errorf("no authentication method configured (set password or private_key_file)")
	}

	clientCfg := &gossh.ClientConfig{
		User:            t.cfg.User,
		Auth:            auths,
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	client, err := gossh.Dial("tcp", addr, clientCfg)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer client.Close()

	t.logger.Info("connected", "addr", addr)

	if err := t.killRemotePortHolder(client); err != nil {
		t.logger.Warn("failed to clear remote port", "port", t.cfg.RemotePort, "error", err)
	}

	remoteAddr := fmt.Sprintf("%s:%d", t.cfg.BindAddress, t.cfg.RemotePort)
	listener, err := client.Listen("tcp", remoteAddr)
	if err != nil {
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

	t.logger.Info("forwarding", "remote", remoteAddr, "mode", t.cfg.Mode, "target_host", t.cfg.TargetHost, "target_port", t.cfg.LocalPort)

	var proxy *ProxyBridge
	if t.cfg.Mode == "ssh_proxy" {
		if t.sshCfg == nil || t.sshCfg.Auth.Type != "password" {
			return fmt.Errorf("ssh_proxy mode currently requires gateway ssh.auth.type=password")
		}
		hostKeyPEM, err := gatewayssh.LoadOrGenerateHostKeyPEM("host_key", t.logger)
		if err != nil {
			return fmt.Errorf("load gateway host key: %w", err)
		}
		proxy = NewProxyBridge(t.cfg, t.sshCfg, hostKeyPEM, t.logger)
	}

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
				if proxy != nil {
					proxy.Serve(remote)
					return
				}
				t.forward(remote)
			}()
		}
	}
}

func (t *ReverseTunnel) killRemotePortHolder(client *gossh.Client) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	port := fmt.Sprintf("%d", t.cfg.RemotePort)
	findCmd := fmt.Sprintf(`ss -tlnp 2>/dev/null | grep ':%s ' | grep -oP 'pid=\\K[0-9]+' | sort -u`, port)

	out, err := session.CombinedOutput(findCmd)
	if err != nil {
		return fmt.Errorf("find remote port holder: %w", err)
	}

	pids := strings.Fields(string(out))
	if len(pids) == 0 {
		return nil
	}

	for _, pid := range pids {
		t.logger.Info("killing residual remote process", "port", port, "pid", pid)
		killSession, err := client.NewSession()
		if err != nil {
			t.logger.Warn("open session to kill failed", "pid", pid, "error", err)
			continue
		}
		_ = killSession.Run(fmt.Sprintf("kill %s 2>/dev/null || true", pid))
		killSession.Close()

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

func (t *ReverseTunnel) targetAddr() string {
	return net.JoinHostPort(t.cfg.TargetHost, strconv.Itoa(t.cfg.LocalPort))
}

func (t *ReverseTunnel) forward(remote net.Conn) {
	defer remote.Close()

	targetAddr := t.targetAddr()
	local, err := net.DialTimeout("tcp", targetAddr, 5*time.Second)
	if err != nil {
		t.logger.Error("dial target failed", "addr", targetAddr, "error", err)
		return
	}
	defer local.Close()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(local, remote)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(remote, local)
		done <- struct{}{}
	}()

	<-done
}

func loadPrivateKey(path string) (gossh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	return gossh.ParsePrivateKey(data)
}
