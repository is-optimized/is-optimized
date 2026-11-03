package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"github.com/is-optimized/is-optimized/internal/render"
	"html/template"
	"net/http"
	"time"
)

//go:embed home.html
var homeHTML string

var homeTemplate = template.Must(template.New("home").Parse(homeHTML))

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept")
	w.Header().Set("Link", "<"+s.cfg.PublicURL+"/openapi.json>; rel=\"service-desc\"; type=\"application/vnd.oai.openapi+json\", <"+s.cfg.PublicURL+"/.well-known/api-catalog>; rel=\"api-catalog\"")
	if markdownRequested(r) {
		child := r.Clone(r.Context())
		child.URL.Path = "/llms.txt"
		s.publicDocument(w, child)
		return
	}
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
		Origin            string
		Schema            template.JS
	}{template.CSS(render.Style), s.cfg.SMS, s.cfg.Disclosure, featuredSites, recent, err != nil, s.cfg.PublicURL, s.productSchema()})
}

func (s *Server) productSchema() template.JS {
	data, _ := json.Marshal(map[string]any{"@context": "https://schema.org", "@type": "SoftwareApplication", "name": "is-optimized", "url": s.cfg.PublicURL, "description": "Free public website audits for SEO, GEO, AEO and agent readiness with evidence and developer remediation.", "applicationCategory": "DeveloperApplication", "operatingSystem": "Web", "logo": s.cfg.PublicURL + "/assets/mark.svg", "sameAs": []string{"https://github.com/is-optimized/is-optimized", "https://sire.run/"}, "offers": map[string]any{"@type": "Offer", "price": "0", "priceCurrency": "USD"}, "contactPoint": map[string]any{"@type": "ContactPoint", "email": "privacy@sire.run", "contactType": "customer support"}})
	return template.JS(data)
}
