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

// ForwardSpec describes one remote listener -> local target forwarding rule.
type ForwardSpec struct {
	Label      string // e.g. "COM3", for logging only
	RemotePort int    // port to bind on the remote server (BindAddress:RemotePort)
	TargetHost string // host to dial from the gateway side
	TargetPort int    // port to dial from the gateway side
}

type ReverseTunnel struct {
	cfg      *config.ReverseTunnelConfig
	sshCfg   *config.SSHConfig
	forwards []ForwardSpec
	logger   *slog.Logger
	stopCh   chan struct{}
	once     sync.Once
	wg       sync.WaitGroup
}

// New builds a single-forward tunnel from cfg, preserving the legacy behavior of
// reverse_tunnel / adb_reverse_tunnel (one remote_port -> target_host:local_port).
func New(cfg *config.ReverseTunnelConfig, sshCfg *config.SSHConfig, logger *slog.Logger) *ReverseTunnel {
	return &ReverseTunnel{
		cfg:    cfg,
		sshCfg: sshCfg,
		forwards: []ForwardSpec{{
			RemotePort: cfg.RemotePort,
			TargetHost: cfg.TargetHost,
			TargetPort: cfg.LocalPort,
		}},
		logger: logger.With("component", "tunnel"),
		stopCh: make(chan struct{}),
	}
}

// NewMulti builds a tunnel that opens N remote listeners over one SSH connection.
// cfg supplies only the connection fields (host/port/user/auth/bind_address);
// forwards supplies the per-port remote/target mapping.
func NewMulti(cfg *config.ReverseTunnelConfig, sshCfg *config.SSHConfig, forwards []ForwardSpec, logger *slog.Logger) *ReverseTunnel {
	return &ReverseTunnel{
		cfg:      cfg,
		sshCfg:   sshCfg,
		forwards: forwards,
		logger:   logger.With("component", "tunnel"),
		stopCh:   make(chan struct{}),
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

	t.logger.Info("connected", "addr", addr, "forwards", len(t.forwards))

	// Clear residual holders on all remote ports before opening any listener.
	if err := t.killRemotePortHolders(client, t.remotePorts()); err != nil {
		t.logger.Warn("failed to clear one or more remote ports", "error", err)
	}

	// ssh_proxy is only valid for the single-forward legacy path (reverse_tunnel).
	var proxy *ProxyBridge
	if len(t.forwards) == 1 && t.cfg.Mode == "ssh_proxy" {
		if t.sshCfg == nil || t.sshCfg.Auth.Type != "password" {
			return fmt.Errorf("ssh_proxy mode currently requires gateway ssh.auth.type=password")
		}
		hostKeyPEM, err := gatewayssh.LoadOrGenerateHostKeyPEM("host_key", t.logger)
		if err != nil {
			return fmt.Errorf("load gateway host key: %w", err)
		}
		proxy = NewProxyBridge(t.cfg, t.sshCfg, hostKeyPEM, t.logger)
	}

	// Open one remote listener per forward spec, all over the same client.
	type listenerWithSpec struct {
		ln   net.Listener
		spec ForwardSpec
	}
	var listeners []listenerWithSpec
	defer func() {
		for _, l := range listeners {
			_ = l.ln.Close()
		}
	}()

	for _, f := range t.forwards {
		remoteAddr := fmt.Sprintf("%s:%d", t.cfg.BindAddress, f.RemotePort)
		ln, lerr := client.Listen("tcp", remoteAddr)
		if lerr != nil {
			t.logger.Warn("listen failed, retrying after clearing port", "port", f.RemotePort, "label", f.Label, "error", lerr)
			time.Sleep(500 * time.Millisecond)
			_ = t.killRemotePortHolders(client, []int{f.RemotePort})
			time.Sleep(500 * time.Millisecond)
			ln, lerr = client.Listen("tcp", remoteAddr)
			if lerr != nil {
				return fmt.Errorf("remote listen on %s: %w", remoteAddr, lerr)
			}
		}
		t.logger.Info("forwarding", "remote", remoteAddr, "mode", t.cfg.Mode, "target_host", f.TargetHost, "target_port", f.TargetPort, "label", f.Label)
		listeners = append(listeners, listenerWithSpec{ln: ln, spec: f})
	}

	type acceptedConn struct {
		conn net.Conn
		spec ForwardSpec
	}
	acceptCh := make(chan acceptedConn, 8)
	// errCh signals that a listener died so the whole tunnel reconnects.
	errCh := make(chan error, len(listeners))

	for _, l := range listeners {
		l := l
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			for {
				remote, aerr := l.ln.Accept()
				if aerr != nil {
					select {
					case <-t.stopCh:
					default:
						t.logger.Error("accept error", "port", l.spec.RemotePort, "label", l.spec.Label, "error", aerr)
						errCh <- fmt.Errorf("listener %s (:%d) closed: %w", l.spec.Label, l.spec.RemotePort, aerr)
					}
					return
				}
				select {
				case acceptCh <- acceptedConn{conn: remote, spec: l.spec}:
				case <-t.stopCh:
					remote.Close()
					return
				}
			}
		}()
	}

	for {
		select {
		case <-t.stopCh:
			t.logger.Info("stopping")
			return nil
		case err := <-errCh:
			// Any listener failure tears down the whole tunnel and reconnects.
			return fmt.Errorf("remote listener failed: %w", err)
		case ac := <-acceptCh:
			t.wg.Add(1)
			go func(ac acceptedConn) {
				defer t.wg.Done()
				if proxy != nil {
					proxy.Serve(ac.conn)
					return
				}
				t.forward(ac.conn, ac.spec)
			}(ac)
		}
	}
}

// remotePorts returns the remote listen ports across all forwards.
func (t *ReverseTunnel) remotePorts() []int {
	out := make([]int, 0, len(t.forwards))
	for _, f := range t.forwards {
		out = append(out, f.RemotePort)
	}
	return out
}

// killRemotePortHolders clears residual processes holding each given remote port
// on the SSH server. A failure on one port is logged and skipped, not fatal.
func (t *ReverseTunnel) killRemotePortHolders(client *gossh.Client, ports []int) error {
	var firstErr error
	for _, p := range ports {
		port := fmt.Sprintf("%d", p)
		findCmd := fmt.Sprintf(`ss -tlnp 2>/dev/null | grep ':%s ' | grep -oP 'pid=\\K[0-9]+' | sort -u`, port)

		session, err := client.NewSession()
		if err != nil {
			t.logger.Warn("open session to find port holder failed", "port", port, "error", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out, err := session.CombinedOutput(findCmd)
		session.Close()
		if err != nil {
			t.logger.Warn("find remote port holder failed", "port", port, "error", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		for _, pid := range strings.Fields(string(out)) {
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
	}

	return firstErr
}

func (t *ReverseTunnel) forward(remote net.Conn, spec ForwardSpec) {
	defer remote.Close()

	targetAddr := net.JoinHostPort(spec.TargetHost, strconv.Itoa(spec.TargetPort))
	local, err := net.DialTimeout("tcp", targetAddr, 5*time.Second)
	if err != nil {
		t.logger.Error("dial target failed", "addr", targetAddr, "label", spec.Label, "error", err)
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
