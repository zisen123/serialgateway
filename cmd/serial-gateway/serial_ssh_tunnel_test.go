package main

import (
	"log/slog"
	"testing"

	"github.com/zisen123/serialgateway/internal/config"
)

func TestBuildSerialSSHForwards(t *testing.T) {
	cfg := &config.Config{
		SSH:   config.SSHConfig{BasePort: 2200},
		Ports: []config.PortConfig{{Device: "COM3"}, {Device: "COM25"}},
	}
	fs := buildSerialSSHForwards(cfg, slog.Default())

	if len(fs) != 2 {
		t.Fatalf("len = %d, want 2 (%+v)", len(fs), fs)
	}
	want := map[string]int{"COM3": 2203, "COM25": 2225}
	for _, f := range fs {
		if want[f.Label] != f.RemotePort {
			t.Fatalf("%s RemotePort = %d, want %d", f.Label, f.RemotePort, want[f.Label])
		}
		if f.RemotePort != f.TargetPort {
			t.Fatalf("%s RemotePort(%d) != TargetPort(%d)", f.Label, f.RemotePort, f.TargetPort)
		}
		if f.TargetHost != "127.0.0.1" {
			t.Fatalf("%s TargetHost = %q, want 127.0.0.1", f.Label, f.TargetHost)
		}
	}
}

func TestBuildSerialSSHForwards_SkipsNonCOM(t *testing.T) {
	cfg := &config.Config{
		SSH:   config.SSHConfig{BasePort: 2200},
		Ports: []config.PortConfig{{Device: "/dev/ttyUSB0"}},
	}
	fs := buildSerialSSHForwards(cfg, slog.Default())
	if len(fs) != 0 {
		t.Fatalf("len = %d, want 0 (%+v)", len(fs), fs)
	}
}

func TestBuildSerialSSHForwards_Dedup(t *testing.T) {
	cfg := &config.Config{
		SSH:   config.SSHConfig{BasePort: 2200},
		Ports: []config.PortConfig{{Device: "COM3"}, {Device: "COM3"}},
	}
	fs := buildSerialSSHForwards(cfg, slog.Default())
	if len(fs) != 1 {
		t.Fatalf("len = %d, want 1 (%+v)", len(fs), fs)
	}
}

func TestBuildSerialSSHForwards_Empty(t *testing.T) {
	cfg := &config.Config{SSH: config.SSHConfig{BasePort: 2200}}
	fs := buildSerialSSHForwards(cfg, slog.Default())
	if len(fs) != 0 {
		t.Fatalf("len = %d, want 0", len(fs))
	}
}
