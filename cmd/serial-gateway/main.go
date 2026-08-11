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

	var rt *tunnel.ReverseTunnel
	if cfg.ReverseTunnel.Host != "" {
		rt = tunnel.New(&cfg.ReverseTunnel, &cfg.SSH, logger)
		rt.Start()
		logger.Info("reverse tunnel started",
			"host", cfg.ReverseTunnel.Host,
			"remote_port", cfg.ReverseTunnel.RemotePort,
			"mode", cfg.ReverseTunnel.Mode,
			"target_host", cfg.ReverseTunnel.TargetHost,
			"target_port", cfg.ReverseTunnel.LocalPort)
	}

	var adbRt *tunnel.ReverseTunnel
	if cfg.AdbReverseTunnel.Host != "" {
		adbRt = tunnel.New(&cfg.AdbReverseTunnel, &cfg.SSH, logger)
		adbRt.Start()
		logger.Info("ADB reverse tunnel started",
			"host", cfg.AdbReverseTunnel.Host,
			"remote_port", cfg.AdbReverseTunnel.RemotePort,
			"mode", cfg.AdbReverseTunnel.Mode,
			"target_host", cfg.AdbReverseTunnel.TargetHost,
			"target_port", cfg.AdbReverseTunnel.LocalPort)
	}

	var serialSSHRt *tunnel.ReverseTunnel
	if cfg.SerialSSHTunnel.Host != "" {
		forwards := buildSerialSSHForwards(cfg, logger)
		if len(forwards) == 0 {
			logger.Warn("serial_ssh_tunnel configured but no forwardable COM ports in cfg.ports")
		} else {
			serialSSHRt = tunnel.NewMulti(&cfg.SerialSSHTunnel, &cfg.SSH, forwards, logger)
			serialSSHRt.Start()
			logger.Info("serial SSH reverse tunnel started",
				"host", cfg.SerialSSHTunnel.Host,
				"bind_address", cfg.SerialSSHTunnel.BindAddress,
				"forwards", len(forwards))
		}
	}

	gw := sgwhttp.NewGatewayWithManager(cfg, pm, logger)
	go gw.StartHTTP()

	logger.Info("SerialGateway started", "http_port", cfg.Gateway.HTTPPort, "ssh_base_port", cfg.SSH.BasePort)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	<-sigCh

	logger.Info("shutting down...")
	if rt != nil {
		rt.Stop()
	}
	if adbRt != nil {
		adbRt.Stop()
	}
	if serialSSHRt != nil {
		serialSSHRt.Stop()
	}
	pm.Shutdown()
}
