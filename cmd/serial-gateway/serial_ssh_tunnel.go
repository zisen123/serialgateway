package main

import (
	"log/slog"

	"github.com/zisen123/serialgateway/internal/config"
	"github.com/zisen123/serialgateway/internal/ssh"
	"github.com/zisen123/serialgateway/internal/tunnel"
)

// buildSerialSSHForwards derives one forward per configured COM port so the
// remote host exposes each serial-SSH endpoint on the SAME port number the
// gateway uses locally (COM3 -> 2203 on both sides).
//
// Coverage is the startup cfg.Ports list only; mappings added later via the HTTP
// API are not auto-forwarded (the gateway must be restarted to pick them up).
func buildSerialSSHForwards(cfg *config.Config, logger *slog.Logger) []tunnel.ForwardSpec {
	seen := make(map[int]struct{})
	var forwards []tunnel.ForwardSpec
	for _, p := range cfg.Ports {
		port := ssh.PortMapping(p.Device, cfg)
		if port == 0 {
			logger.Warn("skipping device for serial SSH tunnel: cannot derive port", "device", p.Device)
			continue
		}
		if _, dup := seen[port]; dup {
			logger.Warn("skipping duplicate port for serial SSH tunnel", "device", p.Device, "port", port)
			continue
		}
		seen[port] = struct{}{}
		forwards = append(forwards, tunnel.ForwardSpec{
			Label:      p.Device,
			RemotePort: port,
			TargetHost: "127.0.0.1",
			TargetPort: port, // identical on both sides by design
		})
	}
	return forwards
}
