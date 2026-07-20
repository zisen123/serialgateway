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

func TestApplyDefaultsSetsReverseTunnelTargetHost(t *testing.T) {
	cfg := &Config{}
	cfg.ReverseTunnel.Host = "example.com"
	cfg.AdbReverseTunnel.Host = "example.com"

	ApplyDefaults(cfg)

	if cfg.ReverseTunnel.TargetHost != "127.0.0.1" {
		t.Errorf("expected reverse tunnel target host 127.0.0.1, got %q", cfg.ReverseTunnel.TargetHost)
	}
	if cfg.ReverseTunnel.Mode != "tcp" {
		t.Errorf("expected reverse tunnel mode tcp, got %q", cfg.ReverseTunnel.Mode)
	}
	if cfg.AdbReverseTunnel.TargetHost != "127.0.0.1" {
		t.Errorf("expected ADB reverse tunnel target host 127.0.0.1, got %q", cfg.AdbReverseTunnel.TargetHost)
	}
	if cfg.AdbReverseTunnel.Mode != "tcp" {
		t.Errorf("expected ADB reverse tunnel mode tcp, got %q", cfg.AdbReverseTunnel.Mode)
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
reverse_tunnel:
  host: "relay.example.com"
  target_host: "192.168.1.10"
  local_port: 22
  mode: "ssh_proxy"
  target_user: "root"
  target_password: "boardpw"
`
	tmp, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}

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
	if cfg.ReverseTunnel.TargetHost != "192.168.1.10" {
		t.Errorf("expected reverse tunnel target host 192.168.1.10, got %q", cfg.ReverseTunnel.TargetHost)
	}
	if cfg.ReverseTunnel.LocalPort != 22 {
		t.Errorf("expected reverse tunnel local port 22, got %d", cfg.ReverseTunnel.LocalPort)
	}
	if cfg.ReverseTunnel.Mode != "ssh_proxy" {
		t.Errorf("expected reverse tunnel mode ssh_proxy, got %q", cfg.ReverseTunnel.Mode)
	}
	if cfg.ReverseTunnel.TargetUser != "root" {
		t.Errorf("expected reverse tunnel target user root, got %q", cfg.ReverseTunnel.TargetUser)
	}
	if cfg.ReverseTunnel.TargetPassword != "boardpw" {
		t.Errorf("expected reverse tunnel target password boardpw, got %q", cfg.ReverseTunnel.TargetPassword)
	}
}
