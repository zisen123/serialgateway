package ssh

import (
	"testing"

	"github.com/zisen123/serialgateway/internal/config"
	"github.com/zisen123/serialgateway/internal/serial"
)

func TestSSHPortMapping(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	if got := PortMapping("COM3", cfg); got != 2203 {
		t.Errorf("expected COM3->2203, got %d", got)
	}
	if got := PortMapping("COM8", cfg); got != 2208 {
		t.Errorf("expected COM8->2208, got %d", got)
	}
}

func TestNewSSHServer(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	sess := serial.NewSerialSession("COM3", cfg)
	srv := NewSSHServer("COM3", PortMapping("COM3", cfg), cfg, sess)
	if srv.Port() != 2203 {
		t.Errorf("expected port 2203, got %d", srv.Port())
	}
}

func TestADBPortMapping(t *testing.T) {
	adbCfg := &config.AdbConfig{BasePort: 2300}
	cfg := &config.Config{ADB: adbCfg}
	config.ApplyDefaults(cfg)

	port := ADBPortMapping("ce1617181a3b3b0d03", cfg)
	if port < 2300 || port >= 3300 {
		t.Errorf("expected port in [2300, 3299], got %d", port)
	}
	// Deterministic: same serial always maps to same port
	port2 := ADBPortMapping("ce1617181a3b3b0d03", cfg)
	if port != port2 {
		t.Errorf("expected deterministic mapping, got %d and %d", port, port2)
	}
	// Different serials should map differently (with high probability)
	portOther := ADBPortMapping("zzz999differentserial", cfg)
	if portOther < 2300 || portOther >= 3300 {
		t.Errorf("expected port in [2300, 3299], got %d", portOther)
	}
}
