package scanner

import "github.com/is-optimized/is-optimized/internal/reportmodel"

// External provider measurements must remain explicitly unobserved until a
// supported, credentialed source is integrated. Page markup cannot prove them.
func externalProviderChecks(home string) []reportmodel.Finding {
	var out []reportmodel.Finding
	for _, check := range []struct{ id, title, reason string }{
		{"agent.discoverability.brand", "Brand search discoverability", "No supported search provider is configured; top-ten brand ranking was not measured."},
		{"agent.discoverability.docs", "Developer-resource search discoverability", "No supported search provider is configured; API/docs search placement was not measured."},
		{"agent.discoverability.entities", "Off-site entity references", "Wikipedia/Wikidata entity ownership was not verified against an external data source."},
		{"agent.discoverability.directory", "Agent directory listing", "No supported app-directory source is configured; a listing was not verified."},
		{"agent.mcp_registry.listing", "MCP registry listing", "No supported registry lookup is configured; homepage links do not prove registry registration."},
	} {
		out = append(out, reportmodel.Finding{ID: check.id, Pillar: "agent", Title: check.title, Status: "unknown", Weight: 1, Tier: "recommended", Stage: "find", URL: home, Evidence: check.reason, Applicability: "External discoverability requires independently observed provider evidence; local HTML is insufficient.", Remediation: "Verify the matching domain and product in a supported external search or registry source before making a discoverability claim."})
	}
	out = append(out, reportmodel.Finding{ID: "seo.web_vitals", Pillar: "seo", Title: "Browser performance metrics", Status: "unknown", Weight: 1, Tier: "recommended", URL: home, Evidence: "No PageSpeed or field-data source is configured. Browser LCP, INP and CLS were not measured; HTTP fetch timing is a separate observation.", Applicability: "Browser performance requires a browser or supported field-data provider.", Remediation: "Measure LCP, INP and CLS with a supported browser/field-data tool. Do not substitute server fetch duration.", Standards: []reportmodel.StandardReference{{ID: "Web Vitals", URL: "https://web.dev/articles/vitals", Coverage: "reference"}}})
	return out
}
