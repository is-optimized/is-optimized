package scanner

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"golang.org/x/net/html"
)

// AgentReadySpec pins the practice inventory used by this methodology. A mapped
// observation covers only the stated heuristic, never the entire practice.
const AgentReadySpec = "https://github.com/agentready-org/standard/blob/273d2d7c991f30cd0785415023801b03766abc67/spec/spec.md"

type ReadinessCheck struct {
	ID, Stage, Title, Applicability, Remediation string
	Standards                                    []reportmodel.StandardReference
}

func references(coverage string, ids ...string) []reportmodel.StandardReference {
	out := make([]reportmodel.StandardReference, 0, len(ids))
	for _, id := range ids {
		out = append(out, reportmodel.StandardReference{ID: id, URL: AgentReadySpec, Coverage: coverage})
	}
	return out
}

// ReadinessRegistry is the complete zero-weight readiness observation inventory.
// Missing optional interfaces never reduce a pillar score. Product intent and
// actual task completion cannot be determined from linked URLs alone.
func ReadinessRegistry() []ReadinessCheck {
	return []ReadinessCheck{
		{ID: "agent.find.docs", Stage: "find", Title: "Linked documentation", Applicability: "Relevant when the site publishes documentation; same-origin links are candidates, not proof of answer quality.", Remediation: "Link useful first-party documentation from pages visitors already reach.", Standards: references("partial", "AR-FIND-02", "AR-READ-03", "AR-READ-04")},
		{ID: "agent.read.pricing", Stage: "read", Title: "Linked pricing information", Applicability: "Relevant when the product publishes prices; absence does not establish whether pricing applies.", Remediation: "If you publish prices, link a first-party page with plans, limits, and prices in its initial HTML.", Standards: references("partial", "AR-READ-11")},
		{ID: "agent.find.resources", Stage: "find", Title: "Linked machine-readable resources", Applicability: "Relevant to resources the site actually offers; conventional filenames are not universal requirements.", Remediation: "Link any useful llms.txt, AI catalog, OpenAPI, agent metadata, or markdown resources from public pages.", Standards: references("partial", "AR-FIND-04", "AR-READ-06", "AR-READ-07", "AR-READ-09")},
		{ID: "agent.act.openapi", Stage: "act", Title: "Declared API contract links", Applicability: "Relevant when a public API exists; finding a link does not validate its contract or endpoints.", Remediation: "If a public API exists, link its maintained OpenAPI contract and document responses, errors, and limits.", Standards: references("reference", "AR-ACT-02")},
		{ID: "agent.act.auth", Stage: "act", Title: "Declared authentication links", Applicability: "Relevant when actions require authentication; no authentication or delegation was attempted.", Remediation: "For authenticated products, document keys, scopes, and supported user-delegation flows.", Standards: references("reference", "AR-ACT-01")},
		{ID: "agent.act.interfaces", Stage: "act", Title: "Declared agent interface links", Applicability: "Relevant when agents should perform tasks; MCP, CLI, NLWeb, and agent identity files are context-dependent.", Remediation: "Link the interfaces you actually support. Verify their capabilities and permissions in a separately authorized task evaluation.", Standards: references("reference", "AR-ACT-03", "AR-ACT-06", "AR-ACT-09", "AR-ACT-10")},
		{ID: "agent.read.facts", Stage: "read", Title: "Structured facts and initial text", Applicability: "Relevant when JSON-LD contains business or product facts; exact text matching cannot establish truth or semantic equivalence.", Remediation: "Repeat critical verified facts in readable page text; do not rely solely on JSON-LD or hydration data.", Standards: references("partial", "AR-READ-08")},
	}
}

func readinessObservations(p pageResult) []reportmodel.Finding {
	registry := ReadinessRegistry()
	out := make([]reportmodel.Finding, 0, len(registry))
	groups := map[string]map[string]bool{}
	for _, c := range registry {
		groups[c.ID] = map[string]bool{}
	}
	base, _ := url.Parse(p.page.URL)
	var structured []string
	if p.page.Error == "" && p.doc != nil {
		walk(p.doc, func(n *html.Node) {
			if n.Type != html.ElementNode {
				return
			}
			if n.Data == "script" && strings.EqualFold(strings.TrimSpace(attr(n, "type")), "application/ld+json") && len(structured) < 64 {
				var data any
				if json.Unmarshal([]byte(text(n)), &data) == nil {
					collectFacts(data, &structured)
				}
			}
			if n.Data != "a" && n.Data != "link" {
				return
			}
			u, ok := readinessLink(attr(n, "href"), base)
			if !ok {
				return
			}
			path := strings.ToLower(u.Path)
			add := func(id string) {
				if len(groups[id]) < 128 {
					groups[id][u.String()] = true
				}
			}
			if origin(u) == origin(base) {
				if pathSegment(path, "docs", "documentation", "developers", "developer", "guides", "guide", "help", "support") {
					add("agent.find.docs")
				}
				if pathSegment(path, "pricing", "plans") {
					add("agent.read.pricing")
				}
			}
			if strings.HasSuffix(path, "/llms.txt") || strings.HasSuffix(path, "/llms-full.txt") || strings.HasPrefix(path, "/.well-known/") || strings.HasSuffix(path, "/agents.md") || strings.HasSuffix(path, ".md") || strings.EqualFold(attr(n, "type"), "text/markdown") || strings.EqualFold(attr(n, "rel"), "ai-catalog") {
				add("agent.find.resources")
			}
			if strings.HasSuffix(path, "/openapi.json") || strings.HasSuffix(path, "/openapi.yaml") || strings.HasSuffix(path, "/openapi.yml") || strings.HasSuffix(path, "/swagger.json") || strings.EqualFold(attr(n, "type"), "application/vnd.oai.openapi+json") || strings.EqualFold(attr(n, "type"), "application/vnd.oai.openapi") {
				add("agent.act.openapi")
				add("agent.find.resources")
			}
			if pathSegment(path, "auth", "authentication", "oauth", "login", "signin", "api-keys") || strings.Contains(path, "/.well-known/oauth-") || strings.HasSuffix(path, "/.well-known/openid-configuration") {
				add("agent.act.auth")
			}
			if pathSegment(path, "mcp", "cli") || path == "/ask" || strings.HasSuffix(path, "/agent-card.json") || strings.HasSuffix(path, "/agent.json") || strings.HasSuffix(path, "/agentfacts.json") || strings.HasSuffix(path, "/agentfacts") {
				add("agent.act.interfaces")
			}
		})
	}
	for _, c := range registry {
		f := reportmodel.Finding{ID: c.ID, Pillar: "agent", Title: c.Title, URL: p.page.URL, Status: "information", Stage: c.Stage, Applicability: c.Applicability, Standards: c.Standards, Remediation: c.Remediation}
		if p.page.Error != "" || p.doc == nil {
			f.Status = "unknown"
			f.Evidence = "Page content unavailable; linked resources and facts were not assessed."
		} else if c.ID == "agent.read.facts" {
			f.EvidenceLevel = "observed_html"
			normalized := strings.ToLower(initialText(p.doc))
			matched := 0
			for _, fact := range structured {
				if strings.Contains(normalized, strings.ToLower(fact)) {
					matched++
				}
			}
			f.Evidence = fmt.Sprintf("Inspected %d JSON-LD name, description, or price values (maximum 64); %d also appear as exact text outside script/style/hidden markup. CSS rendering, critical-fact selection, accuracy, and semantic equivalence were not evaluated.", len(structured), matched)
		} else {
			links := sortedLinks(groups[c.ID])
			f.EvidenceLevel = "observed_html"
			f.Evidence = "No matching link observed in this sampled page. Product applicability is not established; no score penalty."
			if len(links) > 0 {
				f.EvidenceLevel = "discovered_interface"
				f.Evidence = fmt.Sprintf("%d matching link(s) observed; destinations were not fetched or validated: %s", len(links), strings.Join(links[:min(len(links), 3)], "; "))
			}
		}
		f.Evidence = excerpt(f.Evidence)
		out = append(out, f)
	}
	return out
}

// Links are declarations only. Reject credentials, query strings, fragments and
// unsafe/private literal targets before retaining an example. Never resolve DNS
// or fetch a discovered endpoint here. The caller's fetcher owns network safety.
func readinessLink(raw string, base *url.URL) (*url.URL, bool) {
	if base == nil || strings.TrimSpace(raw) == "" {
		return nil, false
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, false
	}
	u = base.ResolveReference(u)
	if validateURL(u) != nil {
		return nil, false
	}
	return u, true
}

func pathSegment(path string, segments ...string) bool {
	for _, part := range strings.Split(strings.Trim(path, "/"), "/") {
		for _, segment := range segments {
			if part == segment {
				return true
			}
		}
	}
	return false
}

func sortedLinks(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for v := range values {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func initialText(root *html.Node) string {
	var b strings.Builder
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "head" || n.Data == "script" || n.Data == "style" || n.Data == "template" || strings.EqualFold(attr(n, "aria-hidden"), "true") {
				return
			}
			for _, a := range n.Attr {
				if a.Key == "hidden" {
					return
				}
			}
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(root)
	return strings.Join(strings.Fields(b.String()), " ")
}

func collectFacts(value any, out *[]string) {
	// JSON input already shares the response byte bound. Cap traversed nodes and
	// retained values independently to keep this informational analysis bounded.
	stack := []any{value}
	for visited := 0; len(stack) > 0 && visited < 2048 && len(*out) < 64; visited++ {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch v := v.(type) {
		case map[string]any:
			// Sorted keys make capped extraction deterministic for identical input.
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if len(*out) >= 64 {
					break
				}
				if k == "name" || k == "description" || k == "price" {
					s := ""
					switch item := v[k].(type) {
					case string:
						s = strings.Join(strings.Fields(item), " ")
					case float64:
						s = fmt.Sprint(item)
					}
					if s != "" && len(s) <= 300 {
						*out = append(*out, s)
					}
				}
				if len(stack) < 2048 {
					stack = append(stack, v[k])
				}
			}
		case []any:
			for i := len(v) - 1; i >= 0 && len(stack) < 2048; i-- {
				stack = append(stack, v[i])
			}
		}
	}
}

func annotateReadiness(findings []reportmodel.Finding) {
	for i := range findings {
		f := &findings[i]
		if f.Stage == "" {
			f.Stage = "read"
		}
		if f.ID == "seo.index" || f.ID == "seo.sitemap" || strings.HasPrefix(f.ID, "agent.discovery") {
			f.Stage = "find"
		}
		if f.Applicability == "" {
			f.Applicability = "Assesses only the selected public page and this check's documented conditions."
			if f.Status == "not_applicable" {
				f.Applicability = "The page does not contain the content or controls to which this check applies."
			}
		}
		if f.Status != "unknown" && f.EvidenceLevel == "" && f.ID != "seo.sitemap" && !strings.HasPrefix(f.ID, "agent.discovery") {
			switch f.ID {
			case "seo.access", "geo.access", "seo.index", "seo.hsts", "seo.page_status":
				f.EvidenceLevel = "observed_response"
			default:
				f.EvidenceLevel = "observed_html"
			}
		}
		if len(f.Standards) > 0 {
			continue
		}
		switch f.ID {
		case "agent.access", "geo.access":
			f.Standards = references("partial", "AR-READ-01")
		case "geo.entity":
			f.Standards = references("partial", "AR-READ-08")
		case "seo.access":
			f.Standards = references("partial", "AR-READ-02")
		case "seo.sitemap":
			f.Standards = references("partial", "AR-FIND-03")
		case "agent.discovery/llms.txt":
			f.Standards = references("partial", "AR-READ-07")
		}
	}
}
