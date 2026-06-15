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