package http

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zisen123/serialgateway/internal/serial"
	"github.com/zisen123/serialgateway/internal/ssh"
)

func (gw *Gateway) handleExec(w http.ResponseWriter, r *http.Request, srv *ssh.SSHServer) {
	if r.Method != "GET" {
		writeJSON(w, 405, map[string]string{"error": "method not allowed, use GET"})
		return
	}

	cmd := r.URL.Query().Get("cmd")
	if cmd == "" {
		writeJSON(w, 400, map[string]string{"error": "cmd query parameter is required"})
		return
	}

	sess := srv.Session()
	if !sess.IsConnected() {
		if err := sess.Open(); err != nil {
			writeJSON(w, 500, map[string]string{"error": fmt.Sprintf("serial open failed: %v", err)})
			return
		}
	}

	// Subscribe before writing so we don't miss output
	sub := sess.Subscribe()
	defer sess.Unsubscribe(sub)

	// Send the command
	writeReq := serial.WriteRequest{
		Data: []byte(cmd),
		Done: make(chan error, 1),
	}
	sess.WriteChannel() <- writeReq

	select {
	case writeErr := <-writeReq.Done:
		if writeErr != nil {
			writeJSON(w, 500, map[string]string{"error": writeErr.Error()})
			return
		}
	case <-time.After(gw.cfg.SerialDefaults.WriteTimeout):
		writeJSON(w, 500, map[string]string{"error": "write timeout"})
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("WARN: ResponseWriter does not support Flusher; streaming may be buffered")
	}

	silenceTimeout := gw.cfg.SerialDefaults.ExecSilenceTimeout
	if v := r.URL.Query().Get("silence_timeout"); v != "" {
		if d, err := strconv.ParseFloat(v, 64); err == nil && d > 0 {
			silenceTimeout = time.Duration(d * float64(time.Second))
		}
	}
	maxDuration := gw.cfg.SerialDefaults.ExecMaxDuration
	if v := r.URL.Query().Get("max_duration"); v != "" {
		if d, err := strconv.ParseFloat(v, 64); err == nil && d > 0 {
			maxDuration = time.Duration(d * float64(time.Second))
		}
	}

	silenceTimer := time.NewTimer(silenceTimeout)
	maxTimer := time.NewTimer(maxDuration)
	defer silenceTimer.Stop()
	defer maxTimer.Stop()

	var lineBuf []byte

	for {
		select {
		case <-r.Context().Done():
			return

		case <-maxTimer.C:
			// Flush any remaining partial data
			if len(lineBuf) > 0 {
				fmt.Fprintf(w, "data: %s\n", string(lineBuf))
				lineBuf = nil
			}
			fmt.Fprintf(w, "event: done\ndata: {\"reason\":\"timeout\"}\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return

		case <-silenceTimer.C:
			// Flush any remaining partial data
			if len(lineBuf) > 0 {
				fmt.Fprintf(w, "data: %s\n", string(lineBuf))
				lineBuf = nil
			}
			fmt.Fprintf(w, "event: done\ndata: {\"reason\":\"silence\"}\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return

		case msg, ok := <-sub:
			if !ok {
				return
			}
			// Reset silence timer on each received message
			if !silenceTimer.Stop() {
				select {
				case <-silenceTimer.C:
				default:
				}
			}
			silenceTimer.Reset(silenceTimeout)

			// Accumulate and split on newlines
			lineBuf = append(lineBuf, []byte(msg)...)
			for {
				idx := -1
				for i, b := range lineBuf {
					if b == '\n' {
						idx = i
						break
					}
				}
				if idx < 0 {
					break
				}
				line := string(lineBuf[:idx])
				lineBuf = lineBuf[idx+1:]
				line = strings.TrimRight(line, "\r")
				fmt.Fprintf(w, "data: %s\n", line)
			}
			fmt.Fprintf(w, "\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}