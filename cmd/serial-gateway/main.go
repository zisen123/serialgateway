package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/yicongwu/serialgateway/internal/config"
	"github.com/yicongwu/serialgateway/internal/core"
	sgwhttp "github.com/yicongwu/serialgateway/internal/http"
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

	gw := sgwhttp.NewGatewayWithManager(cfg, pm)
	go gw.StartHTTP()

	log.Printf("SerialGateway started — HTTP :%d, SSH base port %d", cfg.Gateway.HTTPPort, cfg.SSH.BasePort)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	<-sigCh

	log.Println("shutting down...")
	pm.Shutdown()
}