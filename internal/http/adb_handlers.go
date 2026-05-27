package http

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/zisen123/serialgateway/internal/adb"
)

// handleAdbDevices handles GET /api/adb/devices
func (gw *Gateway) handleAdbDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	devices, err := adb.ListDevices(gw.cfg.ADB.AdbPath)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]interface{}{"devices": devices})
}

// handleAdbDevice dispatches /api/adb/{serial}/{action}
func (gw *Gateway) handleAdbDevice(w http.ResponseWriter, r *http.Request) {
	// path: /api/adb/{serial}/{action}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/adb/"), "/")
	if len(parts) < 2 || parts[0] == "" {
		writeJSON(w, 400, map[string]string{"error": "missing serial or action"})
		return
	}
	serial := parts[0]
	action := parts[1]

	switch action {
	case "push":
		gw.handleAdbPush(w, r, serial)
	case "pull":
		gw.handleAdbPull(w, r, serial)
	case "exec":
		gw.handleAdbExec(w, r, serial)
	case "exec-stream":
		gw.handleAdbExecStream(w, r, serial)
	default:
		writeJSON(w, 404, map[string]string{"error": "unknown adb action: " + action})
	}
}

// handleAdbPush handles POST /api/adb/{serial}/push
// Body: multipart/form-data with fields "file" (file data) and "path" (remote path)
func (gw *Gateway) handleAdbPush(w http.ResponseWriter, r *http.Request, serial string) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, 400, map[string]string{"error": "parse multipart: " + err.Error()})
		return
	}
	remotePath := strings.TrimSpace(r.FormValue("path"))
	if remotePath == "" {
		writeJSON(w, 400, map[string]string{"error": "path field required"})
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "file field required: " + err.Error()})
		return
	}
	defer file.Close()

	if err := adb.PushReader(gw.cfg.ADB.AdbPath, serial, remotePath, file); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"serial": serial,
		"path":   remotePath,
		"status": "ok",
	})
}

// handleAdbPull handles GET /api/adb/{serial}/pull?path=/remote/path
func (gw *Gateway) handleAdbPull(w http.ResponseWriter, r *http.Request, serial string) {
	if r.Method != "GET" {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	remotePath := r.URL.Query().Get("path")
	if remotePath == "" {
		writeJSON(w, 400, map[string]string{"error": "path query parameter required"})
		return
	}

	// Derive a filename from the remote path for Content-Disposition
	filename := remotePath
	if idx := strings.LastIndexAny(remotePath, "/\\"); idx >= 0 {
		filename = remotePath[idx+1:]
	}
	if filename == "" {
		filename = "download"
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Type", "application/octet-stream")

	if err := adb.PullWriter(gw.cfg.ADB.AdbPath, serial, remotePath, w); err != nil {
		// Headers already sent; log but can't write JSON error
		return
	}
}

// handleAdbExec handles POST /api/adb/{serial}/exec
// Body: {"command": "ls /tmp"}
// Returns synchronously with full stdout/stderr and exit code.
func (gw *Gateway) handleAdbExec(w http.ResponseWriter, r *http.Request, serial string) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Command == "" {
		writeJSON(w, 400, map[string]string{"error": "command is required"})
		return
	}
	result := adb.Exec(gw.cfg.ADB.AdbPath, serial, req.Command)
	writeJSON(w, 200, map[string]interface{}{
		"serial":    serial,
		"command":   req.Command,
		"stdout":    result.Stdout,
		"stderr":    result.Stderr,
		"exit_code": result.ExitCode,
	})
}

// handleAdbExecStream handles POST /api/adb/{serial}/exec-stream
// Body: {"command": "dmesg -w"}
// Streams stdout+stderr as chunked transfer encoding; connection closes when command exits.
func (gw *Gateway) handleAdbExecStream(w http.ResponseWriter, r *http.Request, serial string) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Command == "" {
		writeJSON(w, 400, map[string]string{"error": "command is required"})
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	// Use a flushing writer so chunks arrive at the client immediately.
	fw := &flushWriter{w: w}
	// Run with request context so client disconnect kills the process.
	ctx := r.Context()
	done := make(chan error, 1)
	go func() {
		done <- adb.ExecStream(gw.cfg.ADB.AdbPath, serial, req.Command, fw)
	}()
	select {
	case <-ctx.Done():
	case <-done:
	}
}

type flushWriter struct {
	w io.Writer
}

func (fw *flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if f, ok := fw.w.(http.Flusher); ok {
		f.Flush()
	}
	return n, err
}
