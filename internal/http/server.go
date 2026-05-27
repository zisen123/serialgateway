package http

import (
	"fmt"
	"log"
	"net/http"

	"github.com/zisen123/serialgateway/internal/config"
	"github.com/zisen123/serialgateway/internal/core"
)

type Gateway struct {
	cfg *config.Config
	pm  *core.PortManager
}

func NewGatewayWithManager(cfg *config.Config, pm *core.PortManager) *Gateway {
	return &Gateway{
		cfg: cfg,
		pm:  pm,
	}
}

func (gw *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ports", gw.handleGetPorts)
	mux.HandleFunc("/api/mappings", gw.handleMappings)
	mux.HandleFunc("/api/mappings/", gw.handleMappingDetail)
	mux.HandleFunc("/api/config", gw.handleConfig)
	if gw.cfg.ADB != nil {
		mux.HandleFunc("/api/adb/devices", gw.handleAdbDevices)
		mux.HandleFunc("/api/adb/", gw.handleAdbDevice)
	}
	return mux
}

func (gw *Gateway) StartHTTP() {
	addr := fmt.Sprintf(":%d", gw.cfg.Gateway.HTTPPort)
	log.Printf("HTTP API listening on %s", addr)
	http.ListenAndServe(addr, gw.Handler())
}