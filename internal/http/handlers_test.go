package http

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/yicongwu/serialgateway/internal/config"
	"github.com/yicongwu/serialgateway/internal/core"
)

func TestGetPorts(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	pm := core.NewPortManager(cfg)
	gw := NewGatewayWithManager(cfg, pm)
	handler := gw.Handler()

	req := httptest.NewRequest("GET", "/api/ports", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	ports, ok := resp["ports"]
	if !ok {
		t.Fatal("response missing 'ports' key")
	}
	t.Logf("ports: %v", ports)
}

func TestGetMappings(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	pm := core.NewPortManager(cfg)
	gw := NewGatewayWithManager(cfg, pm)
	handler := gw.Handler()

	req := httptest.NewRequest("GET", "/api/mappings", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	mappings, ok := resp["mappings"]
	if !ok {
		t.Fatal("response missing 'mappings' key")
	}
	t.Logf("mappings: %v", mappings)
}

func TestGetConfig(t *testing.T) {
	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	pm := core.NewPortManager(cfg)
	gw := NewGatewayWithManager(cfg, pm)
	handler := gw.Handler()

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["gateway"] == nil {
		t.Fatal("response missing 'gateway' key")
	}
}