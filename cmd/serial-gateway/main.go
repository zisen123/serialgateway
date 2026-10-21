package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"github.com/zisen123/serialgateway/internal/config"
	"github.com/zisen123/serialgateway/internal/core"
	sgwhttp "github.com/zisen123/serialgateway/internal/http"
	sgwlog "github.com/zisen123/serialgateway/internal/log"
	"github.com/zisen123/serialgateway/internal/tunnel"
)

func main() {
	configPath := flag.String("config", "serial-gateway.yaml", "config file path")
	httpPort := flag.Int("http-port", 0, "override HTTP port (0 = use config)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}
	if *httpPort != 0 {
		cfg.Gateway.HTTPPort = *httpPort
	}

	logger, err := sgwlog.New(&cfg.Logging)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	slog.SetDefault(logger)

	pm := core.NewPortManager(cfg, logger)
	if err := pm.AutoStart(); err != nil {
		logger.Warn("auto-start had warnings", "error", err)
	}

	// Collect every configured tunnel — the three named legacy fields plus any
	// entries in the reverse_tunnels list — and start them uniformly.
	var tunnels []tunnel.Tunnel
	start := func(name string, rtCfg *config.ReverseTunnelConfig) {
		if t := startReverseTunnel(name, rtCfg, cfg, logger); t != nil {
			tunnels = append(tunnels, t)
		}
	}

	if cfg.ReverseTunnel.Host != "" {
		start("reverse_tunnel", &cfg.ReverseTunnel)
	}
	if cfg.AdbReverseTunnel.Host != "" {
		start("adb_reverse_tunnel", &cfg.AdbReverseTunnel)
	}
	if cfg.SerialSSHTunnel.Host != "" {
		cfg.SerialSSHTunnel.Kind = "serial_ssh"
		start("serial_ssh_tunnel", &cfg.SerialSSHTunnel)
	}
	for i := range cfg.ReverseTunnels {
		rtCfg := &cfg.ReverseTunnels[i]
		if rtCfg.Host == "" {
			continue
		}
		name := rtCfg.Name
		if name == "" {
			name = fmt.Sprintf("reverse_tunnels[%d]", i)
		}
		start(name, rtCfg)
	}

	gw := sgwhttp.NewGatewayWithManager(cfg, pm, logger)
	go gw.StartHTTP()

	logger.Info("SerialGateway started", "http_port", cfg.Gateway.HTTPPort, "ssh_base_port", cfg.SSH.BasePort)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	<-sigCh

	logger.Info("shutting down...")
	for _, t := range tunnels {
		t.Stop()
	}
	pm.Shutdown()
}

// startReverseTunnel builds and starts one tunnel, dispatching on Kind:
// "serial_ssh" auto-derives per-COM-port forwards, everything else is a
// single remote_port -> target_host:local_port forward. Returns nil when the
// tunnel could not be started (e.g. serial_ssh with no forwardable ports).
func startReverseTunnel(name string, rtCfg *config.ReverseTunnelConfig, cfg *config.Config, logger *slog.Logger) tunnel.Tunnel {
	if rtCfg.Kind == "local_forward" {
		t := tunnel.NewLocalForward(rtCfg, logger)
		t.Start()
		logger.Info("local forward tunnel started",
			"name", name,
			"host", rtCfg.Host,
			"local_bind", rtCfg.BindAddress,
			"local_port", rtCfg.LocalPort,
			"target_host", rtCfg.TargetHost,
			"target_port", rtCfg.RemotePort)
		return t
	}

	if rtCfg.Kind == "serial_ssh" {
		forwards := buildSerialSSHForwards(cfg, logger)
		if len(forwards) == 0 {
			logger.Warn("serial_ssh tunnel configured but no forwardable COM ports in cfg.ports", "name", name)
			return nil
		}
		t := tunnel.NewMulti(rtCfg, &cfg.SSH, forwards, logger)
		t.Start()
		logger.Info("serial SSH reverse tunnel started",
			"name", name,
			"host", rtCfg.Host,
			"bind_address", rtCfg.BindAddress,
			"forwards", len(forwards))
		return t
	}

	t := tunnel.New(rtCfg, &cfg.SSH, logger)
	t.Start()
	logger.Info("reverse tunnel started",
		"name", name,
		"host", rtCfg.Host,
		"remote_port", rtCfg.RemotePort,
		"mode", rtCfg.Mode,
		"target_host", rtCfg.TargetHost,
		"target_port", rtCfg.LocalPort)
	return t
}
