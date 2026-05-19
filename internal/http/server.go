package http

import (
	"fmt"
	"log"
	"net/http"

	"github.com/yicongwu/serialgateway/internal/config"
	"github.com/yicongwu/serialgateway/internal/core"
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
	return mux
}

func (gw *Gateway) StartHTTP() {
	addr := fmt.Sprintf(":%d", gw.cfg.Gateway.HTTPPort)
	log.Printf("HTTP API listening on %s", addr)
	http.ListenAndServe(addr, gw.Handler())
}