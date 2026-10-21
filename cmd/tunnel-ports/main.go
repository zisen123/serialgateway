// tunnel-ports prints every reverse-tunnel target declared by a gateway config
// as JSON, so shell scripts (clean-tunnel-ports.ps1, restart-gateway.ps1) can
// consume the exact same tunnel set the gateway itself would start — parsed by
// the same internal/config loader, never hand-copied.
//
// Output:
//
//	{"http_port":18080,"tunnels":[
//	  {"name":"reverse_tunnel","host":"10.80.38.24","port":22,"user":"yicong.wu",
//	   "private_key_file":"C:/...","remote_ports":[18080]},
//	  {"name":"opencli-19825","kind":"local_forward","local_port":19825}] }
//
// Tunnels with a remote listener list their remote ports (serial_ssh expands to
// one per configured COM port, mirroring cmd/serial-gateway); local_forward
// entries carry local_port instead; disabled tunnels (host:"") are omitted.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/zisen123/serialgateway/internal/config"
	gatewayssh "github.com/zisen123/serialgateway/internal/ssh"
)

type tunnelTarget struct {
	Name           string `json:"name"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	User           string `json:"user"`
	PrivateKeyFile string `json:"private_key_file,omitempty"`
	// RemotePorts are the ports this tunnel asks the server to bind. Empty for
	// local_forward (which listens on this machine instead).
	RemotePorts []int `json:"remote_ports,omitempty"`
	// LocalPort is set only for local_forward: the port bound on this machine.
	LocalPort int `json:"local_port,omitempty"`
}

type output struct {
	HTTPPort int            `json:"http_port"`
	LogFile  string         `json:"log_file"`
	Tunnels  []tunnelTarget `json:"tunnels"`
}

func main() {
	configPath := flag.String("config", "serial-gateway.yaml", "config file path")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Same collection order as cmd/serial-gateway/main.go: three named tunnels,
	// then every reverse_tunnels list entry.
	named := []struct {
		name string
		cfg  *config.ReverseTunnelConfig
	}{
		{"reverse_tunnel", &cfg.ReverseTunnel},
		{"adb_reverse_tunnel", &cfg.AdbReverseTunnel},
		{"serial_ssh_tunnel", &cfg.SerialSSHTunnel},
	}
	var targets []tunnelTarget
	for _, n := range named {
		if n.cfg.Host == "" {
			continue
		}
		// Mirror main.go: the named serial_ssh_tunnel field implies serial_ssh
		// behavior even when the YAML section doesn't set kind explicitly.
		if n.name == "serial_ssh_tunnel" {
			n.cfg.Kind = "serial_ssh"
		}
		targets = append(targets, buildTarget(n.name, n.cfg, cfg))
	}
	for i := range cfg.ReverseTunnels {
		rt := &cfg.ReverseTunnels[i]
		if rt.Host == "" {
			continue
		}
		name := rt.Name
		if name == "" {
			name = fmt.Sprintf("reverse_tunnels[%d]", i)
		}
		targets = append(targets, buildTarget(name, rt, cfg))
	}

	out, _ := json.MarshalIndent(output{HTTPPort: cfg.Gateway.HTTPPort, LogFile: cfg.Logging.FilePath, Tunnels: targets}, "", "  ")
	fmt.Println(string(out))
}

// buildTarget expands one tunnel config into its remote-listener ports. Port
// derivation mirrors startReverseTunnel in cmd/serial-gateway/main.go.
func buildTarget(name string, rt *config.ReverseTunnelConfig, cfg *config.Config) tunnelTarget {
	t := tunnelTarget{
		Name:           name,
		Host:           rt.Host,
		Port:           rt.Port,
		User:           rt.User,
		PrivateKeyFile: rt.PrivateKeyFile,
	}
	switch {
	case rt.Kind == "local_forward":
		t.LocalPort = rt.LocalPort
	case rt.Kind == "serial_ssh":
		seen := map[int]struct{}{}
		for _, p := range cfg.Ports {
			port := gatewayssh.PortMapping(p.Device, cfg)
			if port == 0 {
				continue
			}
			if _, dup := seen[port]; dup {
				continue
			}
			seen[port] = struct{}{}
			t.RemotePorts = append(t.RemotePorts, port)
		}
	default:
		t.RemotePorts = []int{rt.RemotePort}
	}
	return t
}
