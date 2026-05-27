package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zisen123/serialgateway/internal/adb"
	"github.com/zisen123/serialgateway/internal/config"
	"github.com/zisen123/serialgateway/internal/serial"
	"github.com/zisen123/serialgateway/internal/ssh"
)

func writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func (gw *Gateway) handleGetPorts(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	result := make([]map[string]interface{}, 0)

	ports, err := serial.ListPorts()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	for _, p := range ports {
		baudrate := gw.cfg.SerialDefaults.Baudrate
		for _, cp := range gw.cfg.Ports {
			if cp.Device == p.Device && cp.Baudrate != 0 {
				baudrate = cp.Baudrate
			}
		}
		sshPort := ssh.PortMapping(p.Device, gw.cfg)
		status := "inactive"
		srv := gw.pm.GetServer(p.Device)
		if srv != nil && srv.IsRunning() {
			status = "active"
		}
		result = append(result, map[string]interface{}{
			"device":      p.Device,
			"description": p.Description,
			"hwid":        p.HWID,
			"baudrate":    baudrate,
			"ssh_port":    sshPort,
			"status":      status,
			"type":        "serial",
		})
	}

	if gw.cfg.ADB != nil {
		adbDevices, err := adb.ListDevices(gw.cfg.ADB.AdbPath)
		if err == nil {
			for _, d := range adbDevices {
				sshPort := ssh.ADBPortMapping(d.Serial, gw.cfg)
				status := "inactive"
				srv := gw.pm.GetServer(d.Serial)
				if srv != nil && srv.IsRunning() {
					status = "active"
				}
				result = append(result, map[string]interface{}{
					"device":      d.Serial,
					"description": d.Model,
					"hwid":        "",
					"baudrate":    0,
					"ssh_port":    sshPort,
					"status":      status,
					"type":        "adb",
				})
			}
		}
	}

	writeJSON(w, 200, map[string]interface{}{"ports": result})
}

func (gw *Gateway) handleMappings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		gw.handleGetMappings(w, r)
	case "POST":
		gw.handleCreateMapping(w, r)
	default:
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
	}
}

func (gw *Gateway) handleGetMappings(w http.ResponseWriter, r *http.Request) {
	mappings := gw.pm.Mappings()
	result := make([]map[string]interface{}, 0, len(mappings))
	for _, m := range mappings {
		device := m["device"].(string)
		srv := gw.pm.GetServer(device)
		result = append(result, map[string]interface{}{
			"device":      device,
			"serial_port": device,
			"ssh_port":    m["ssh_port"],
			"connections": 0,
			"baudrate":    srv.Session().Baudrate(),
			"connected":   srv.Session().IsConnected(),
			"type":        m["type"],
		})
	}
	writeJSON(w, 200, map[string]interface{}{"mappings": result})
}

func (gw *Gateway) handleCreateMapping(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Device string `json:"device"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Device == "" {
		writeJSON(w, 400, map[string]string{"error": "device is required"})
		return
	}
	srv, err := gw.pm.AddMapping(req.Device)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeJSON(w, 409, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
		}
		return
	}
	writeJSON(w, 201, map[string]interface{}{
		"device":      req.Device,
		"serial_port": req.Device,
		"ssh_port":    srv.Port(),
		"status":      "active",
		"type":        gw.pm.GetDeviceType(req.Device),
	})
}

func (gw *Gateway) handleMappingDetail(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(strings.TrimPrefix(path, "/api/mappings/"), "/")
	device := parts[0]

	if r.Method == "DELETE" && len(parts) == 1 {
		err := gw.pm.RemoveMapping(device)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]interface{}{
			"device": device,
			"status": "removed",
		})
		return
	}

	srv := gw.pm.GetServer(device)
	if srv == nil {
		writeJSON(w, 404, map[string]string{"error": "mapping not found for " + device})
		return
	}
	if len(parts) >= 2 {
		switch parts[1] {
		case "tail":
			gw.handleTail(w, r, srv)
		case "log":
			gw.handleLog(w, r, srv)
		case "write":
			gw.handleWrite(w, r, srv)
		default:
			writeJSON(w, 404, map[string]string{"error": "unknown sub-path"})
		}
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"device":      srv.Device(),
		"serial_port": srv.Device(),
		"ssh_port":    srv.Port(),
		"connected":   srv.Session().IsConnected(),
		"baudrate":    srv.Session().Baudrate(),
		"type":        gw.pm.GetDeviceType(srv.Device()),
	})
}

func (gw *Gateway) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		writeJSON(w, 200, gw.cfg)
	case "PUT":
		var newCfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid config"})
			return
		}
		config.ApplyDefaults(&newCfg)
		gw.cfg = &newCfg
		writeJSON(w, 200, gw.cfg)
	default:
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
	}
}

func (gw *Gateway) handleTail(w http.ResponseWriter, r *http.Request, srv *ssh.SSHServer) {
	lines := 200
	if l := r.URL.Query().Get("lines"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			lines = n
		}
	}
	entries := srv.Session().RingBuffer().Tail(lines)
	var textLines []string
	for _, e := range entries {
		textLines = append(textLines, e.Line)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(200)
	w.Write([]byte(strings.Join(textLines, "\n")))
}

func (gw *Gateway) handleLog(w http.ResponseWriter, r *http.Request, srv *ssh.SSHServer) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "text"
	}
	entries := srv.Session().RingBuffer().All()
	if format == "jsonl" {
		var lines []string
		for _, e := range entries {
			b, _ := json.Marshal(e)
			lines = append(lines, string(b))
		}
		content := strings.Join(lines, "\n")
		writeJSON(w, 200, map[string]interface{}{
			"device":  srv.Device(),
			"format":  "jsonl",
			"content": content,
			"bytes":   len(content),
		})
		return
	}
	var textLines []string
	for _, e := range entries {
		textLines = append(textLines, fmt.Sprintf("%s [%s] %s", e.TS.Format(time.RFC3339), srv.Device(), e.Line))
	}
	content := strings.Join(textLines, "\n")
	writeJSON(w, 200, map[string]interface{}{
		"device":  srv.Device(),
		"format":  "text",
		"content": content,
		"bytes":   len(content),
	})
}

func (gw *Gateway) handleWrite(w http.ResponseWriter, r *http.Request, srv *ssh.SSHServer) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]string{"error": "method not allowed, use POST"})
		return
	}
	var req struct {
		Data string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Data == "" {
		writeJSON(w, 400, map[string]string{"error": "data is required"})
		return
	}

	sess := srv.Session()
	if !sess.IsConnected() {
		if err := sess.Open(); err != nil {
			writeJSON(w, 500, map[string]string{"error": fmt.Sprintf("serial open failed: %v", err)})
			return
		}
	}

	writeReq := serial.WriteRequest{
		Data: []byte(req.Data),
		Done: make(chan error, 1),
	}
	sess.WriteChannel() <- writeReq

	var writeErr error
	select {
	case writeErr = <-writeReq.Done:
	case <-time.After(gw.cfg.SerialDefaults.WriteTimeout):
		writeErr = fmt.Errorf("write timeout")
	}

	if writeErr != nil {
		writeJSON(w, 500, map[string]string{"error": writeErr.Error()})
		return
	}

	time.Sleep(500 * time.Millisecond)

	entries := sess.RingBuffer().Tail(50)
	var responseLines []string
	for _, e := range entries {
		responseLines = append(responseLines, e.Line)
	}
	writeJSON(w, 200, map[string]interface{}{
		"sent":   req.Data,
		"output": strings.Join(responseLines, "\n"),
	})
}