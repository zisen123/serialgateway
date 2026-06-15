# Toast Notification API Design

**Date**: 2026-06-11  
**Status**: approved  
**Branch**: feat-toast-notification

## Overview

Add a new HTTP endpoint `POST /api/notify` that receives a text message with a title and content, and pops a Windows toast notification on the desktop via the `go-toast` library.

## Use Case

General-purpose notification channel: any external script, automation tool, or service that can reach the gateway's HTTP API may call this to notify the desktop user.

## API Specification

### `POST /api/notify`

**Request** (JSON):
```json
{
  "title": "通知标题",
  "content": "通知正文内容"
}
```

Fields:
- `title` (string, required, non-empty): toast heading (bold line)
- `content` (string, required, non-empty): toast body text

**Response**:

| Status | Condition | Body |
|--------|-----------|------|
| 200 | Notification dispatched to Windows toast system | `{"status": "dispatched"}` |
| 400 | Missing or empty `title` or `content` | `{"error": "title and content are required"}` |
| 405 | Method is not POST | `{"error": "method not allowed"}` |
| 500 | OS-level toast failure (e.g., AUMID registration failed, WinRT error) | `{"error": "<details>"}` |

**Note on 200**: `"dispatched"` rather than `"ok"` to signal that the toast has been handed off to the Windows notification system. The OS may suppress the actual popup (Focus Assist, fullscreen mode, user-disabled notifications) — this is not detectable from the API level.

### Design Decisions

| Item | Decision | Rationale |
|------|----------|-----------|
| Library | `github.com/go-toast/toast` | Pure Go WinRT calls; no PowerShell spawn per notification |
| AUMID registration | On startup, create shortcut in `%APPDATA%\Microsoft\Windows\Start Menu\Programs\` once | Required by Windows 10/11 for non-packaged apps to show modern toasts |
| No config | No config additions needed | Feature is stateless; no tuneable parameters |
| Response semantic | `"dispatched"` not `"ok"` | Honest about the OS-level uncertainty |

## Implementation

### Dependencies

Add to `go.mod`:
```
github.com/go-toast/toast <latest>
```

### File Changes

| File | Action | Purpose |
|------|--------|---------|
| `internal/notify/toast.go` | New | Wraps `go-toast`: AUMID registration on first call, `Show(title, content string) error` |
| `internal/http/notify_handler.go` | New | HTTP handler: parse JSON, validate, call `notify.Show()`, write response |
| `internal/http/server.go` | Edit | Register `POST /api/notify` route in `Handler()` |

### `internal/notify/toast.go`

Package `notify` exposes a single public function:

```go
// Show pops a Windows toast notification with the given title and content.
// On first call, registers the AppUserModelID shortcut if needed.
// Returns nil if the notification was dispatched successfully to WinRT.
func Show(title, content string) error
```

Internally:
- Exported function variable `Show` (defaults to the real implementation) so HTTP handler tests can replace it with a mock.
- Lazy-init: first call runs `toast.Setup()` to create the Start Menu shortcut. Non-Windows platforms will get an error.
- Calls `toast.Notification{AppID: "SerialGateway.Notifier", Title: title, Message: content}.Push()`
- XML-escapes title and content (go-toast may do this; verify; if not, escape `<`, `>`, `&`, `"` before constructing the Notification)

### `internal/http/notify_handler.go`

```go
func (gw *Gateway) handleNotify(w http.ResponseWriter, r *http.Request)
```

Logic:
1. Reject non-POST → 405
2. Decode JSON body → 400 on parse failure
3. Validate `req.Title != ""` and `req.Content != ""` → 400 if either empty
4. Call `notify.Show(req.Title, req.Content)` → 500 on error
5. `writeJSON(w, 200, map[string]string{"status": "dispatched"})`

Logging: info log on success, error log on failure (using `gw.logger`).

### `internal/http/server.go`

Add one route in `Handler()`:
```go
mux.HandleFunc("/api/notify", gw.handleNotify)
```

## Error Handling

| Scenario | Behavior |
|----------|----------|
| Shortcut creation fails on startup | Log WARN; subsequent `Show()` calls may fail → 500 |
| `Push()` returns error | Handler returns 500 with error text |
| Non-Windows OS | `go-toast` or shortcut setup returns error → 500 |
| Focus Assist / user-disabled | `Show()` succeeds; notification silently hidden by OS — no error |

## Testing

### Unit Tests (`internal/http/notify_handler_test.go`)

Mock `notify.Show` (replace the exported function variable in test) and verify:

| Test | Input | Expected |
|------|-------|----------|
| Valid request | `{"title":"T","content":"C"}` | 200, `{"status":"dispatched"}` |
| Missing title | `{"content":"C"}` | 400 |
| Missing content | `{"title":"T"}` | 400 |
| Empty title | `{"title":"","content":"C"}` | 400 |
| Empty content | `{"title":"T","content":""}` | 400 |
| Invalid JSON | `not json` | 400 |
| Non-POST method | GET | 405 |
| notify.Show errors | (mock returns error) | 500 |

### Manual Verification

```bash
curl -X POST http://localhost:8080/api/notify \
  -H "Content-Type: application/json" \
  -d '{"title":"Test Title","content":"Test message body"}'
```

Confirm toast appears on the Windows desktop.

## Risks & Limitations

1. **AUMID shortcut**: Writes one file to `%APPDATA%\Microsoft\Windows\Start Menu\Programs\SerialGateway.Notifier.lnk`. Benign, but observable.
2. **No delivery confirmation**: 200 response means "handed to WinRT," not "user saw it." Focus Assist, fullscreen, or user notification settings may suppress the popup.
3. **go-toast maintenance**: External dependency; if the library breaks on future Windows versions, fallback to PowerShell approach.
4. **No auth**: The endpoint is open (same as existing `/api/*` endpoints). Acceptable for a local gateway service.
