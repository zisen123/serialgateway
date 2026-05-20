package ssh

import (
	"testing"

	"github.com/zisen123/serialgateway/internal/config"
)

func TestSSHPortMapping(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	mapping := PortMapping("COM3", cfg)
	if mapping != 2203 {
		t.Errorf("expected COM3->2203, got %d", mapping)
	}
	mapping8 := PortMapping("COM8", cfg)
	if mapping8 != 2208 {
		t.Errorf("expected COM8->2208, got %d", mapping8)
	}
}

func TestNewSSHServer(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	srv, err := NewSSHServer("COM3", cfg)
	if err != nil {
		t.Fatalf("NewSSHServer failed: %v", err)
	}
	if srv.Port() != 2203 {
		t.Errorf("expected port 2203, got %d", srv.Port())
	}
}