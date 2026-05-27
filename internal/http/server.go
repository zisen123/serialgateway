package http

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/zisen123/serialgateway/internal/config"
	"github.com/zisen123/serialgateway/internal/core"
)

type Gateway struct {
	cfg    *config.Config
	pm     *core.PortManager
	logger *slog.Logger
}

func NewGatewayWithManager(cfg *config.Config, pm *core.PortManager, logger *slog.Logger) *Gateway {
	return &Gateway{
		cfg:    cfg,
		pm:     pm,
		logger: logger,
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
	gw.logger.Info("HTTP API listening", "addr", addr)
	http.ListenAndServe(addr, gw.Handler())
}