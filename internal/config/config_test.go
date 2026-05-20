package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("nonexistent.yaml")
	if err != nil {
		t.Fatalf("Load should not fail for nonexistent file: %v", err)
	}
	if cfg.Gateway.HTTPPort != 8080 {
		t.Errorf("expected default HTTPPort 8080, got %d", cfg.Gateway.HTTPPort)
	}
	if cfg.SerialDefaults.Baudrate != 115200 {
		t.Errorf("expected default baudrate 115200, got %d", cfg.SerialDefaults.Baudrate)
	}
	if cfg.SSH.BasePort != 2200 {
		t.Errorf("expected default SSH base port 2200, got %d", cfg.SSH.BasePort)
	}
	if cfg.RingBuffer.MaxLines != 100000 {
		t.Errorf("expected default ring buffer max lines 100000, got %d", cfg.RingBuffer.MaxLines)
	}
	if cfg.Reconnect.MaxInterval != 30*time.Second {
		t.Errorf("expected default reconnect max interval 30s, got %v", cfg.Reconnect.MaxInterval)
	}
}

func TestLoadFromFile(t *testing.T) {
	content := `
gateway:
  http_port: 9000
serial_defaults:
  baudrate: 9600
ssh:
  base_port: 3000
  auth:
    type: "password"
    password: "testpw"
ring_buffer:
  max_lines: 100
`
	tmp, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	tmp.WriteString(content)
	tmp.Close()

	cfg, err := Load(tmp.Name())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Gateway.HTTPPort != 9000 {
		t.Errorf("expected HTTPPort 9000, got %d", cfg.Gateway.HTTPPort)
	}
	if cfg.SerialDefaults.Baudrate != 9600 {
		t.Errorf("expected baudrate 9600, got %d", cfg.SerialDefaults.Baudrate)
	}
	if cfg.SSH.Auth.Password != "testpw" {
		t.Errorf("expected password testpw, got %s", cfg.SSH.Auth.Password)
	}
	if cfg.RingBuffer.MaxLines != 100 {
		t.Errorf("expected max lines 100, got %d", cfg.RingBuffer.MaxLines)
	}
}