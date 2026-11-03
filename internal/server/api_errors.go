package server

import (
	"encoding/json"
	"mime"
	"net/http"
	"strings"
)

func wantsJSON(r *http.Request) bool {
	for _, value := range strings.Split(r.Header.Get("Accept"), ",") {
		kind, params, err := mime.ParseMediaType(strings.TrimSpace(value))
		if err == nil && (kind == "application/json" || kind == "application/problem+json") && params["q"] != "0" && params["q"] != "0.0" {
			return true
		}
	}
	return false
}

// Preserve existing text errors for clients that did not request JSON. Only
// error bodies are buffered, and their public detail is bounded.
type problemWriter struct {
	http.ResponseWriter
	status      int
	passthrough bool
	detail      strings.Builder
}

func (p *problemWriter) WriteHeader(status int) {
	if p.status != 0 {
		return
	}
	p.status = status
	kind := p.Header().Get("Content-Type")
	p.passthrough = status < 400 || strings.HasPrefix(kind, "application/json") || strings.HasPrefix(kind, "application/problem+json")
	if p.passthrough {
		p.ResponseWriter.WriteHeader(status)
	}
}

func (p *problemWriter) Write(body []byte) (int, error) {
	if p.status == 0 {
		p.WriteHeader(http.StatusOK)
	}
	if p.passthrough {
		return p.ResponseWriter.Write(body)
	}
	if remaining := 1024 - p.detail.Len(); remaining > 0 {
		p.detail.Write(body[:min(len(body), remaining)])
	}
	return len(body), nil
}

func (p *problemWriter) finish() {
	if p.status < 400 || p.passthrough {
		return
	}
	p.Header().Set("Content-Type", "application/problem+json")
	p.Header().Del("Content-Length")
	p.Header().Add("Vary", "Accept")
	p.ResponseWriter.WriteHeader(p.status)
	json.NewEncoder(p.ResponseWriter).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(p.status), "status": p.status, "detail": strings.TrimSpace(p.detail.String())})
}
