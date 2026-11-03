package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"golang.org/x/net/html"
	"gopkg.in/yaml.v3"
)

const agentSkillsSchemaV020 = "https://schemas.agentskills.io/discovery/0.2.0/schema.json"

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type apiProbeCandidate struct {
	url      string
	health   bool
	priority int
}

type apiProbeOutcome struct {
	candidate apiProbeCandidate
	response  response
	err       error
}

func apiSurfaceFindings(ss *session, home *url.URL, specURL string, applicable bool, document map[string]any) []reportmodel.Finding {
	base := func(id, title, tier string, weight float64, remediation string) reportmodel.Finding {
		return reportmodel.Finding{ID: id, Pillar: "agent", Title: title, Status: "unknown", Weight: weight, Tier: tier, Stage: "act", URL: specURL, Applicability: "Applies when a public API contract with safe unauthenticated GET operations is observed; only bounded read-only GET requests are sent without credentials.", Remediation: remediation, Standards: []reportmodel.StandardReference{{ID: "OpenAPI 3.1", URL: openAPISpecURL, Coverage: "partial"}, {ID: "RFC 9110", URL: "https://www.rfc-editor.org/rfc/rfc9110.html", Coverage: "partial"}}}
	}
	findings := []reportmodel.Finding{
		base("agent.api.reachability", "Public API reachability", "recommended", 2, "Document at least one safe unauthenticated GET endpoint in the OpenAPI contract, and return a documented JSON response."),
		base("agent.api.json_errors", "API JSON error responses", "essential", 2, "Return structured JSON errors for public GET failures; application/problem+json is preferred. Do not require credentials for public health or discovery endpoints."),
		base("agent.api.rate_limits", "API rate-limit headers", "recommended", 1, "Publish RateLimit and RateLimit-Policy response headers, or X-RateLimit-* compatibility headers, on API responses."),
		base("agent.api.onboarding", "Zero-auth API onboarding", "recommended", 1, "Provide an unauthenticated health or status GET that returns 200, and describe how users obtain credentials for protected operations."),
	}
	if !applicable {
		for i := range findings {
			findings[i].Status = "not_applicable"
			findings[i].Evidence = "No API or developer surface was observed; live API checks were not applicable."
		}
		return findings
	}
	candidates := selectAPIGetCandidates(home, specURL, document)
	if len(candidates) == 0 {
		for i := range findings {
			findings[i].Evidence = "An API/developer surface was observed, but no public, parameter-free GET operation could be safely selected from a valid OpenAPI contract. No API request was sent."
		}
		return findings
	}
	results := make([]apiProbeOutcome, len(candidates))
	var wg sync.WaitGroup
	for i, candidate := range candidates {
		wg.Add(1)
		go func(i int, candidate apiProbeCandidate) {
			defer wg.Done()
			headers := make(http.Header)
			headers.Set("Accept", "application/json, application/problem+json;q=0.9")
			r, err := ss.fetchRequest(candidate.url, true, "", headers, false, true)
			results[i] = apiProbeOutcome{candidate: candidate, response: r, err: err}
		}(i, candidate)
	}
	wg.Wait()

	observed := 0
	successful := 0
	protected := 0
	validJSON := 0
	problemJSON := 0
	errorResponses := 0
	jsonErrors := 0
	standardRateHeaders := false
	legacyRateHeaders := false
	health200 := false
	unauthenticated200 := false
	unauthChallenge := false
	var evidence []string
	for _, result := range results {
		if result.err != nil {
			evidence = append(evidence, result.candidate.url+": not observed ("+result.err.Error()+")")
			continue
		}
		r := result.response
		observed++
		mediaType, _, _ := mime.ParseMediaType(r.contentType)
		if r.code >= 200 && r.code < 300 {
			successful++
		}
		if (r.code == http.StatusUnauthorized || r.code == http.StatusForbidden) && r.code < 500 {
			protected++
		}
		if r.code >= 200 && r.code < 300 && strings.Contains(mediaType, "json") {
			var decoded any
			if json.Unmarshal(r.body, &decoded) == nil {
				validJSON++
			}
		}
		if r.code >= 400 {
			errorResponses++
			if mediaType == "application/problem+json" {
				problemJSON++
			}
			if strings.Contains(mediaType, "json") {
				var decoded any
				if json.Unmarshal(r.body, &decoded) == nil {
					jsonErrors++
				}
			}
		}
		if r.header.Get("RateLimit") != "" || r.header.Get("RateLimit-Policy") != "" {
			standardRateHeaders = true
		}
		if r.header.Get("X-RateLimit-Limit") != "" || r.header.Get("X-RateLimit-Remaining") != "" || r.header.Get("X-RateLimit-Reset") != "" {
			legacyRateHeaders = true
		}
		if result.candidate.health && r.code == http.StatusOK {
			health200 = true
		}
		if r.code == http.StatusOK {
			unauthenticated200 = true
		}
		challenge := strings.ToLower(r.header.Get("WWW-Authenticate"))
		if r.code == http.StatusUnauthorized && strings.Contains(challenge, "bearer") && strings.Contains(challenge, "resource_metadata=") {
			unauthChallenge = true
		}
		evidence = append(evidence, fmt.Sprintf("%s returned HTTP %d, Content-Type %q", result.candidate.url, r.code, r.contentType))
	}
	joinedEvidence := strings.Join(evidence, "; ")
	findings[0].Evidence = fmt.Sprintf("%d/%d selected GET probe(s) completed; %d returned 2xx and %d successful response(s) contained valid JSON. %s", observed, len(candidates), successful, validJSON, joinedEvidence)
	if observed == 0 {
		findings[0].Status = "unknown"
	} else if successful == 0 && protected > 0 {
		findings[0].Status = "partial"
	} else if successful == 0 {
		findings[0].Status = "fail"
	} else if validJSON == 0 {
		findings[0].Status = "partial"
	} else if validJSON < successful || successful < observed {
		findings[0].Status = "partial"
	} else {
		findings[0].Status = "pass"
	}
	findings[1].Evidence = fmt.Sprintf("Observed %d error response(s), %d with parseable JSON (including %d application/problem+json); authenticated challenge with resource metadata=%t. %s", errorResponses, jsonErrors, problemJSON, unauthChallenge, joinedEvidence)
	if errorResponses == 0 {
		findings[1].Status = "unknown"
		findings[1].Evidence = "No error response was observed from the safe GET probes; an error was not induced. " + joinedEvidence
	} else if jsonErrors == 0 {
		findings[1].Status = "fail"
	} else if jsonErrors < errorResponses || problemJSON == 0 {
		findings[1].Status = "partial"
	} else {
		findings[1].Status = "pass"
	}
	findings[2].Evidence = fmt.Sprintf("RateLimit/RateLimit-Policy observed=%t; X-RateLimit-* observed=%t. %s", standardRateHeaders, legacyRateHeaders, joinedEvidence)
	if observed == 0 {
		findings[2].Status = "unknown"
	} else if standardRateHeaders {
		findings[2].Status = "pass"
	} else if legacyRateHeaders {
		findings[2].Status = "partial"
	} else {
		findings[2].Status = "fail"
	}
	findings[3].Evidence = fmt.Sprintf("Unauthenticated health/status GET returned HTTP 200=%t; any safe unauthenticated GET returned HTTP 200=%t; Bearer resource_metadata challenge observed=%t. %s", health200, unauthenticated200, unauthChallenge, joinedEvidence)
	if observed == 0 {
		findings[3].Status = "unknown"
	} else if health200 {
		findings[3].Status = "pass"
	} else if unauthChallenge {
		findings[3].Status = "partial"
	} else if unauthenticated200 {
		findings[3].Status = "partial"
	} else {
		findings[3].Status = "fail"
	}
	return findings
}

func selectAPIGetCandidates(home *url.URL, specURL string, document map[string]any) []apiProbeCandidate {
	if home == nil || document == nil {
		return nil
	}
	spec, err := url.Parse(specURL)
	if err != nil || validateURL(spec) != nil {
		spec = home
	}
	servers := sliceValue(document["servers"])
	if len(servers) == 0 {
		servers = []any{map[string]any{"url": "/"}}
	}
	var roots []*url.URL
	for _, rawServer := range servers {
		raw := stringValue(mapValue(rawServer)["url"])
		if raw == "" || strings.Contains(raw, "{") || strings.Contains(raw, "}") {
			continue
		}
		serverURL, err := url.Parse(raw)
		if err != nil || serverURL.User != nil || serverURL.RawQuery != "" || serverURL.Fragment != "" {
			continue
		}
		serverURL = spec.ResolveReference(serverURL)
		if validateURL(serverURL) != nil || serverURL.Scheme != "https" {
			continue
		}
		roots = append(roots, serverURL)
	}
	if len(roots) == 0 {
		return nil
	}
	paths := mapValue(document["paths"])
	var found []apiProbeCandidate
	for pathValue, pathItemValue := range paths {
		path := stringValue(pathValue)
		if path == "" || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "{}?#") || len(path) > 256 {
			continue
		}
		lower := strings.ToLower(path)
		if strings.Contains(lower, "logout") || strings.Contains(lower, "delete") || strings.Contains(lower, "remove") || strings.Contains(lower, "unsubscribe") || strings.Contains(lower, "checkout") || strings.Contains(lower, "purchase") {
			continue
		}
		pathItem := mapValue(pathItemValue)
		operation := mapValue(pathItem["get"])
		if len(operation) == 0 || !openAPIPublicOperation(document, operation) {
			continue
		}
		if hasRequiredPathParameter(pathItem, operation) {
			continue
		}
		for _, root := range roots {
			target := *root
			target.Path = strings.TrimSuffix(root.Path, "/") + path
			if target.Path == "" {
				target.Path = "/"
			}
			if validateURL(&target) != nil {
				continue
			}
			priority := 2
			health := false
			if strings.Contains(lower, "health") || strings.Contains(lower, "status") || strings.Contains(lower, "ready") {
				priority = 0
				health = true
			} else if strings.Contains(lower, "version") || strings.Contains(lower, "ping") {
				priority = 1
			}
			found = append(found, apiProbeCandidate{url: target.String(), health: health, priority: priority})
			break
		}
	}
	if len(found) == 0 {
		return nil
	}
	// Stable priority plus path ordering makes the two-request probe deterministic.
	sortAPIProbeCandidates(found)
	out := make([]apiProbeCandidate, 0, 2)
	seen := map[string]bool{}
	for _, item := range found {
		if seen[item.url] {
			continue
		}
		seen[item.url] = true
		out = append(out, item)
		if len(out) == 2 {
			break
		}
	}
	return out
}

func sortAPIProbeCandidates(candidates []apiProbeCandidate) {
	// Kept as a helper so selection is deterministic without depending on JSON map order.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].priority == candidates[j].priority {
			return candidates[i].url < candidates[j].url
		}
		return candidates[i].priority < candidates[j].priority
	})
}

func openAPIPublicOperation(document, operation map[string]any) bool {
	security, exists := operation["security"]
	if !exists {
		security = document["security"]
	}
	if rules, ok := security.([]any); ok {
		if len(rules) == 0 {
			return true
		}
		for _, rule := range rules {
			if len(mapValue(rule)) == 0 {
				return true
			}
		}
		return false
	}
	return security == nil
}

func hasRequiredPathParameter(pathItem, operation map[string]any) bool {
	for _, set := range [][]any{sliceValue(pathItem["parameters"]), sliceValue(operation["parameters"])} {
		for _, value := range set {
			parameter := mapValue(value)
			if strings.EqualFold(stringValue(parameter["in"]), "path") && parameter["required"] == true {
				return true
			}
		}
	}
	return false
}

func developerSurfaceChecks(home *url.URL, page pageResult) []reportmodel.Finding {
	findings := []reportmodel.Finding{
		devFinding("agent.developer.portal", "Developer portal", 1, "Publish a first-party developer portal and link it from the homepage."),
		devFinding("agent.developer.cli", "Developer CLI", 1, "Publish documentation for an official CLI and link its package or installation guide."),
		devFinding("agent.developer.sdk", "Developer SDKs", 1, "Link first-party SDK packages from supported package registries and identify the matching product."),
		devFinding("agent.developer.sandbox", "API sandbox", 1, "Offer a public sandbox or test-environment guide for API developers."),
		devFinding("agent.developer.docs", "API documentation links", 1, "Link public API reference documentation from the homepage."),
		devFinding("agent.developer.github", "Developer source repository", 1, "Link the official source repository or GitHub organization from the developer documentation."),
	}
	if page.doc == nil || page.page.Error != "" {
		for i := range findings {
			findings[i].Status = "unknown"
			findings[i].Evidence = "Homepage HTML unavailable; developer links were not assessed."
		}
		return findings
	}
	links := make([]*url.URL, 0, 64)
	var labels []string
	walk(page.doc, func(n *html.Node) {
		if n.Type != html.ElementNode || (n.Data != "a" && n.Data != "link") {
			return
		}
		if u, ok := readinessLink(attr(n, "href"), home); ok {
			links = append(links, u)
			labels = append(labels, strings.ToLower(text(n)+" "+attr(n, "aria-label")+" "+attr(n, "title")))
		}
	})
	seen := make([]bool, 6)
	matched := make([][]string, 6)
	for i, u := range links {
		path := strings.ToLower(u.Path)
		host := strings.ToLower(u.Hostname())
		label := labels[i]
		if origin(u) == origin(home) && (pathSegment(path, "developers", "developer", "docs", "documentation", "api-reference", "api-docs") || strings.Contains(label, "developer portal")) {
			seen[0], seen[4] = true, true
			matched[0], matched[4] = append(matched[0], u.String()), append(matched[4], u.String())
		}
		if (strings.HasPrefix(host, "docs.") || strings.Contains(label, "documentation") || strings.Contains(label, "api docs")) && (pathSegment(path, "docs", "documentation", "api", "reference") || strings.HasPrefix(host, "docs.")) {
			seen[0], seen[4] = true, true
			matched[0], matched[4] = append(matched[0], u.String()), append(matched[4], u.String())
		}
		if strings.Contains(label, "cli") || strings.Contains(path, "cli") {
			seen[1] = true
			matched[1] = append(matched[1], u.String())
		}
		if host == "npmjs.com" || host == "www.npmjs.com" || host == "pypi.org" || host == "rubygems.org" || host == "crates.io" || host == "pkg.go.dev" {
			seen[2] = true
			matched[2] = append(matched[2], u.String())
		}
		if pathSegment(path, "sandbox", "playground", "test", "testing", "try-it") || strings.Contains(label, "sandbox") || strings.Contains(label, "playground") {
			seen[3] = true
			matched[3] = append(matched[3], u.String())
		}
		if host == "github.com" || strings.HasSuffix(host, ".github.com") {
			seen[5] = true
			matched[5] = append(matched[5], u.String())
		}
	}
	developerSurface := hasAPISurface(page.doc, home)
	for _, signal := range seen {
		developerSurface = developerSurface || signal
	}
	for i := range findings {
		if !developerSurface {
			findings[i].Status = "not_applicable"
			findings[i].Evidence = "No API, developer documentation, CLI, SDK, sandbox, or source-repository surface was observed; developer-specific discovery is not applicable."
			continue
		}
		findings[i].Evidence = "No qualifying developer link was observed on the sampled homepage."
		if seen[i] {
			urls := matched[i]
			findings[i].Evidence = fmt.Sprintf("Observed %d candidate link(s): %s. Link presence does not prove package ownership, maintenance, or feature quality.", len(urls), strings.Join(urls[:min(len(urls), 3)], "; "))
			findings[i].Status = "partial"
			if i == 0 || i == 4 || i == 5 {
				findings[i].Status = "pass"
			}
		} else {
			findings[i].Status = "fail"
		}
	}
	return findings
}

func devFinding(id, title string, weight float64, remediation string) reportmodel.Finding {
	return reportmodel.Finding{ID: id, Pillar: "agent", Title: title, Status: "unknown", Weight: weight, Tier: "recommended", Stage: "find", Applicability: "Applies when the site serves developers or provides a public API; this check observes first-party homepage links only and does not certify linked packages.", Remediation: remediation, Standards: []reportmodel.StandardReference{{ID: "Agent readiness developer surfaces", URL: AgentReadySpec, Coverage: "partial"}}}
}

func siteTypeCheck(home *url.URL, page pageResult) reportmodel.Finding {
	f := reportmodel.Finding{ID: "site.type", Pillar: "agent", Title: "Site type declaration", Status: "information", Stage: "find", URL: origin(home) + "/", Applicability: "Informs applicability of developer and commerce checks; it does not change scores by itself.", Remediation: `Declare a site type with <meta name="site:type" content="docs|business|app|store"> when inference is ambiguous.`}
	if page.doc == nil || page.page.Error != "" {
		f.Status = "unknown"
		f.Evidence = "Homepage HTML unavailable; site type was not inferred."
		return f
	}
	typeValue, source := inferSiteType(home, page)
	f.Evidence = fmt.Sprintf("Site type %q (%s). Inference is heuristic; an explicit site:type declaration overrides inferred signals.", typeValue, source)
	return f
}

func inferSiteType(home *url.URL, page pageResult) (string, string) {
	typeValue, source := "", "inferred"
	walk(page.doc, func(n *html.Node) {
		if typeValue != "" || n.Type != html.ElementNode || n.Data != "meta" || !strings.EqualFold(attr(n, "name"), "site:type") {
			return
		}
		candidate := strings.ToLower(strings.TrimSpace(attr(n, "content")))
		if candidate == "docs" || candidate == "business" || candidate == "app" || candidate == "store" {
			typeValue, source = candidate, "publisher declaration"
		}
	})
	if typeValue == "" {
		lower := strings.ToLower(initialText(page.doc))
		if strings.Contains(lower, "shopping cart") || strings.Contains(lower, "add to cart") || hasSchemaType(page.doc, "Product") || hasSchemaType(page.doc, "Offer") {
			typeValue = "store"
		} else if hasSchemaType(page.doc, "SoftwareApplication") || strings.Contains(lower, "sign in") || strings.Contains(lower, "dashboard") {
			typeValue = "app"
		} else if pathSegment(home.Path, "docs", "documentation", "guides") || strings.Contains(lower, "api reference") || strings.Contains(lower, "documentation") {
			typeValue = "docs"
		} else {
			typeValue = "business"
		}
	}
	return typeValue, source
}

func hasSchemaType(doc *html.Node, target string) bool {
	found := false
	walk(doc, func(n *html.Node) {
		if found || n.Type != html.ElementNode || n.Data != "script" || !strings.EqualFold(strings.TrimSpace(attr(n, "type")), "application/ld+json") {
			return
		}
		var data any
		if json.Unmarshal([]byte(jsonLDTextContent(n)), &data) == nil && jsonContainsType(data, target) {
			found = true
		}
	})
	return found
}

// JSON-LD must be parsed from the raw script text; the visible-text helper
// joins and normalizes text nodes, which can change JSON tokens.
func jsonLDTextContent(script *html.Node) string {
	var b strings.Builder
	for child := script.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			b.WriteString(child.Data)
		}
	}
	return b.String()
}

func jsonContainsType(value any, target string) bool {
	switch node := value.(type) {
	case map[string]any:
		if raw, ok := node["@type"]; ok {
			switch kind := raw.(type) {
			case string:
				if kind == target || strings.HasSuffix(kind, "/"+target) {
					return true
				}
			case []any:
				for _, item := range kind {
					if item == target || strings.HasSuffix(stringValue(item), "/"+target) {
						return true
					}
				}
			}
		}
		for _, child := range node {
			if jsonContainsType(child, target) {
				return true
			}
		}
	case []any:
		for _, child := range node {
			if jsonContainsType(child, target) {
				return true
			}
		}
	}
	return false
}

func commerceChecks(home *url.URL, page pageResult) []reportmodel.Finding {
	profile := reportmodel.Finding{ID: "agent.commerce.profile", Pillar: "agent", Title: "Commerce product and offer profile", Status: "unknown", Weight: 1, Tier: "recommended", Stage: "read", URL: origin(home) + "/", Applicability: "Applies to sites explicitly declared as stores or inferred to offer products for sale; checks public Product/Offer schema and links only.", Remediation: "For a storefront, publish truthful Product and Offer structured data with stable product identifiers, currency, availability, and a public product page.", Standards: []reportmodel.StandardReference{{ID: "Schema.org Product and Offer", URL: "https://schema.org/Product", Coverage: "partial"}}}
	checkout := reportmodel.Finding{ID: "agent.commerce.checkout", Pillar: "agent", Title: "Commerce path signals", Status: "information", Stage: "find", URL: origin(home) + "/", Applicability: "Informational only; the scanner never opens a cart, submits checkout data, or purchases anything.", Remediation: "Link product, pricing, cart, and checkout information clearly for human and agent readers."}
	if page.doc == nil || page.page.Error != "" {
		profile.Status, profile.Evidence = "unknown", "Homepage HTML unavailable; commerce signals were not evaluated."
		checkout.Status, checkout.Evidence = "unknown", profile.Evidence
		return []reportmodel.Finding{profile, checkout}
	}
	typeValue, source := inferSiteType(home, page)
	if typeValue != "store" {
		profile.Status = "not_applicable"
		profile.Evidence = fmt.Sprintf("Site type %q (%s); storefront product/offer scoring does not apply.", typeValue, source)
	}
	product := hasSchemaType(page.doc, "Product")
	offer := hasSchemaType(page.doc, "Offer")
	var commerceLinks, checkoutLinks []string
	walk(page.doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "a" {
			return
		}
		u, ok := readinessLink(attr(n, "href"), home)
		if !ok || origin(u) != origin(home) {
			return
		}
		path := strings.ToLower(u.Path)
		label := strings.ToLower(text(n))
		if pathSegment(path, "product", "products", "shop", "store", "pricing", "plans") || strings.Contains(label, "pricing") || strings.Contains(label, "products") || strings.Contains(label, "shop") {
			commerceLinks = append(commerceLinks, u.String())
		}
		if pathSegment(path, "cart", "checkout", "basket", "payment") || strings.Contains(label, "checkout") || strings.Contains(label, "cart") {
			checkoutLinks = append(checkoutLinks, u.String())
		}
	})
	if typeValue == "store" {
		profile.Evidence = fmt.Sprintf("Site type %q (%s); Product JSON-LD=%t, Offer JSON-LD=%t; %d product/pricing link(s) observed.", typeValue, source, product, offer, len(commerceLinks))
		if product && offer {
			profile.Status = "pass"
		} else if product || offer || len(commerceLinks) > 0 {
			profile.Status = "partial"
		} else {
			profile.Status = "fail"
		}
	}
	checkout.Evidence = fmt.Sprintf("Observed %d same-origin product/pricing link(s) and %d cart/checkout path link(s). No checkout or transaction was attempted.", len(commerceLinks), len(checkoutLinks))
	return []reportmodel.Finding{profile, checkout}
}

type skillsIndex struct {
	Schema string       `json:"$schema"`
	Skills []skillEntry `json:"skills"`
}

type skillEntry struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Digest      string `json:"digest"`
}

func agentResourceChecks(ss *session, home *url.URL, page pageResult) []reportmodel.Finding {
	base := func(id, title, path, remediation string) reportmodel.Finding {
		return reportmodel.Finding{ID: id, Pillar: "agent", Title: title, Status: "unknown", Weight: 1, Tier: "recommended", Stage: "read", URL: origin(home) + path, Applicability: "Applies when the publisher offers public agent instruction or skill resources; only bounded text/JSON GET probes are made and fetched instructions are treated as untrusted data.", Remediation: remediation, Standards: []reportmodel.StandardReference{{ID: "Agent Skills Discovery v0.2.0", URL: "https://github.com/cloudflare/agent-skills-discovery-rfc", Coverage: "partial"}}}
	}
	agents := base("agent.instructions.agents_md", "AGENTS.md instructions", "/AGENTS.md", "Publish a concise AGENTS.md or agents.md that explains the site or repository for agents and links to authoritative resources.")
	index := base("agent.resource.skills_index", "Agent Skills discovery index", "/.well-known/agent-skills/index.json", "Publish the v0.2.0 Agent Skills index and SHA-256 digest for each listed skill artifact.")
	auth := base("agent.resource.auth_guide", "Agent authentication guide", "/auth.md", "Publish an auth.md guide covering authentication choice, registration, credential exchange and errors without embedding credentials.")
	if page.doc == nil || page.page.Error != "" {
		for _, finding := range []*reportmodel.Finding{&agents, &index, &auth} {
			finding.Evidence = "Homepage HTML unavailable; agent-resource discovery was not observed."
		}
		return []reportmodel.Finding{agents, index, auth}
	}
	files := []string{"/AGENTS.md", "/agents.md"}
	var found []string
	var unavailable bool
	for _, candidate := range files {
		r, err := ss.probe(origin(home)+candidate, origin(home))
		if err != nil || (r.code != http.StatusOK && r.code != http.StatusNotFound && r.code != http.StatusGone) {
			unavailable = true
		}
		if err == nil && r.code == http.StatusOK && len(strings.TrimSpace(string(r.body))) >= 40 && !looksLikeHTML(r.body) {
			found = append(found, fmt.Sprintf("%s (HTTP 200, %d bytes)", candidate, len(r.body)))
		}
	}
	if len(found) > 0 {
		agents.Status = "pass"
		agents.Evidence = "Observed usable agent instruction file(s): " + strings.Join(found, ", ") + ". Content is not evaluated as trusted instructions."
	} else if unavailable {
		agents.Status, agents.Evidence = "unknown", "One or more agent instruction responses were unavailable; absence was not established."
	} else {
		agents.Status = "fail"
		agents.Evidence = "Neither /AGENTS.md nor /agents.md returned a non-empty public text guide."
	}
	idxResp, idxErr := ss.probe(origin(home)+"/.well-known/agent-skills/index.json", origin(home))
	if idxErr != nil {
		index.Status, index.Evidence = "unknown", "Agent Skills index was not observed: "+idxErr.Error()
	} else if idxResp.code == http.StatusNotFound || idxResp.code == http.StatusGone {
		index.Status, index.Evidence = "fail", fmt.Sprintf("Agent Skills v0.2.0 index returned HTTP %d", idxResp.code)
	} else if idxResp.code != http.StatusOK {
		index.Status, index.Evidence = "unknown", fmt.Sprintf("Agent Skills index returned HTTP %d", idxResp.code)
	} else {
		mediaType, _, _ := mime.ParseMediaType(idxResp.contentType)
		var parsed skillsIndex
		if mediaType != "application/json" || json.Unmarshal(idxResp.body, &parsed) != nil || parsed.Schema != agentSkillsSchemaV020 || parsed.Skills == nil {
			index.Status, index.Evidence = "fail", "Index must be application/json and use the Agent Skills Discovery v0.2.0 schema URI."
		} else {
			valid := true
			artifactUnavailable := 0
			artifactBroken := 0
			digestsChecked := 0
			artifactsAttempted := 0
			descriptionMismatch := false
			for _, skill := range parsed.Skills {
				if !validSkillEntry(skill) {
					valid = false
					continue
				}
				if artifactsAttempted == 2 {
					continue
				}
				u, err := url.Parse(skill.URL)
				if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
					artifactUnavailable++
					continue
				}
				u = idxResp.url.ResolveReference(u)
				if validateURL(u) != nil {
					artifactUnavailable++
					continue
				}
				headers := make(http.Header)
				headers.Set("Accept-Encoding", "identity")
				artifactsAttempted++
				artifact, err := ss.fetchRequest(u.String(), true, origin(u), headers, false, true)
				if err != nil {
					artifactUnavailable++
					continue
				}
				if artifact.code != http.StatusOK {
					if artifact.code == http.StatusNotFound || artifact.code == http.StatusGone {
						artifactBroken++
					} else {
						artifactUnavailable++
					}
					continue
				}
				digest := sha256.Sum256(artifact.body)
				if "sha256:"+hex.EncodeToString(digest[:]) != skill.Digest {
					artifactBroken++
					continue
				}
				if skill.Type == "skill-md" {
					artifactType, _, _ := mime.ParseMediaType(artifact.contentType)
					if artifactType != "text/markdown" && artifactType != "text/plain" {
						artifactBroken++
						continue
					}
					metadataValid, descriptionMatches := checkSkillMarkdown(artifact.body, skill.Name, skill.Description)
					if !metadataValid {
						artifactBroken++
						continue
					}
					descriptionMismatch = descriptionMismatch || !descriptionMatches
				}
				digestsChecked++
			}
			if !valid || artifactBroken > 0 {
				index.Status = "fail"
				index.Evidence = fmt.Sprintf("Index entry validation or %d artifact(s) failed availability/content/digest verification; %d digest(s) passed.", artifactBroken, digestsChecked)
			} else if digestsChecked == 0 {
				index.Status = "partial"
				index.Evidence = fmt.Sprintf("v0.2.0 index structure is valid but no artifacts could be verified; %d skill entries, %d unavailable URLs or fetches.", len(parsed.Skills), artifactUnavailable)
			} else if digestsChecked < len(parsed.Skills) {
				index.Status = "partial"
				index.Evidence = fmt.Sprintf("v0.2.0 index is structurally valid; SHA-256 verified %d of %d skill artifacts (sample cap 2, %d unavailable).", digestsChecked, len(parsed.Skills), artifactUnavailable)
			} else if descriptionMismatch {
				index.Status = "partial"
				index.Evidence = fmt.Sprintf("v0.2.0 index and all %d sampled artifact digests are valid, but a skill description differs from the SKILL.md frontmatter.", digestsChecked)
			} else if len(parsed.Skills) == 0 {
				index.Status = "partial"
				index.Evidence = "v0.2.0 index schema is valid but its skills array is empty."
			} else {
				index.Status = "pass"
				index.Evidence = fmt.Sprintf("v0.2.0 index is valid and all %d skill artifact SHA-256 digests were verified.", digestsChecked)
			}
		}
	}
	if page.doc == nil || !hasAuthSurface(page.doc, home) && !hasAPISurface(page.doc, home) {
		auth.Status = "not_applicable"
		auth.Evidence = "No authentication, API, or developer surface was observed; an auth guide is not applicable."
	} else if authResp, authErr := ss.probe(origin(home)+"/auth.md", origin(home)); authErr != nil {
		auth.Status, auth.Evidence = "unknown", "auth.md was not observed: "+authErr.Error()
	} else if authResp.code != http.StatusOK && authResp.code != http.StatusNotFound && authResp.code != http.StatusGone {
		auth.Status, auth.Evidence = "unknown", fmt.Sprintf("auth.md was not observed: HTTP %d", authResp.code)
	} else if authResp.code != http.StatusOK || looksLikeHTML(authResp.body) {
		auth.Status, auth.Evidence = "fail", fmt.Sprintf("/auth.md returned HTTP %d instead of a public Markdown guide", authResp.code)
	} else {
		lower := strings.ToLower(string(authResp.body))
		sections := []string{"register", "exchange", "credential", "error"}
		matches := 0
		for _, section := range sections {
			if strings.Contains(lower, section) {
				matches++
			}
		}
		auth.Evidence = fmt.Sprintf("/auth.md returned %d text bytes; %d/4 expected authentication walkthrough topics observed (register, exchange, credential use, errors).", len(authResp.body), matches)
		if matches == len(sections) {
			auth.Status = "pass"
		} else if matches > 0 {
			auth.Status = "partial"
		} else {
			auth.Status = "fail"
		}
	}
	return []reportmodel.Finding{agents, index, auth}
}

func validSkillEntry(skill skillEntry) bool {
	if !validSkillName(skill.Name) {
		return false
	}
	if skill.Type != "skill-md" && skill.Type != "archive" || strings.TrimSpace(skill.Description) == "" || utf8.RuneCountInString(skill.Description) > 1024 || strings.TrimSpace(skill.URL) == "" || !digestPattern.MatchString(skill.Digest) {
		return false
	}
	return true
}

func validSkillName(name string) bool {
	return len(name) > 0 && len(name) <= 64 && skillNamePattern.MatchString(name) && !strings.Contains(name, "--")
}

func checkSkillMarkdown(body []byte, name, description string) (bool, bool) {
	content := strings.ReplaceAll(string(body), "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return false, false
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return false, false
	}
	frontMatter := content[4 : 4+end]
	var metadata map[string]any
	if yaml.Unmarshal([]byte(frontMatter), &metadata) != nil {
		return false, false
	}
	frontName := stringValue(metadata["name"])
	frontDescription := stringValue(metadata["description"])
	valid := frontName == name && validSkillName(frontName) && frontDescription != "" && utf8.RuneCountInString(frontDescription) <= 1024 && strings.TrimSpace(content[4+end+4:]) != ""
	return valid, valid && frontDescription == description
}
