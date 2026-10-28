package server

import (
	"context"
	_ "embed"
	"github.com/is-optimized/is-optimized/internal/render"
	"html/template"
	"net/http"
	"time"
)

//go:embed home.html
var homeHTML string

var homeTemplate = template.Must(template.New("home").Parse(homeHTML))

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	recent, err := s.recentPublicReports(ctx)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	homeTemplate.Execute(w, struct {
		CSS               template.CSS
		SMS               bool
		Disclosure        string
		Featured          []featuredSite
		Recent            []publicSite
		RecentUnavailable bool
	}{template.CSS(render.Style), s.cfg.SMS, s.cfg.Disclosure, featuredSites, recent, err != nil})
}
