# Toast Notification API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `POST /api/notify` endpoint that receives title+content and pops a Windows toast notification via the `go-toast` library.

**Architecture:** New `internal/notify` package wraps `github.com/go-toast/toast` with a `Show` function variable (mockable for tests). Handler in `internal/http` parses JSON, validates, and calls `notify.Show`. One new route in `server.go`.

**Tech Stack:** Go 1.26.3, `github.com/go-toast/toast`, `net/http`, `encoding/json`

---

### Task 1: Create worktree and add dependency

**Files:**
- Create: `.claude/worktrees/feat-toast-notification/` (git worktree)
- Update: `go.mod`, `go.sum`

- [ ] **Step 1: Create worktree from latest master**

```bash
git fetch origin master
git worktree add .claude/worktrees/feat-toast-notification origin/master
cd .claude/worktrees/feat-toast-notification
```

Expected: new directory created, HEAD at `origin/master`.

- [ ] **Step 2: Add go-toast dependency**

```bash
go get github.com/go-toast/toast@latest
```

Expected: dependency added to `go.mod` and `go.sum`.

- [ ] **Step 3: Verify go-toast API by reading its types**

In Go module cache or via `go doc`:
```bash
go doc github.com/go-toast/toast Notification
go doc github.com/go-toast/toast Notification.Push
```

Expected: confirm the struct has `AppID`, `Title`, `Message` fields and a `Push() error` method. Note the exact field names for the next task.

- [ ] **Step 4: Commit dependency**

```bash
git add go.mod go.sum
git commit -m "deps: add github.com/go-toast/toast for Windows toast notifications

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 2: Write notify/toast.go

**Files:**
- Create: `internal/notify/toast.go`

- [ ] **Step 1: Create the notify package with Show function**

Create `internal/notify/toast.go`:

```go
// Package notify provides Windows toast notification support.
package notify

import (
	"fmt"

	"github.com/go-toast/toast"
)

const appID = "SerialGateway.Notifier"

// Show is an exported function variable so tests can replace it with a mock.
// The default implementation pops a Windows toast notification.
var Show = showImpl

func showImpl(title, content string) error {
	notification := toast.Notification{
		AppID:   appID,
		Title:   title,
		Message: content,
	}
	if err := notification.Push(); err != nil {
		return fmt.Errorf("toast push failed: %w", err)
	}
	return nil
}
```

**Note on go-toast API:** If the actual go-toast version uses different field names (e.g., `Body` instead of `Message`, or a different struct constructor), adjust accordingly after Task 1 Step 3. The structure remains the same: a `Show` function variable wrapping the library call.

- [ ] **Step 2: Verify the file compiles**

```bash
go build ./internal/notify/
```

Expected: no errors (may produce "no Go files" if running from wrong dir; just ensure no compile errors).

- [ ] **Step 3: Commit**

```bash
git add internal/notify/toast.go
git commit -m "feat: add notify package wrapping go-toast for Windows toast notifications

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 3: Write failing test for notify handler

**Files:**
- Create: `internal/http/notify_handler_test.go`

- [ ] **Step 1: Write the test file**

Create `internal/http/notify_handler_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail (handler/route not yet implemented)**

```bash
go test ./internal/http/ -run TestHandleNotify -v
```

Expected: FAIL — "404 not found" or panic because `/api/notify` route does not exist yet.

- [ ] **Step 3: Commit test file**

```bash
git add internal/http/notify_handler_test.go
git commit -m "test: add failing tests for POST /api/notify handler

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 4: Write notify handler and register route

**Files:**
- Create: `internal/http/notify_handler.go`
- Edit: `internal/http/server.go` (add one route line)

- [ ] **Step 1: Write the handler**

Create `internal/http/notify_handler.go`:

```go
package http

import (
	"encoding/json"
	"net/http"

	"github.com/zisen123/serialgateway/internal/notify"
)

type notifyRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

func (gw *Gateway) handleNotify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}

	var req notifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Title == "" || req.Content == "" {
		writeJSON(w, 400, map[string]string{"error": "title and content are required"})
		return
	}

	if err := notify.Show(req.Title, req.Content); err != nil {
		gw.logger.Error("toast notification failed", "error", err)
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	gw.logger.Info("toast notification dispatched", "title", req.Title)
	writeJSON(w, 200, map[string]string{"status": "dispatched"})
}
```

- [ ] **Step 2: Register the route in server.go**

In `internal/http/server.go`, inside the `Handler()` method, add one line after the existing routes:

```go
mux.HandleFunc("/api/notify", gw.handleNotify)
```

The updated `Handler()` should look like:

```go
func (gw *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ports", gw.handleGetPorts)
	mux.HandleFunc("/api/mappings", gw.handleMappings)
	mux.HandleFunc("/api/mappings/", gw.handleMappingDetail)
	mux.HandleFunc("/api/config", gw.handleConfig)
	mux.HandleFunc("/api/notify", gw.handleNotify)
	return mux
}
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/http/notify_handler.go internal/http/server.go
git commit -m "feat: add POST /api/notify endpoint for Windows toast notifications

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 5: Run tests and fix issues

- [ ] **Step 1: Run all tests**

```bash
go test ./internal/http/ -run TestHandleNotify -v
```

Expected: all 7 tests PASS.

- [ ] **Step 2: Run full test suite to check for regressions**

```bash
go test ./... -count=1
```

Expected: all existing tests still pass.

- [ ] **Step 3: Fix any failures**

If tests fail, fix inline and re-run. Common issues:
- Import path mismatch in `notify_handler.go` (missing the `notify` import)
- go-toast API mismatch (field names differ) — adjust `notify/toast.go`
- If go-toast uses `Body` instead of `Message`, change `notify/toast.go` to use `Body`

- [ ] **Step 4: Commit fixes (if any)**

```bash
git add -A
git commit -m "fix: adjust implementation to pass tests

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 6: Build and manual verification

- [ ] **Step 1: Build the binary**

```bash
go build -o serial-gateway.exe ./cmd/serial-gateway/
```

Expected: binary produced, no errors.

- [ ] **Step 2: Manual verification**

Start the gateway:
```bash
.\serial-gateway.exe
```

In another terminal, send a test notification:
```bash
curl -X POST http://localhost:8080/api/notify -H "Content-Type: application/json" -d '{"title":"Test Title","content":"Test message body"}'
```

Expected:
- HTTP 200 with `{"status":"dispatched"}`
- A Windows toast notification appears on the desktop with title "Test Title" and body "Test message body"

- [ ] **Step 3: Test error cases manually**

```bash
# Missing title — expect 400
curl -X POST http://localhost:8080/api/notify -H "Content-Type: application/json" -d '{"content":"C"}'

# Wrong method — expect 405
curl -X GET http://localhost:8080/api/notify
```

---

### Task 7: Final review and commit

- [ ] **Step 1: Review all changed files**

```bash
git diff --stat origin/master
git log --oneline origin/master..HEAD
```

Expected: 4 new files (`notify/toast.go`, `http/notify_handler.go`, `http/notify_handler_test.go`, `http/server.go` modified), 2 dependency files updated (`go.mod`, `go.sum`).

- [ ] **Step 2: Final commit if needed**

If any cleanup commits remain, squash or leave as-is depending on team preference.

```bash
git log --oneline origin/master..HEAD
```

---

## Plan Notes

1. **go-toast API verification (Task 1 Step 3):** The go-toast library has several forks with slightly different APIs. After `go get`, check the actual struct field names. The plan assumes `toast.Notification{AppID, Title, Message}.Push()`. If the resolved version uses `Body` instead of `Message`, or a `toast.New(...)` constructor pattern, adjust `internal/notify/toast.go` accordingly in Task 2 Step 1.

2. **AUMID shortcut:** Some go-toast versions handle shortcut creation automatically. If the toast doesn't appear during manual testing (Task 6), a shortcut with the `SerialGateway.Notifier` AppUserModelID may need to be created manually or via `toast.Setup()` if the API provides it.

3. **Build constraint:** This feature is Windows-only. The `go-toast` library should handle non-Windows gracefully (stub out), but the plan assumes Windows build target.
