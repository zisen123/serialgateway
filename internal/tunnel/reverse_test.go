package tunnel

import (
	"log/slog"
	"testing"

	"github.com/zisen123/serialgateway/internal/config"
)

func TestReverseTunnelTargetAddr(t *testing.T) {
	sshCfg := &config.SSHConfig{}

	t.Run("default localhost target", func(t *testing.T) {
		cfg := &config.ReverseTunnelConfig{
			TargetHost: "127.0.0.1",
			LocalPort:  18080,
		}
		rt := New(cfg, sshCfg, slog.Default())

		if got := rt.targetAddr(); got != "127.0.0.1:18080" {
			t.Fatalf("targetAddr() = %q, want %q", got, "127.0.0.1:18080")
		}
	})

	t.Run("custom LAN target", func(t *testing.T) {
		cfg := &config.ReverseTunnelConfig{
			TargetHost: "192.168.1.10",
			LocalPort:  22,
		}
		rt := New(cfg, sshCfg, slog.Default())

		if got := rt.targetAddr(); got != "192.168.1.10:22" {
			t.Fatalf("targetAddr() = %q, want %q", got, "192.168.1.10:22")
		}
	})
}

func TestNewProxyBridge(t *testing.T) {
	cfg := &config.ReverseTunnelConfig{
		Mode:           "ssh_proxy",
		TargetHost:     "127.0.0.1",
		LocalPort:      22,
		TargetUser:     "root",
		TargetPassword: "secret",
	}
	sshCfg := &config.SSHConfig{}
	bridge := NewProxyBridge(cfg, sshCfg, []byte("pem"), slog.Default())
	if bridge == nil {
		t.Fatal("expected proxy bridge")
	}
	if bridge.cfg != cfg {
		t.Fatal("expected bridge to keep config pointer")
	}
	if bridge.sshCfg != sshCfg {
		t.Fatal("expected bridge to keep ssh config pointer")
	}
}
