package tunnel

import (
	"log/slog"
	"testing"

	"github.com/zisen123/serialgateway/internal/config"
)

func TestReverseTunnelTargetAddr(t *testing.T) {
	t.Run("default localhost target", func(t *testing.T) {
		cfg := &config.ReverseTunnelConfig{
			TargetHost: "127.0.0.1",
			LocalPort:  18080,
		}
		rt := New(cfg, slog.Default())

		if got := rt.targetAddr(); got != "127.0.0.1:18080" {
			t.Fatalf("targetAddr() = %q, want %q", got, "127.0.0.1:18080")
		}
	})

	t.Run("custom LAN target", func(t *testing.T) {
		cfg := &config.ReverseTunnelConfig{
			TargetHost: "192.168.1.10",
			LocalPort:  22,
		}
		rt := New(cfg, slog.Default())

		if got := rt.targetAddr(); got != "192.168.1.10:22" {
			t.Fatalf("targetAddr() = %q, want %q", got, "192.168.1.10:22")
		}
	})
}
