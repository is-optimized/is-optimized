package scanner

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"golang.org/x/net/html"
)

var explicitPrice = regexp.MustCompile(`(?i)([$€£]\s?\d+|\b\d+(?:\.\d+)?\s?(USD|EUR|GBP)\b)`)

func pricingChecks(home *url.URL, pages []pageResult) []reportmodel.Finding {
	f := reportmodel.Finding{ID: "agent.pricing", Pillar: "agent", Title: "Public pricing information", Status: "unknown", Weight: 1, Tier: "recommended", Stage: "read", URL: home.String(), Applicability: "Applies to inferred apps/stores or explicitly advertised pricing/offers. Examines sampled HTML and Offer declarations, not completeness of commercial terms.", Remediation: "Publish truthful pricing or a clearly explained free offer in initial HTML and link its public pricing page. For structured offers include verified price and priceCurrency; never invent prices.", Standards: []reportmodel.StandardReference{{ID: "Schema.org Offer", URL: "https://schema.org/Offer", Coverage: "partial"}}}
	if len(pages) == 0 || pages[0].doc == nil || pages[0].page.Error != "" {
		f.Evidence = "Homepage HTML unavailable; pricing applicability and information were not observed."
		return []reportmodel.Finding{f}
	}
	typeValue, _ := inferSiteType(home, pages[0])
	links := map[string]bool{}
	offer := false
	walk(pages[0].doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "a" {
			if u, ok := readinessLink(attr(n, "href"), home); ok && origin(u) == origin(home) && pathSegment(strings.ToLower(u.Path), "pricing", "prices", "plans") {
				links[u.String()] = true
			}
		}
		if n.Data == "script" && strings.EqualFold(attr(n, "type"), "application/ld+json") {
			var data any
			if json.Unmarshal([]byte(rawScriptText(n)), &data) == nil {
				var entities []map[string]any
				walkJSONLD(data, &entities, map[string]bool{})
				for _, entity := range entities {
					if schemaTypes(entity)["offer"] && schemaValuePresent(entity["price"]) && schemaValuePresent(entity["priceCurrency"]) {
						offer = true
					}
				}
			}
		}
	})
	if typeValue != "app" && typeValue != "store" && len(links) == 0 && !offer {
		f.Status = "not_applicable"
		f.Evidence = "No app/store type, pricing link or explicit Offer was observed; pricing publication was not assumed for an informational/business site."
		return []reportmodel.Finding{f}
	}
	if offer && (explicitPrice.MatchString(initialText(pages[0].doc)) || strings.Contains(strings.ToLower(initialText(pages[0].doc)), "free")) {
		f.Status = "pass"
		f.Evidence = "An Offer with price/currency and visible price/free language was observed on the homepage; commercial accuracy and all plan terms were not verified."
		return []reportmodel.Finding{f}
	}
	observed, unavailable := 0, 0
	for _, page := range pages {
		if !links[page.page.URL] && !links[page.page.RequestedURL] {
			continue
		}
		if page.doc == nil || page.page.Error != "" || page.page.StatusCode != 200 {
			unavailable++
			continue
		}
		if explicitPrice.MatchString(initialText(page.doc)) {
			observed++
		}
	}
	switch {
	case observed > 0:
		f.Status = "pass"
	case len(links) > 0 || offer:
		f.Status = "partial"
		if unavailable > 0 {
			f.Status = "unknown"
		}
	default:
		f.Status = "fail"
	}
	f.Evidence = fmt.Sprintf("%d first-party pricing links; %d sampled pricing pages with explicit currency amounts; %d unavailable sampled pricing pages; structured Offer=%t. Unsampled links establish discovery only.", len(links), observed, unavailable, offer)
	return []reportmodel.Finding{f}
}
