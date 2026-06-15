package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zisen123/serialgateway/internal/config"
	"github.com/zisen123/serialgateway/internal/core"
	"github.com/zisen123/serialgateway/internal/notify"
)

func TestHandleNotify_Valid(t *testing.T) {
	// Mock notify.Show to succeed
	origShow := notify.Show
	defer func() { notify.Show = origShow }()
	notify.Show = func(title, content string) error {
		if title == "" || content == "" {
			return errors.New("title and content are required")
		}
		return nil
	}

	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	pm := core.NewPortManager(cfg, slog.Default())
	gw := NewGatewayWithManager(cfg, pm, slog.Default())
	handler := gw.Handler()

	body := `{"title":"Test Title","content":"Test Content"}`
	req := httptest.NewRequest("POST", "/api/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "dispatched" {
		t.Fatalf("expected status 'dispatched', got %q", resp["status"])
	}
}

func TestHandleNotify_MissingTitle(t *testing.T) {
	origShow := notify.Show
	defer func() { notify.Show = origShow }()
	notify.Show = func(title, content string) error {
		if title == "" || content == "" {
			return errors.New("title and content are required")
		}
		return nil
	}

	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	pm := core.NewPortManager(cfg, slog.Default())
	gw := NewGatewayWithManager(cfg, pm, slog.Default())
	handler := gw.Handler()

	body := `{"content":"C"}`
	req := httptest.NewRequest("POST", "/api/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] == "" {
		t.Fatal("expected error message")
	}
}

func TestHandleNotify_MissingContent(t *testing.T) {
	origShow := notify.Show
	defer func() { notify.Show = origShow }()
	notify.Show = func(title, content string) error {
		if title == "" || content == "" {
			return errors.New("title and content are required")
		}
		return nil
	}

	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	pm := core.NewPortManager(cfg, slog.Default())
	gw := NewGatewayWithManager(cfg, pm, slog.Default())
	handler := gw.Handler()

	body := `{"title":"T"}`
	req := httptest.NewRequest("POST", "/api/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleNotify_EmptyFields(t *testing.T) {
	origShow := notify.Show
	defer func() { notify.Show = origShow }()
	notify.Show = func(title, content string) error {
		if title == "" || content == "" {
			return errors.New("title and content are required")
		}
		return nil
	}

	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	pm := core.NewPortManager(cfg, slog.Default())
	gw := NewGatewayWithManager(cfg, pm, slog.Default())
	handler := gw.Handler()

	for _, tc := range []struct {
		name string
		body string
	}{
		{"both empty", `{"title":"","content":""}`},
		{"empty title", `{"title":"","content":"C"}`},
		{"empty content", `{"title":"T","content":""}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/notify", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != 400 {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHandleNotify_InvalidJSON(t *testing.T) {
	origShow := notify.Show
	defer func() { notify.Show = origShow }()
	// Keep real Show but it shouldn't be called
	notify.Show = func(title, content string) error {
		t.Fatal("Show should not be called for invalid JSON")
		return nil
	}

	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	pm := core.NewPortManager(cfg, slog.Default())
	gw := NewGatewayWithManager(cfg, pm, slog.Default())
	handler := gw.Handler()

	req := httptest.NewRequest("POST", "/api/notify", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleNotify_WrongMethod(t *testing.T) {
	origShow := notify.Show
	defer func() { notify.Show = origShow }()
	// Keep real Show but it shouldn't be called
	notify.Show = func(title, content string) error {
		t.Fatal("Show should not be called for wrong method")
		return nil
	}

	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	pm := core.NewPortManager(cfg, slog.Default())
	gw := NewGatewayWithManager(cfg, pm, slog.Default())
	handler := gw.Handler()

	req := httptest.NewRequest("GET", "/api/notify", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 405 {
		t.Fatalf("expected 405, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleNotify_ShowFails(t *testing.T) {
	origShow := notify.Show
	defer func() { notify.Show = origShow }()
	notify.Show = func(title, content string) error {
		return errors.New("toast push failed: notification system unavailable")
	}

	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	pm := core.NewPortManager(cfg, slog.Default())
	gw := NewGatewayWithManager(cfg, pm, slog.Default())
	handler := gw.Handler()

	body := `{"title":"T","content":"C"}`
	req := httptest.NewRequest("POST", "/api/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] == "" {
		t.Fatal("expected error message")
	}
}
