package scanner

import (
	"fmt"
	"strings"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"golang.org/x/net/html"
)

func hasToken(value, token string) bool {
	for _, part := range strings.Fields(value) {
		if strings.EqualFold(part, token) {
			return true
		}
	}
	return false
}
func observationStatus(p pageResult) string {
	if p.page.Error != "" {
		return "unknown"
	}
	return "information"
}
func wordEvidence(p pageResult, visible string) string {
	if p.page.Error != "" {
		return p.page.Error
	}
	return fmt.Sprintf("%d whitespace-delimited initial-HTML words; excludes head, script and style. Hidden text and language segmentation not verified; no minimum or score penalty.", len(strings.Fields(visible)))
}

// Compare complete normalized values, never the truncated evidence excerpts.
// Redirect aliases count once per final URL, and unobserved pages do not participate.
func markDuplicates(findings []reportmodel.Finding, pages []pageResult) []reportmodel.Finding {
	type metadata struct{ title, description string }
	byURL := map[string]metadata{}
	for _, p := range pages {
		if p.page.Error != "" || p.doc == nil {
			continue
		}
		m := metadata{}
		walk(p.doc, func(n *html.Node) {
			if n.Type != html.ElementNode {
				return
			}
			if n.Data == "title" {
				m.title = text(n)
			}
			if n.Data == "meta" && strings.EqualFold(attr(n, "name"), "description") {
				m.description = attr(n, "content")
			}
		})
		byURL[p.page.URL] = m
	}
	normalize := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	titles, descriptions := map[string]int{}, map[string]int{}
	for _, m := range byURL {
		if k := normalize(m.title); k != "" {
			titles[k]++
		}
		if k := normalize(m.description); k != "" {
			descriptions[k]++
		}
	}
	for i := range findings {
		f := &findings[i]
		if f.Status != "pass" {
			continue
		}
		m, ok := byURL[f.URL]
		if !ok {
			continue
		}
		count := 0
		switch f.ID {
		case "seo.title":
			count = titles[normalize(m.title)]
		case "seo.description":
			count = descriptions[normalize(m.description)]
		default:
			continue
		}
		if count > 1 {
			f.Status = "partial"
			// Put the outcome first so long metadata cannot truncate the duplicate evidence.
			f.Evidence = fmt.Sprintf("Reused on %d distinct observed sample URLs (case/whitespace normalized): %s", count, excerpt(f.Evidence))
			f.Evidence = excerpt(f.Evidence)
		} else {
			f.Evidence = excerpt(fmt.Sprintf("No duplicate among %d distinct observed sample URLs; not a full-site uniqueness check. %s", len(byURL), f.Evidence))
		}
	}
	for _, p := range pages {
		evidence := fmt.Sprintf("HTTP %d; final URL %s", p.page.StatusCode, p.page.URL)
		if p.page.StatusCode == 0 {
			evidence = "No final HTTP response observed"
		}
		if p.page.RequestedURL != "" && p.page.RequestedURL != p.page.URL {
			evidence += "; requested " + p.page.RequestedURL + "; redirected"
		}
		if p.page.Error != "" {
			evidence += "; content unobserved: " + p.page.Error
		} else {
			evidence += "; HTML evaluated"
		}
		findings = append(findings, reportmodel.Finding{ID: "seo.page_status", Pillar: "seo", Title: "Sampled page outcome", Status: observationStatus(p), URL: p.page.URL, Evidence: excerpt(evidence)})
	}
	return findings
}
