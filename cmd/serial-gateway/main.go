package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/zisen123/serialgateway/internal/config"
	"github.com/zisen123/serialgateway/internal/core"
	sgwhttp "github.com/zisen123/serialgateway/internal/http"
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

	pm := core.NewPortManager(cfg)
	if err := pm.AutoStart(); err != nil {
		log.Printf("auto-start warning: %v", err)
	}

	var rt *tunnel.ReverseTunnel
	if cfg.ReverseTunnel.Host != "" {
		rt = tunnel.New(&cfg.ReverseTunnel)
		rt.Start()
		log.Printf("reverse tunnel started: %s:%d -> local:%d",
			cfg.ReverseTunnel.Host, cfg.ReverseTunnel.RemotePort, cfg.ReverseTunnel.LocalPort)
	}

	gw := sgwhttp.NewGatewayWithManager(cfg, pm)
	go gw.StartHTTP()

	log.Printf("SerialGateway started - HTTP :%d, SSH base port %d", cfg.Gateway.HTTPPort, cfg.SSH.BasePort)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	<-sigCh

	log.Println("shutting down...")
	if rt != nil {
		rt.Stop()
	}
	pm.Shutdown()
}