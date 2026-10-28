package render

import (
	"embed"
	"net/http"
)

// Assets are served from the Go binary; production needs no frontend runtime.
//
//go:embed assets/*
var assets embed.FS

//go:embed assets/brand.css
var Style string

func Asset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name != "mark.svg" && name != "wordmark.svg" {
		http.NotFound(w, r)
		return
	}
	data, err := assets.ReadFile("assets/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(data)
}
