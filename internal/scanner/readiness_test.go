package scanner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"golang.org/x/net/html"
)

func readinessPage(t *testing.T, body string) pageResult {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return pageResult{page: reportmodel.Page{URL: "https://example.com/", StatusCode: 200}, doc: doc}
}

func readinessByID(t *testing.T, findings []reportmodel.Finding, id string) reportmodel.Finding {
	t.Helper()
	for _, f := range findings {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("finding %s missing", id)
	return reportmodel.Finding{}
}

func TestReadinessLinkObservationsAreDeclarations(t *testing.T) {
	p := readinessPage(t, `<title>Example</title><a href="/docs/start">Docs</a><a href="/pricing">Pricing</a><a href="https://other.example/docs">External docs</a><link rel="alternate" href="/llms.txt"><link rel="ai-catalog" href="/.well-known/ai-catalog.json"><link type="application/vnd.oai.openapi+json" href="/contracts/api"><a href="/docs/authentication">Auth</a><a href="https://api.example.com/mcp">MCP</a>`)
	findings := readinessObservations(p)
	for _, id := range []string{"agent.find.docs", "agent.read.pricing", "agent.find.resources", "agent.act.openapi", "agent.act.auth", "agent.act.interfaces"} {
		f := readinessByID(t, findings, id)
		if f.Status != "information" || f.Weight != 0 || f.EvidenceLevel != "discovered_interface" {
			t.Errorf("%s incorrectly asserts validation: %+v", id, f)
		}
		if f.Stage == "" || f.Applicability == "" || len(f.Standards) == 0 {
			t.Errorf("%s missing provenance", id)
		}
		if !strings.Contains(f.Evidence, "not fetched or validated") {
			t.Errorf("%s lacks declaration limit: %s", id, f.Evidence)
		}
	}
	docs := readinessByID(t, findings, "agent.find.docs")
	if !strings.Contains(docs.Evidence, "https://example.com/docs/start") || strings.Contains(docs.Evidence, "other.example") {
		t.Fatalf("docs scope/resolution wrong: %s", docs.Evidence)
	}
	for _, f := range findings {
		for _, ref := range f.Standards {
			if ref.URL != AgentReadySpec || (ref.Coverage != "partial" && ref.Coverage != "reference") {
				t.Fatalf("unqualified standards mapping: %+v", ref)
			}
		}
	}
}

func TestReadinessAbsentOptionalSurfacesDoNotPenalizeScores(t *testing.T) {
	p := readinessPage(t, `<title>Example</title><main><h1>About</h1><p>A simple public informational page with no account, API, payment service, or agent product.</p></main>`)
	findings := evaluate(p)
	var weighted []reportmodel.Finding
	for _, f := range findings {
		if f.Weight > 0 {
			weighted = append(weighted, f)
		}
	}
	if !reflect.DeepEqual(score(findings), score(weighted)) {
		t.Fatal("zero-weight readiness observations changed scores")
	}
	for _, f := range readinessObservations(p) {
		if f.Status != "information" || f.Weight != 0 || f.Foundational {
			t.Fatalf("missing optional surface penalized site: %+v", f)
		}
		if f.EvidenceLevel != "observed_html" {
			t.Fatalf("absent resource claimed as discovered: %+v", f)
		}
	}
}

func TestReadinessFactsDoNotConfuseJSONLDWithInitialText(t *testing.T) {
	markup := `<script type="application/ld+json">{"@type":"Product","name":"Schema Only Name","description":"Private Payload","offers":{"price":"19.99"}}</script><p>19.99</p><span hidden>Schema Only Name</span><div aria-hidden="true">Private Payload</div>`
	f := readinessByID(t, readinessObservations(readinessPage(t, markup)), "agent.read.facts")
	if f.Status != "information" || !strings.Contains(f.Evidence, "Inspected 3 JSON-LD") || !strings.Contains(f.Evidence, "; 1 also appear") {
		t.Fatalf("structured-only facts counted as readable text: %s", f.Evidence)
	}
	markup += `<p>Schema Only Name</p><p>Private Payload</p>`
	f = readinessByID(t, readinessObservations(readinessPage(t, markup)), "agent.read.facts")
	if !strings.Contains(f.Evidence, "; 3 also appear") {
		t.Fatalf("initial facts not recognized: %s", f.Evidence)
	}
	if !strings.Contains(f.Evidence, "accuracy") {
		t.Fatal("fact presence presented as truth")
	}
}

func TestReadinessUnavailablePageHasNoObservedEvidenceLevel(t *testing.T) {
	p := pageResult{page: reportmodel.Page{URL: "https://example.com/", StatusCode: 403, Error: "Blocked"}}
	for _, f := range evaluate(p) {
		if f.Status != "unknown" || f.EvidenceLevel != "" {
			t.Fatalf("unavailable content claimed observed: %+v", f)
		}
		if f.Stage == "" || f.Applicability == "" {
			t.Fatalf("unknown lacks scope metadata: %+v", f)
		}
	}
}

func TestReadinessResponseProvenanceIsDistinctFromHTML(t *testing.T) {
	p := readinessPage(t, `<title>Example</title><main><h1>Public information</h1><p>Useful public content.</p></main>`)
	findings := evaluate(p)
	for _, id := range []string{"seo.access", "geo.access", "seo.index", "seo.hsts"} {
		if f := readinessByID(t, findings, id); f.EvidenceLevel != "observed_response" {
			t.Errorf("%s response evidence mislabeled: %+v", id, f)
		}
	}
	if f := readinessByID(t, findings, "seo.title"); f.EvidenceLevel != "observed_html" {
		t.Errorf("title HTML evidence mislabeled: %+v", f)
	}
}

func TestReadinessRejectsUnsafeOrCredentialBearingLinks(t *testing.T) {
	base, _ := url.Parse("https://example.com/docs/")
	for _, raw := range []string{"javascript:alert(1)", "data:text/html,evil", "file:///etc/passwd", "https://user:secret@example.com/openapi.json", "https://127.0.0.1/openapi.json", "https://169.254.169.254/openapi.json", "https://example.com/openapi.json?token=secret", "https://example.com/openapi.json#secret", "://"} {
		if u, ok := readinessLink(raw, base); ok {
			t.Errorf("unsafe declaration retained: %s", u)
		}
	}
	u, ok := readinessLink("../openapi.json", base)
	if !ok || u.String() != "https://example.com/openapi.json" {
		t.Fatalf("valid relative reference lost: %v", u)
	}
}

func TestReadinessAddsNoInterfaceRequestsAndRecordsNotRun(t *testing.T) {
	var requested []string
	s := fixtureScanner(roundTrip(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.Path)
		if r.URL.Path == "/robots.txt" {
			return reply(r, 200, "User-agent: *\nAllow: /", "text/plain"), nil
		}
		if r.URL.Path == "/" {
			return reply(r, 200, `<title>Example</title><main>Enough public words to describe this simple informational website with several useful resources for readers and agents to discover in initial HTML.</main><link href="/openapi.json"><link href="/.well-known/oauth-authorization-server"><link href="/mcp"><link href="/llms.txt">`, "text/html"), nil
		}
		return reply(r, 404, "Not found", "text/plain"), nil
	}))
	report, err := s.Scan(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range requested {
		if request != "/robots.txt" && request != "/" && request != "/llms.txt" && request != "/llms-full.txt" {
			t.Fatalf("discovery declaration caused network request: %s", request)
		}
	}
	if report.BehavioralEvaluation != "not_run" || report.MethodologyVersion != "2026-10-agentready-v3" {
		t.Fatalf("new methodology/task provenance missing: %+v", report)
	}
	if report.SelectionPolicy != "home-links-sitemap-two-hop-v2" {
		t.Fatalf("selection policy not versioned: %s", report.SelectionPolicy)
	}
	f := readinessByID(t, report.Findings, "agent.access")
	if f.Stage != "read" || f.EvidenceLevel != "observed_html" || len(f.Standards) != 1 || f.Standards[0].Coverage != "partial" {
		t.Fatalf("existing weighted check metadata missing: %+v", f)
	}
	b, err := json.Marshal(report)
	if err != nil || !strings.Contains(string(b), `"behavioral_evaluation":"not_run"`) {
		t.Fatalf("JSON contract lost: %s %v", b, err)
	}
}

func TestSelectLinksSkipsCloudflareInfrastructure(t *testing.T) {
	p := readinessPage(t, `<a href="/cdn-cgi/l/email-protection">Email</a><a href="/cdn-cgi">Internal</a><a href="/about">About</a><a href="/docs">Docs</a><a href="/pricing">Pricing</a><a href="/contact">Contact</a>`)
	base, _ := url.Parse(p.page.URL)
	got := selectLinks(p.doc, base)
	want := []string{"https://example.com/about", "https://example.com/docs", "https://example.com/pricing", "https://example.com/contact"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("infrastructure consumed content sample: %v", got)
	}
}
