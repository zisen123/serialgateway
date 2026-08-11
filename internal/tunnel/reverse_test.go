package tunnel

import (
	"log/slog"
	"testing"

	"github.com/zisen123/serialgateway/internal/config"
)

func TestNewSingleForwardBackwardCompat(t *testing.T) {
	sshCfg := &config.SSHConfig{}

	t.Run("default localhost target", func(t *testing.T) {
		cfg := &config.ReverseTunnelConfig{
			RemotePort: 18080,
			TargetHost: "127.0.0.1",
			LocalPort:  18080,
		}
		rt := New(cfg, sshCfg, slog.Default())

		if len(rt.forwards) != 1 {
			t.Fatalf("len(forwards) = %d, want 1", len(rt.forwards))
		}
		f := rt.forwards[0]
		if f.RemotePort != 18080 || f.TargetHost != "127.0.0.1" || f.TargetPort != 18080 {
			t.Fatalf("forward = %+v, want {RemotePort:18080 TargetHost:127.0.0.1 TargetPort:18080}", f)
		}
	})

	t.Run("custom LAN target", func(t *testing.T) {
		cfg := &config.ReverseTunnelConfig{
			RemotePort: 10022,
			TargetHost: "192.168.1.10",
			LocalPort:  22,
		}
		rt := New(cfg, sshCfg, slog.Default())

		if len(rt.forwards) != 1 {
			t.Fatalf("len(forwards) = %d, want 1", len(rt.forwards))
		}
		f := rt.forwards[0]
		if f.RemotePort != 10022 || f.TargetHost != "192.168.1.10" || f.TargetPort != 22 {
			t.Fatalf("forward = %+v", f)
		}
	})
}

func TestNewMultiForwards(t *testing.T) {
	cfg := &config.ReverseTunnelConfig{Host: "h", Port: 22}
	fs := []ForwardSpec{
		{Label: "COM3", RemotePort: 2203, TargetHost: "127.0.0.1", TargetPort: 2203},
		{Label: "COM25", RemotePort: 2225, TargetHost: "127.0.0.1", TargetPort: 2225},
	}
	rt := NewMulti(cfg, &config.SSHConfig{}, fs, slog.Default())

	if len(rt.forwards) != 2 {
		t.Fatalf("len(forwards) = %d, want 2", len(rt.forwards))
	}
	if rt.forwards[0].RemotePort != 2203 || rt.forwards[1].Label != "COM25" {
		t.Fatalf("forwards = %+v", rt.forwards)
	}
}

func TestRemotePorts(t *testing.T) {
	rt := NewMulti(&config.ReverseTunnelConfig{}, &config.SSHConfig{},
		[]ForwardSpec{{RemotePort: 2203}, {RemotePort: 2225}}, slog.Default())

	got := rt.remotePorts()
	want := []int{2203, 2225}
	if len(got) != len(want) {
		t.Fatalf("remotePorts() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("remotePorts() = %v, want %v", got, want)
		}
	}
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
