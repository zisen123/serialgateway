package core

import (
	"testing"

	"github.com/yicongwu/serialgateway/internal/config"
)

func TestNewPortManager(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	pm := NewPortManager(cfg)
	if pm == nil {
		t.Fatal("PortManager should not be nil")
	}
}

func TestStartupConfiguredPorts(t *testing.T) {
	cfg := &config.Config{
		Ports: []config.PortConfig{
			{Device: "COM3", Baudrate: 115200},
			{Device: "COM4", Baudrate: 9600},
		},
	}
	config.ApplyDefaults(cfg)
	pm := NewPortManager(cfg)
	mappings := pm.Mappings()
	t.Logf("mappings: %v", mappings)
}

func TestAutoStart(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	pm := NewPortManager(cfg)
	err := pm.AutoStart()
	if err != nil {
		t.Logf("AutoStart returned error (expected if no ports available): %v", err)
	}
}