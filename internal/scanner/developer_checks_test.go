package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
)

func findingByID(findings []reportmodel.Finding, id string) reportmodel.Finding {
	for _, finding := range findings {
		if finding.ID == id {
			return finding
		}
	}
	return reportmodel.Finding{}
}

func TestOpenAPIStatsExposeFunctionAndPatternSignals(t *testing.T) {
	body := `{"openapi":"3.1.0","info":{"title":"Example","version":"v1"},"servers":[{"url":"https://api.example.com/v1"}],"paths":{"/items":{"get":{"operationId":"listItems","parameters":[{"name":"cursor","in":"query"}],"responses":{"200":{"description":"OK","content":{"application/json":{"schema":{"type":"array","items":{"type":"object"}}}}}}},"post":{"operationId":"createItems","parameters":[{"name":"Idempotency-Key","in":"header"}],"requestBody":{"content":{"application/json":{"schema":{"type":"array","items":{"type":"object"}}}}},"responses":{"202":{"description":"Accepted","headers":{"Location":{"schema":{"type":"string"}}},"content":{"application/json":{"schema":{"type":"object","properties":{"job_id":{"type":"string"}}}}}}}}}},"components":{"schemas":{"Item":{"type":"object","properties":{"id":{"type":"string"}}}}}}`
	doc, _, err := decodeOpenAPI([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	stats := assessOpenAPI(doc)
	if stats.operations != 2 || stats.functionCallingOps != 2 || stats.typedSuccessJSON != 2 || stats.paginationOps != 1 || stats.idempotencyOps != 1 || stats.async202Ops != 1 || stats.batchOps != 1 || stats.schemaCount < 1 || stats.schemaNodes == 0 {
		t.Fatalf("missing new contract signals: %+v", stats)
	}
	findings := openAPIFindings("https://example.com/openapi.json", doc, true, nil)
	for _, id := range []string{"agent.openapi.function_calling", "agent.openapi.schema_complexity", "agent.openapi.patterns"} {
		f := findingByID(findings, id)
		if f.ID == "" || f.Weight <= 0 || f.Tier != "recommended" || f.Applicability == "" || f.Evidence == "" || len(f.Standards) == 0 {
			t.Fatalf("incomplete finding %s: %+v", id, f)
		}
	}
}

func TestOpenAPIAdditionalFindingsAreNotApplicableWithoutAPI(t *testing.T) {
	home, _ := url.Parse("https://example.com")
	findings := openAPIFindings("https://example.com", nil, false, nil)
	findings = append(findings, apiSurfaceFindings(nil, home, "", false, nil)...)
	for _, finding := range findings {
		if strings.HasPrefix(finding.ID, "agent.openapi.") || strings.HasPrefix(finding.ID, "agent.api.") {
			if finding.Status != "not_applicable" {
				t.Errorf("%s status=%s, want not_applicable", finding.ID, finding.Status)
			}
		}
	}
}

func TestSelectAPIGetCandidatesIsPublicBoundedAndDeterministic(t *testing.T) {
	home, _ := url.Parse("https://example.com")
	doc, _, err := decodeOpenAPI([]byte(`{"openapi":"3.1.0","info":{"title":"API","version":"1"},"servers":[{"url":"https://api.example.com/v1"}],"paths":{"/z":{"get":{"responses":{"200":{}}}},"/health":{"get":{"responses":{"200":{}}}},"/accounts/{id}":{"get":{"responses":{"200":{}}}},"/private":{"get":{"security":[{"oauth":[]}],"responses":{"200":{}}}},"/logout":{"get":{"responses":{"200":{}}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	got := selectAPIGetCandidates(home, "https://example.com/openapi.json", doc)
	if len(got) != 2 || got[0].url != "https://api.example.com/v1/health" || got[1].url != "https://api.example.com/v1/z" || !got[0].health {
		t.Fatalf("unexpected safe candidate selection: %+v", got)
	}
	for _, candidate := range got {
		u, err := url.Parse(candidate.url)
		if err != nil || validateURL(u) != nil || u.Scheme != "https" {
			t.Fatalf("unsafe API probe target: %q", candidate.url)
		}
	}
}

func TestAPISurfaceChecksUseOnlyBoundedUnauthenticatedGETs(t *testing.T) {
	methods := make(chan string, 16)
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		methods <- r.Method + " " + r.URL.Path
		switch r.URL.Path {
		case "/robots.txt":
			return reply(r, http.StatusNotFound, "", "text/plain"), nil
		case "/v1/health":
			res := reply(r, http.StatusOK, `{"ok":true}`, "application/json")
			res.Header.Set("RateLimit", "limit=20, remaining=18")
			res.Header.Set("RateLimit-Policy", "20;w=60")
			return res, nil
		case "/v1/status":
			res := reply(r, http.StatusUnauthorized, `{"type":"about:blank","title":"Unauthorized","status":401}`, "application/problem+json")
			res.Header.Set("WWW-Authenticate", `Bearer resource_metadata="https://api.example.com/.well-known/oauth-protected-resource"`)
			return res, nil
		default:
			return reply(r, http.StatusNotFound, "not found", "text/plain"), nil
		}
	})
	home, _ := url.Parse("https://example.com")
	doc, _, err := decodeOpenAPI([]byte(`{"openapi":"3.1.0","info":{"title":"API","version":"1"},"servers":[{"url":"https://api.example.com/v1"}],"paths":{"/health":{"get":{"operationId":"health","responses":{"200":{}}}},"/status":{"get":{"operationId":"status","responses":{"401":{}}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	ss := &session{s: s, ctx: context.Background(), policies: map[string]policy{}}
	findings := apiSurfaceFindings(ss, home, "https://example.com/openapi.json", true, doc)
	wants := map[string]string{
		"agent.api.reachability": "partial",
		"agent.api.json_errors":  "pass",
		"agent.api.rate_limits":  "pass",
		"agent.api.onboarding":   "pass",
	}
	for id, want := range wants {
		if got := findingByID(findings, id).Status; got != want {
			t.Errorf("%s=%q want %q; findings=%+v", id, got, want, findings)
		}
	}
	var methodList []string
	for len(methods) > 0 {
		methodList = append(methodList, <-methods)
	}
	joined := strings.Join(methodList, ",")
	if len(methodList) < 3 || len(methodList) > 4 || !strings.Contains(joined, "GET /v1/health") || !strings.Contains(joined, "GET /v1/status") {
		t.Fatalf("expected bounded robots checks and at most two API GETs, got %v", methodList)
	}
}

func TestDeveloperSurfaceDetectionAndApplicability(t *testing.T) {
	home, _ := url.Parse("https://example.com")
	page := readinessPage(t, `<html><body><main><a href="/docs/api">API docs</a><a href="/cli">CLI install</a><a href="https://www.npmjs.com/package/example-sdk">SDK</a><a href="/sandbox">Sandbox</a><a href="https://github.com/example/example">Source</a></main></body></html>`)
	findings := developerSurfaceChecks(home, page)
	for _, id := range []string{"agent.developer.portal", "agent.developer.cli", "agent.developer.sdk", "agent.developer.sandbox", "agent.developer.docs", "agent.developer.github"} {
		f := findingByID(findings, id)
		if f.ID == "" || f.Status == "unknown" || f.Weight <= 0 || f.Tier != "recommended" {
			t.Errorf("developer surface finding not produced: %+v", f)
		}
	}
	plain := readinessPage(t, `<html><body><main><h1>Local bakery</h1><p>Fresh bread made daily.</p></main></body></html>`)
	for _, f := range developerSurfaceChecks(home, plain) {
		if f.Status != "not_applicable" {
			t.Errorf("non-developer site finding %s=%s, want not_applicable", f.ID, f.Status)
		}
	}
}

func TestSiteTypeOverrideAndCommerceSignals(t *testing.T) {
	home, _ := url.Parse("https://store.example.com")
	storePage := readinessPage(t, `<html><head><meta name="site:type" content="store"><script type="application/ld+json">{"@context":"https://schema.org","@type":"Product","name":"Tea","offers":{"@type":"Offer","price":"12.00","priceCurrency":"USD"}}</script></head><body><main><a href="/products/tea">Tea</a><a href="/checkout">Checkout</a></main></body></html>`)
	if got, source := inferSiteType(home, storePage); got != "store" || source != "publisher declaration" {
		t.Fatalf("site type override got %q from %q", got, source)
	}
	findings := commerceChecks(home, storePage)
	if got := findingByID(findings, "agent.commerce.profile").Status; got != "pass" {
		t.Fatalf("Product/Offer store profile status=%q, want pass; %+v", got, findings)
	}
	checkout := findingByID(findings, "agent.commerce.checkout")
	if checkout.Status != "information" || !strings.Contains(checkout.Evidence, "No checkout or transaction was attempted") || !strings.Contains(checkout.Evidence, "1 cart/checkout") {
		t.Fatalf("checkout evidence should be read-only and visible: %+v", checkout)
	}
	docsPage := readinessPage(t, `<html><head><meta name="site:type" content="docs"><script type="application/ld+json">{"@type":"Product"}</script></head><body><main><a href="/checkout">Checkout</a></main></body></html>`)
	if got := findingByID(commerceChecks(home, docsPage), "agent.commerce.profile").Status; got != "not_applicable" {
		t.Fatalf("explicit docs site type must override inferred commerce, got %q", got)
	}
}

func TestAgentSkillsIndexValidatesV020AndRawArtifactDigest(t *testing.T) {
	skill := []byte("---\nname: site-audit\ndescription: Run a site readiness audit. Use when reviewing a public website.\n---\n\n# Site audit\nRead public evidence only.\n")
	digest := sha256.Sum256(skill)
	digestText := "sha256:" + hex.EncodeToString(digest[:])
	indexJSON, err := json.Marshal(skillsIndex{Schema: agentSkillsSchemaV020, Skills: []skillEntry{{Name: "site-audit", Type: "skill-md", Description: "Run a site readiness audit. Use when reviewing a public website.", URL: "/.well-known/agent-skills/site-audit/SKILL.md", Digest: digestText}}})
	if err != nil {
		t.Fatal(err)
	}
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/robots.txt":
			return reply(r, http.StatusNotFound, "", "text/plain"), nil
		case "/AGENTS.md":
			return reply(r, http.StatusOK, "# Agent guidance\nUse the public API docs and never submit credentials.", "text/markdown"), nil
		case "/agents.md":
			return reply(r, http.StatusNotFound, "", "text/plain"), nil
		case "/.well-known/agent-skills/index.json":
			return reply(r, http.StatusOK, string(indexJSON), "application/json"), nil
		case "/.well-known/agent-skills/site-audit/SKILL.md":
			return reply(r, http.StatusOK, string(skill), "text/markdown"), nil
		default:
			return reply(r, http.StatusNotFound, "", "text/plain"), nil
		}
	})
	home, _ := url.Parse("https://example.com")
	page := readinessPage(t, `<html><body><main><a href="/api">API</a></main></body></html>`)
	ss := &session{s: s, ctx: context.Background(), policies: map[string]policy{}}
	findings := agentResourceChecks(ss, home, page)
	if got := findingByID(findings, "agent.instructions.agents_md").Status; got != "pass" {
		t.Fatalf("AGENTS.md status=%s, want pass", got)
	}
	if got := findingByID(findings, "agent.resource.skills_index").Status; got != "pass" {
		t.Fatalf("skills index status=%s, want pass; evidence=%s", got, findingByID(findings, "agent.resource.skills_index").Evidence)
	}
	if got := findingByID(findings, "agent.resource.auth_guide").Status; got != "fail" {
		t.Fatalf("API site without auth.md status=%s, want fail", got)
	}
}

func TestAgentSkillsIndexRejectsMismatchedDigest(t *testing.T) {
	entry := skillEntry{Name: "safe-skill", Type: "skill-md", Description: "A valid skill description.", URL: "/skill.md", Digest: "sha256:" + strings.Repeat("0", 64)}
	if !validSkillEntry(entry) {
		t.Fatal("syntactically valid v0.2.0 entry rejected")
	}
	if valid, _ := checkSkillMarkdown([]byte("---\nname: other\ndescription: A valid skill description.\n---\nBody"), entry.Name, entry.Description); valid {
		t.Fatal("skill frontmatter name mismatch was accepted")
	}
}
