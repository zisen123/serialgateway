package tunnel

import (
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	gliderssh "github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/zisen123/serialgateway/internal/config"
)

type ProxyBridge struct {
	cfg        *config.ReverseTunnelConfig
	sshCfg     *config.SSHConfig
	hostKeyPEM []byte
	logger     *slog.Logger
}

func NewProxyBridge(cfg *config.ReverseTunnelConfig, sshCfg *config.SSHConfig, hostKeyPEM []byte, logger *slog.Logger) *ProxyBridge {
	return &ProxyBridge{
		cfg:        cfg,
		sshCfg:     sshCfg,
		hostKeyPEM: hostKeyPEM,
		logger:     logger.With("mode", "ssh_proxy"),
	}
}

func (b *ProxyBridge) Serve(conn net.Conn) {
	defer conn.Close()

	srv := &gliderssh.Server{}
	if err := srv.SetOption(gliderssh.HostKeyPEM(b.hostKeyPEM)); err != nil {
		b.logger.Error("set proxy host key failed", "error", err)
		return
	}
	if b.sshCfg == nil || b.sshCfg.Auth.Type != "password" {
		b.logger.Error("unsupported inbound ssh auth for proxy mode", "auth_type", func() string {
			if b.sshCfg == nil {
				return ""
			}
			return b.sshCfg.Auth.Type
		}())
		return
	}
	password := b.sshCfg.Auth.Password
	srv.PasswordHandler = func(ctx gliderssh.Context, supplied string) bool {
		return supplied == password
	}
	srv.Handler = b.bridgeSession
	// HandleConn (unlike Serve) does not call the unexported ensureHandlers, so
	// ChannelHandlers stays nil and every channel is rejected with
	// "unsupported channel type". Register the default session handler explicitly.
	srv.ChannelHandlers = map[string]gliderssh.ChannelHandler{
		"session": gliderssh.DefaultSessionHandler,
	}
	srv.HandleConn(conn)
}

func (b *ProxyBridge) bridgeSession(in gliderssh.Session) {
	logf := func(level slog.Level, msg string, args ...any) {
		allArgs := append([]any{"remote_addr", in.RemoteAddr().String()}, args...)
		b.logger.Log(nil, level, msg, allArgs...)
	}

	if in.Subsystem() != "" {
		fmt.Fprintf(in, "[unsupported subsystem: %s]\n", in.Subsystem())
		logf(slog.LevelWarn, "unsupported subsystem", "subsystem", in.Subsystem())
		_ = in.Exit(1)
		return
	}

	client, err := b.dialTarget()
	if err != nil {
		fmt.Fprintf(in, "[target ssh dial failed: %v]\n", err)
		logf(slog.LevelError, "target ssh dial failed", "error", err)
		_ = in.Exit(1)
		return
	}
	defer client.Close()

	out, err := client.NewSession()
	if err != nil {
		fmt.Fprintf(in, "[target session failed: %v]\n", err)
		logf(slog.LevelError, "target session failed", "error", err)
		_ = in.Exit(1)
		return
	}
	defer out.Close()

	out.Stdin = in
	out.Stdout = in
	out.Stderr = in.Stderr()

	if pty, winCh, ok := in.Pty(); ok {
		if err := out.RequestPty(pty.Term, pty.Window.Height, pty.Window.Width, gossh.TerminalModes{}); err != nil {
			fmt.Fprintf(in, "[target pty failed: %v]\n", err)
			logf(slog.LevelError, "target pty failed", "error", err)
			_ = in.Exit(1)
			return
		}
		go func() {
			for {
				select {
				case <-in.Context().Done():
					return
				case win, ok := <-winCh:
					if !ok {
						return
					}
					_ = out.WindowChange(win.Height, win.Width)
				}
			}
		}()
	}

	sigCh := make(chan gliderssh.Signal, 8)
	in.Signals(sigCh)
	defer in.Signals(nil)
	go func() {
		for {
			select {
			case <-in.Context().Done():
				return
			case sig, ok := <-sigCh:
				if !ok {
					return
				}
				if sig == "" {
					continue
				}
				_ = out.Signal(gossh.Signal(sig))
			}
		}
	}()

	if in.RawCommand() == "" {
		if err := out.Shell(); err != nil {
			fmt.Fprintf(in, "[target shell failed: %v]\n", err)
			logf(slog.LevelError, "target shell failed", "error", err)
			_ = in.Exit(1)
			return
		}
		err = out.Wait()
	} else {
		err = out.Run(in.RawCommand())
	}

	if err == nil {
		_ = in.Exit(0)
		return
	}
	if exitErr, ok := err.(*gossh.ExitError); ok {
		_ = in.Exit(exitErr.ExitStatus())
		return
	}
	if _, ok := err.(*gossh.ExitMissingError); ok {
		_ = in.Exit(1)
		return
	}
	fmt.Fprintf(in, "[target session error: %v]\n", err)
	logf(slog.LevelWarn, "target session error", "error", err)
	_ = in.Exit(1)
}

func (b *ProxyBridge) dialTarget() (*gossh.Client, error) {
	auths := make([]gossh.AuthMethod, 0, 2)
	if b.cfg.TargetPrivateKey != "" {
		signer, err := loadPrivateKey(b.cfg.TargetPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("load target private key: %w", err)
		}
		auths = append(auths, gossh.PublicKeys(signer))
	}
	if b.cfg.TargetPassword != "" {
		auths = append(auths, gossh.Password(b.cfg.TargetPassword))
	}
	if len(auths) == 0 {
		return nil, fmt.Errorf("no target authentication method configured")
	}
	if b.cfg.TargetUser == "" {
		return nil, fmt.Errorf("target_user is required in ssh_proxy mode")
	}

	clientCfg := &gossh.ClientConfig{
		User:            b.cfg.TargetUser,
		Auth:            auths,
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	return gossh.Dial("tcp", net.JoinHostPort(b.cfg.TargetHost, strconv.Itoa(b.cfg.LocalPort)), clientCfg)
}
