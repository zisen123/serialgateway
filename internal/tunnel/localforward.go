package tunnel

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/zisen123/serialgateway/internal/config"
)

// Tunnel is the common lifecycle shared by reverse and local-forward tunnels,
// so main.go can collect and stop them uniformly.
type Tunnel interface {
	Start()
	Stop()
}

// LocalForward is the SSH -L direction: it listens on a local address on the
// gateway machine and forwards each accepted connection, over one SSH
// connection to cfg.Host, to a target dialed from the SSH server's side. This
// is the mirror of ReverseTunnel.
//
// Field mapping for kind: "local_forward":
//
//	bind_address:local_port  -> local listen address on the gateway machine
//	target_host:remote_port  -> address dialed on the far (server) side
//
// e.g. reach a server-only daemon on 127.0.0.1:19825 from this machine's
// localhost:19825 (bind_address 127.0.0.1, local_port 19825, target_host
// 127.0.0.1, remote_port 19825).
type LocalForward struct {
	cfg    *config.ReverseTunnelConfig
	logger *slog.Logger
	stopCh chan struct{}
	once   sync.Once
	wg     sync.WaitGroup
}

func NewLocalForward(cfg *config.ReverseTunnelConfig, logger *slog.Logger) *LocalForward {
	return &LocalForward{
		cfg:    cfg,
		logger: logger.With("component", "tunnel", "kind", "local_forward"),
		stopCh: make(chan struct{}),
	}
}

func (t *LocalForward) Start() {
	t.wg.Add(1)
	go t.run()
}

func (t *LocalForward) Stop() {
	t.once.Do(func() {
		close(t.stopCh)
	})
	t.wg.Wait()
}

func (t *LocalForward) run() {
	defer t.wg.Done()

	interval := 1 * time.Second
	const maxInterval = 30 * time.Second

	for {
		err := t.serve()
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

func (t *LocalForward) serve() error {
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

	bindAddr := t.cfg.BindAddress
	if bindAddr == "" {
		bindAddr = "127.0.0.1"
	}
	localAddr := net.JoinHostPort(bindAddr, fmt.Sprintf("%d", t.cfg.LocalPort))
	ln, err := net.Listen("tcp", localAddr)
	if err != nil {
		return fmt.Errorf("local listen on %s: %w", localAddr, err)
	}
	defer ln.Close()

	targetHost := t.cfg.TargetHost
	if targetHost == "" {
		targetHost = "127.0.0.1"
	}
	targetAddr := net.JoinHostPort(targetHost, fmt.Sprintf("%d", t.cfg.RemotePort))

	t.logger.Info("connected", "addr", addr)
	t.logger.Info("forwarding", "local", localAddr, "target", targetAddr, "ssh_host", addr)

	// Detect half-dead TCP connections: without keepalives a silently-dropped
	// link leaves the tunnel "connected" forever while every forward is dead.
	go keepAliveMonitor(client, t.stopCh, t.logger, func(err error) {
		t.logger.Warn("ssh keepalive failed, connection likely dead", "error", err)
	})

	// shutdown closes the local listener exactly once so Accept unblocks, both
	// on Stop() and when the SSH connection drops.
	var shutdownOnce sync.Once
	shutdown := func() { shutdownOnce.Do(func() { _ = ln.Close() }) }

	// Tear down when asked to stop.
	go func() {
		<-t.stopCh
		shutdown()
	}()

	// Reconnect when the SSH connection drops underneath us.
	sshClosed := make(chan error, 1)
	go func() {
		sshClosed <- client.Wait()
		shutdown()
	}()

	for {
		local, aerr := ln.Accept()
		if aerr != nil {
			// Distinguish deliberate stop from an SSH-drop-triggered close.
			select {
			case <-t.stopCh:
				t.logger.Info("stopping")
				return nil
			default:
			}
			select {
			case werr := <-sshClosed:
				return fmt.Errorf("ssh connection closed: %w", werr)
			default:
				return fmt.Errorf("local accept on %s: %w", localAddr, aerr)
			}
		}
		t.wg.Add(1)
		go func(local net.Conn) {
			defer t.wg.Done()
			t.handle(client, local, targetAddr)
		}(local)
	}
}

func (t *LocalForward) handle(client *gossh.Client, local net.Conn, targetAddr string) {
	defer local.Close()

	remote, err := client.Dial("tcp", targetAddr)
	if err != nil {
		t.logger.Warn("dial target failed", "target", targetAddr, "error", err)
		return
	}
	defer remote.Close()

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(remote, local); done <- struct{}{} }()
	go func() { _, _ = io.Copy(local, remote); done <- struct{}{} }()
	<-done
}
