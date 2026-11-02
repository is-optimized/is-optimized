package scanner

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"golang.org/x/net/html"
)

const apiCatalogRFC = "https://www.rfc-editor.org/rfc/rfc9727.html"

type apiLinkset struct {
	Linkset []map[string]json.RawMessage `json:"linkset"`
}

type apiCatalogLink struct {
	Href string `json:"href"`
}

func apiCatalogChecks(ss *session, home *url.URL, page pageResult) []reportmodel.Finding {
	applicable := page.doc != nil && hasAPISurface(page.doc, home)
	f := reportmodel.Finding{
		ID: "agent.api_catalog", Pillar: "agent", Title: "API catalog discovery", Status: "unknown", Weight: 2, Tier: "essential", Stage: "find",
		URL: origin(home) + "/.well-known/api-catalog", Applicability: "Applies when the homepage advertises an API, developer documentation, or authentication surface.",
		Remediation: "Publish /.well-known/api-catalog using RFC 9727 and application/linkset+json, with at least one link to a public API endpoint.",
		Standards:   []reportmodel.StandardReference{{ID: "RFC 9727", URL: apiCatalogRFC, Coverage: "partial"}},
	}
	if !applicable {
		f.Status = "not_applicable"
		f.Evidence = "No API or developer surface was linked from the homepage."
		return []reportmodel.Finding{f}
	}
	resp, err := ss.probeAccept(f.URL, origin(home), "application/linkset+json")
	if err != nil {
		f.Evidence = "API catalog was not observed: " + err.Error()
		return []reportmodel.Finding{f}
	}
	if resp.code == http.StatusNotFound || resp.code == http.StatusGone {
		f.Status = "fail"
		f.Evidence = fmt.Sprintf("RFC 9727 well-known API catalog returned HTTP %d", resp.code)
		return []reportmodel.Finding{f}
	}
	if resp.code >= http.StatusInternalServerError || resp.code == http.StatusTooManyRequests {
		f.Evidence = fmt.Sprintf("API catalog was not observed: endpoint returned HTTP %d", resp.code)
		return []reportmodel.Finding{f}
	}
	mediaType, _, mediaErr := mime.ParseMediaType(resp.contentType)
	if resp.code != http.StatusOK || mediaErr != nil || mediaType != "application/linkset+json" {
		f.Status = "fail"
		f.Evidence = fmt.Sprintf("Expected RFC 9727 application/linkset+json response; observed HTTP %d with Content-Type %q", resp.code, resp.contentType)
		return []reportmodel.Finding{f}
	}
	var doc apiLinkset
	if err := json.Unmarshal(resp.body, &doc); err != nil || len(doc.Linkset) == 0 {
		f.Status = "fail"
		f.Evidence = "The response was not a valid Linkset JSON document with a non-empty linkset array."
		return []reportmodel.Finding{f}
	}
	count := 0
	for _, set := range doc.Linkset {
		if raw, ok := set["item"]; ok {
			var links []apiCatalogLink
			if json.Unmarshal(raw, &links) == nil {
				for _, link := range links {
					if validCatalogTarget(link.Href, home) {
						count++
					}
				}
			}
		}
		_, hasAPIDescription := set["service-desc"]
		_, hasDocumentation := set["service-doc"]
		_, hasStatus := set["status"]
		_, hasMetadata := set["service-meta"]
		if raw, ok := set["anchor"]; ok && (hasAPIDescription || hasDocumentation || hasStatus || hasMetadata) {
			var anchor string
			if json.Unmarshal(raw, &anchor) == nil && validCatalogTarget(anchor, home) {
				count++
			}
		}
	}
	if count == 0 {
		f.Status = "fail"
		f.Evidence = "The Linkset contains no valid API endpoint links in item relations or API anchors."
		return []reportmodel.Finding{f}
	}
	f.Status = "pass"
	f.Evidence = fmt.Sprintf("A valid RFC 9727 Linkset exposed %d API endpoint link(s).", count)
	return []reportmodel.Finding{f}
}

func hasAPISurface(doc *html.Node, home *url.URL) bool {
	return hasAuthSurface(doc, home) || func() bool {
		found := false
		walk(doc, func(n *html.Node) {
			if found || n.Type != html.ElementNode || (n.Data != "a" && n.Data != "link") {
				return
			}
			u, ok := readinessLink(attr(n, "href"), home)
			if !ok {
				return
			}
			path := u.Path
			label := attr(n, "rel") + " " + text(n)
			if pathSegment(path, "api", "apis", "developer", "developers", "docs", "documentation") ||
				strings.Contains(label, "api") || strings.Contains(label, "developer") || strings.Contains(label, "documentation") || attr(n, "rel") == "service-desc" {
				found = true
			}
		})
		return found
	}()
}

func validCatalogTarget(raw string, home *url.URL) bool {
	u, err := url.Parse(raw)
	return err == nil && u.IsAbs() && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}
