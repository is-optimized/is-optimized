package scanner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func reply(r *http.Request, status int, body, kind string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{kind}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}
func fixtureScanner(f roundTrip) *Scanner {
	return &Scanner{transport: f, slots: make(chan struct{}, 16)}
}
func TestBoundedDepthAndConcurrency(t *testing.T) {
	var mu sync.Mutex
	var requests []string
	var active, peak atomic.Int32
	var home strings.Builder
	home.WriteString(`<main><h1>Home</h1>`)
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&home, `<a href="/p%02d">Page %d</a>`, i, i)
	}
	home.WriteString(`</main>`)
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requests = append(requests, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/robots.txt" {
			return reply(r, 404, "", "text/plain"), nil
		}
		if strings.HasPrefix(r.URL.Path, "/llms") {
			return reply(r, 404, "", "text/plain"), nil
		}
		if r.URL.Path == "/" {
			return reply(r, 200, home.String(), "text/html"), nil
		}
		if !strings.HasPrefix(r.URL.Path, "/p") || len(r.URL.Path) <= 2 || r.URL.Path[2] < '0' || r.URL.Path[2] > '9' {
			return reply(r, 404, "Not found", "text/plain"), nil
		}
		n := active.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		return reply(r, 200, `<h1>Child</h1><a href="/deep">Deeper</a>`, "text/html"), nil
	})
	report, err := s.Scan(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Pages) != maxContentPages {
		t.Fatalf("pages=%d", len(report.Pages))
	}
	if peak.Load() != defaultPageConcurrency {
		t.Fatalf("want up to %d concurrent page fetches; got %d", defaultPageConcurrency, peak.Load())
	}
	seen := map[string]bool{}
	for _, p := range report.Pages {
		parsed, _ := url.Parse(p.URL)
		seen[parsed.Path] = true
	}
	for _, want := range []string{"/", "/p01", "/p02", "/p03", "/p04", "/p05", "/p31"} {
		if !seen[want] {
			t.Errorf("missing sampled path %s", want)
		}
	}
	for _, p := range requests {
		if p == "/logout" {
			t.Errorf("unexpected fetch %s", p)
		}
	}
	if requests[0] != "/robots.txt" {
		t.Fatalf("first fetch was %s", requests[0])
	}
}

func TestReadBudgetedStopsAtSharedByteLimit(t *testing.T) {
	var total atomic.Int64
	body, err := readBudgeted(strings.NewReader(strings.Repeat("x", 100)), &total, 100, 37)
	if !errors.Is(err, errScanByteBudget) {
		t.Fatalf("expected byte-budget error, got %v", err)
	}
	if len(body) != 37 || total.Load() != 37 {
		t.Fatalf("read %d bytes, accounted %d; want 37", len(body), total.Load())
	}
}

func TestScanPageCapAndConcurrencyBound(t *testing.T) {
	var active, peak atomic.Int32
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/robots.txt" || strings.HasPrefix(r.URL.Path, "/llms") {
			return reply(r, 404, "", "text/plain"), nil
		}
		if r.URL.Path == "/" {
			var links strings.Builder
			for i := 0; i < 40; i++ {
				fmt.Fprintf(&links, `<a href="/p%d">page</a>`, i)
			}
			return reply(r, 200, links.String(), "text/html"), nil
		}
		if !strings.HasPrefix(r.URL.Path, "/p") || len(r.URL.Path) <= 2 || r.URL.Path[2] < '0' || r.URL.Path[2] > '9' {
			return reply(r, 404, "Not found", "text/plain"), nil
		}
		n := active.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		active.Add(-1)
		return reply(r, 200, `<h1>Page</h1>`, "text/html"), nil
	})
	report, err := s.Scan(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Pages) != maxContentPages {
		t.Fatalf("want %d total pages, got %d", maxContentPages, len(report.Pages))
	}
	if peak.Load() > defaultPageConcurrency || peak.Load() < 2 {
		t.Fatalf("observed page concurrency %d; expected 2..%d", peak.Load(), defaultPageConcurrency)
	}
}

func TestScanSamplesDeclaredSitemapAndSecondHop(t *testing.T) {
	var requests []string
	var requestsMu sync.Mutex
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		requestsMu.Lock()
		requests = append(requests, r.URL.Path)
		requestsMu.Unlock()
		switch r.URL.Path {
		case "/robots.txt":
			return reply(r, 200, "User-agent: *\nSitemap: https://example.com/sitemap.xml\n", "text/plain"), nil
		case "/sitemap.xml":
			return reply(r, 200, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://example.com/from-sitemap</loc></url></urlset>`, "application/xml"), nil
		case "/":
			return reply(r, 200, `<h1>Home</h1>`, "text/html"), nil
		case "/from-sitemap":
			return reply(r, 200, `<h1>Listed</h1><a href="/second-hop">Next</a>`, "text/html"), nil
		case "/second-hop":
			return reply(r, 200, `<h1>Second</h1>`, "text/html"), nil
		case "/llms.txt", "/llms-full.txt":
			return reply(r, 404, "", "text/plain"), nil
		default:
			return reply(r, 404, "", "text/plain"), nil
		}
	})
	report, err := s.Scan(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, page := range report.Pages {
		parsed, _ := url.Parse(page.URL)
		seen[parsed.Path] = true
	}
	if !seen["/from-sitemap"] || !seen["/second-hop"] {
		t.Fatalf("sitemap or second-hop page missing from sample: %#v", seen)
	}
	requestsMu.Lock()
	observedRequests := append([]string(nil), requests...)
	requestsMu.Unlock()
	if !containsString(observedRequests, "/sitemap.xml") {
		t.Fatalf("declared sitemap not fetched: %#v", observedRequests)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
func TestRobotsBeforeRedirectOrigin(t *testing.T) {
	var requests []string
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.String())
		if r.URL.Host == "example.com" {
			if r.URL.Path == "/robots.txt" {
				return reply(r, 404, "", "text/plain"), nil
			}
			res := reply(r, 302, "", "text/html")
			res.Header.Set("Location", "https://other.com/")
			return res, nil
		}
		if r.URL.Path == "/robots.txt" {
			return reply(r, 200, "User-agent: *\nDisallow: /", "text/plain"), nil
		}
		t.Fatal("fetched disallowed redirected homepage")
		return nil, nil
	})
	r, e := s.Scan(context.Background(), "example.com")
	if e != nil {
		t.Fatal(e)
	}
	if len(requests) != 3 {
		t.Fatal(requests)
	}
	if !strings.Contains(r.Pages[0].Error, "disallowed") {
		t.Fatal(r.Pages[0])
	}
	for _, p := range r.Pillars {
		if p.Score != nil {
			t.Fatal("unknown page published score")
		}
	}
}
func TestRobotsUnavailableStopsContent(t *testing.T) {
	for _, code := range []int{403, 429, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			calls := 0
			s := fixtureScanner(func(r *http.Request) (*http.Response, error) { calls++; return reply(r, code, "", "text/plain"), nil })
			r, e := s.Scan(context.Background(), "example.com")
			if e != nil || calls != 1 || r.Pages[0].Error == "" {
				t.Fatalf("calls=%d error=%v report=%+v", calls, e, r)
			}
		})
	}
}
func TestRobotsMatching(t *testing.T) {
	r, e := parseRobots("User-agent: *\nDisallow: /\nUser-agent: IsOptimizedBot\nDisallow: /secret*\nAllow: /secret/public$\n")
	if e != nil {
		t.Fatal(e)
	}
	for p, want := range map[string]bool{"/": true, "/secret": false, "/secret/x": false, "/secret/public": true, "/secret/public/extra": false} {
		if got := r.allowed(p); got != want {
			t.Errorf("%s=%t want %t", p, got, want)
		}
	}
	if _, e = parseRobots("<html>oops</html>"); e == nil {
		t.Fatal("HTML accepted as robots")
	}
}

func TestRobotsContentSignalsAndAIAgentDecisions(t *testing.T) {
	r, err := parseRobots("User-agent: *\nDisallow: /private\nContent-Signal: search=yes, ai-train=no\nUser-agent: ChatGPT-User\nAllow: /\nUser-agent: GPTBot\nDisallow: /\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.contentSignals) != 1 || r.contentSignals[0] != "search=yes, ai-train=no" {
		t.Fatalf("content signals not retained: %#v", r.contentSignals)
	}
	for _, tc := range []struct {
		agent string
		path  string
		allow bool
	}{
		{"ChatGPT-User", "/", true},
		{"Claude-User", "/", true},
		{"Claude-User", "/private/page", false},
		{"GPTBot", "/", false},
		{"UnknownBot", "/", true},
	} {
		got, _ := r.decision(tc.agent, tc.path)
		if got != tc.allow {
			t.Errorf("%s %s allowed=%t want %t", tc.agent, tc.path, got, tc.allow)
		}
	}
	comments, err := parseRobots("# no rules\n")
	if err != nil {
		t.Fatal(err)
	}
	allowed, specified := comments.decision("PerplexityBot", "/")
	if !allowed || specified {
		t.Fatalf("empty policy must report unspecified and allow by default: allowed=%t specified=%t", allowed, specified)
	}
}
func TestPublicAddressValidation(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "192.0.2.1", "::1", "::ffff:8.8.8.8", "fc00::1", "2001:db8::1", "64:ff9b::a00:1", "2002:a00:1::"} {
		if publicIP(netip.MustParseAddr(raw)) {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		if !publicIP(netip.MustParseAddr(raw)) {
			t.Errorf("rejected public %s", raw)
		}
	}
}
func TestNormalizeDomain(t *testing.T) {
	for _, bad := range []string{"localhost", "https://user:pass@example.com", "file:///tmp/x", "https://example.com:8080/", "https://example.com/a", "127.0.0.1", "example.com?x=y", "example.com#x", "example.com."} {
		if _, e := NormalizeDomain(bad); e == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	if d, e := NormalizeDomain(" HTTPS://Example.COM/ "); e != nil || d != "example.com" {
		t.Fatalf("%q %v", d, e)
	}
}
func TestByteLimitIsUnknown(t *testing.T) {
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/robots.txt" {
			return reply(r, 404, "", "text/plain"), nil
		}
		return reply(r, 200, strings.Repeat("a", maxBody+1), "text/html"), nil
	})
	r, e := s.Scan(context.Background(), "example.com")
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(r.Pages[0].Error, "size limit") {
		t.Fatal(r.Pages[0])
	}
	if r.Pillars[0].Score != nil {
		t.Fatal("oversize page received score")
	}
}
func TestScoreCoverageAndApplicability(t *testing.T) {
	f := []reportmodel.Finding{{Pillar: "seo", Status: "pass", Weight: 8}, {Pillar: "seo", Status: "unknown", Weight: 2}, {Pillar: "seo", Status: "not_applicable", Weight: 100}}
	p := score(f)[0]
	if p.Score == nil || *p.Score != 100 || p.Coverage != .8 {
		t.Fatalf("%+v", p)
	}
	f[1].Foundational = true
	if score(f)[0].Score != nil {
		t.Fatal("foundational unknown scored")
	}
	f[1].Foundational = false
	f[1].Weight = 3
	if score(f)[0].Score != nil {
		t.Fatal("insufficient coverage scored")
	}
}

func TestAgentReadinessTierScoreAndCoverage(t *testing.T) {
	findings := []reportmodel.Finding{
		{Tier: "essential", Weight: 2, Status: "pass"},
		{Tier: "essential", Weight: 2, Status: "fail"},
		{Tier: "essential", Weight: 1, Status: "unknown"},
		{Tier: "recommended", Weight: 2, Status: "pass"},
		{Tier: "recommended", Weight: 50, Status: "not_applicable"},
		{Tier: "bonus", Weight: 10, Status: "pass"},
		{Tier: "bonus", Weight: 1, Status: "partial"},
	}
	pillar, summary := scoreAgentReadiness(findings)
	if pillar.Score == nil || *pillar.Score != 65 || pillar.Coverage < .85 || pillar.Coverage > .86 {
		t.Fatalf("pillar=%+v summary=%+v", pillar, summary)
	}
	if summary.Essential.Earned != 40 || summary.Recommended.Earned != 20 || summary.BonusPoints != 5 || summary.PositiveSignals != 2 || summary.ScoreLabel != "Important blockers remain" {
		t.Fatalf("summary=%+v", summary)
	}

	findings[2].Status = "not_applicable"
	pillar, summary = scoreAgentReadiness(findings)
	if pillar.Score == nil || summary.Coverage != 1 {
		t.Fatalf("not-applicable evidence affected coverage: pillar=%+v summary=%+v", pillar, summary)
	}
	findings[2].Status = "unknown"
	findings[2].Weight = 2
	pillar, _ = scoreAgentReadiness(findings)
	if pillar.Score != nil {
		t.Fatalf("score should be withheld below 80%% evidence coverage: %+v", pillar)
	}
}

func TestEssentialDiscoveryChecksRunBeforeOptionalChecksAtBudgetBoundary(t *testing.T) {
	home, _ := url.Parse("https://example.com/")
	body := `<html><head><title>Example API</title></head><body><main><h1>Example</h1><a href="/openapi.json">OpenAPI</a><a href="/oauth/login">OAuth</a></main></body></html>`
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/":
			return reply(r, http.StatusOK, body, "text/html"), nil
		case "/openapi.json":
			return reply(r, http.StatusOK, validOpenAPIJSON, "application/json"), nil
		case "/.well-known/oauth-authorization-server":
			return reply(r, http.StatusOK, `{"issuer":"https://example.com","authorization_endpoint":"https://example.com/authorize","code_challenge_methods_supported":["S256"],"scopes_supported":["read"]}`, "application/json"), nil
		case "/.well-known/oauth-protected-resource":
			return reply(r, http.StatusOK, `{"resource":"https://example.com/api","authorization_servers":["https://example.com"]}`, "application/json"), nil
		case "/.well-known/api-catalog":
			return reply(r, http.StatusOK, `{"linkset":[{"anchor":"https://example.com/.well-known/api-catalog","item":[{"href":"https://api.example.com/v1"}]}]}`, `application/linkset+json`), nil
		default:
			return reply(r, http.StatusNotFound, "Not found", "text/plain"), nil
		}
	})
	ss := &session{s: s, ctx: context.Background(), policies: map[string]policy{
		origin(home): {present: true, status: http.StatusOK, rules: robots{}},
	}}
	// This API fixture now includes one MCP-card request and a live API GET
	// in addition to the original fifteen essential discovery requests.
	ss.discovery.Store(maxDiscoveryCount - 17)
	homepage, err := ss.fetch(home.String(), false, origin(home))
	if err != nil {
		t.Fatal(err)
	}
	p := analyze(homepage, nil, home.String(), time.Millisecond)
	findings := siteAgentChecks(ss, home, homepage, p)
	byID := make(map[string]reportmodel.Finding, len(findings))
	for _, f := range findings {
		byID[f.ID] = f
	}
	for _, id := range []string{"agent.markdown", "agent.markdown_404", "agent.robots", "agent.openapi.spec", "agent.oauth.discovery", "agent.api_catalog"} {
		f, ok := byID[id]
		if !ok || f.Status == "unknown" || strings.Contains(f.Evidence, "discovery budget exhausted") {
			t.Errorf("essential discovery %s was starved at budget boundary: %+v", id, f)
		}
	}
}

func TestPageRequestPartitionReservesDiscoveryBudget(t *testing.T) {
	home, _ := url.Parse("https://example.com/")
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		return reply(r, http.StatusOK, "ok", "text/plain"), nil
	})
	ss := &session{s: s, ctx: context.Background(), policies: map[string]policy{
		origin(home): {present: true, status: http.StatusOK, rules: robots{}},
	}}
	for i := 0; i < maxPageRequests; i++ {
		if _, err := ss.fetchPage("https://example.com/page", origin(home)); err != nil {
			t.Fatalf("page request %d was rejected early: %v", i+1, err)
		}
	}
	if _, err := ss.fetchPage("https://example.com/page", origin(home)); err == nil || !strings.Contains(err.Error(), "page request budget") {
		t.Fatalf("page request after reserved page partition should be rejected, got %v", err)
	}
	for i := 0; i < maxDiscoveryCount; i++ {
		if _, err := ss.probe("https://example.com/probe", origin(home)); err != nil {
			t.Fatalf("discovery request %d was starved by page fetches: %v", i+1, err)
		}
	}
	if got := ss.requests.Load(); got != maxRequestCount {
		t.Fatalf("total request count=%d, want exact partition total %d", got, maxRequestCount)
	}
}

func TestMarkdownNegotiationAndAgent404Checks(t *testing.T) {
	homeURL, _ := url.Parse("https://example.com/")
	home := response{url: homeURL, code: http.StatusOK, contentType: "text/html; charset=utf-8"}
	tests := []struct {
		name, markdownType, markdownBody string
		vary                             string
		status                           int
		want404, wantMarkdown            string
	}{
		{name: "complete", markdownType: "text/markdown", markdownBody: "# Example\n\n[Docs](/docs)", vary: "Accept", status: http.StatusNotFound, want404: "pass", wantMarkdown: "pass"},
		{name: "markdown without cache variation", markdownType: "text/markdown", markdownBody: "# Example\n\nBody", status: http.StatusNotFound, want404: "partial", wantMarkdown: "partial"},
		{name: "correct status without markdown", markdownType: "text/html", markdownBody: "<html>Not found</html>", status: http.StatusNotFound, want404: "partial", wantMarkdown: "fail"},
		{name: "soft 404", markdownType: "text/markdown", markdownBody: "# Not found\n\n[Docs](/docs)", vary: "Accept", status: http.StatusOK, want404: "fail", wantMarkdown: "pass"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/" && r.Header.Get("Accept") == "text/markdown" {
					v := reply(r, http.StatusOK, tt.markdownBody, tt.markdownType)
					if tt.vary != "" {
						v.Header.Set("Vary", tt.vary)
					}
					return v, nil
				}
				if strings.HasPrefix(r.URL.Path, "/__is-optimized-404-") {
					if r.Header.Get("Accept") == "text/markdown" {
						return reply(r, tt.status, tt.markdownBody, tt.markdownType), nil
					}
					return reply(r, tt.status, "Not found", "text/html"), nil
				}
				return reply(r, http.StatusNotFound, "", "text/plain"), nil
			})
			ss := &session{s: s, ctx: context.Background(), policies: map[string]policy{}}
			checks := siteAgentChecks(ss, homeURL, home, readinessPage(t, `<html><body><main>Example text</main></body></html>`))
			got := map[string]string{}
			for _, f := range checks {
				got[f.ID] = f.Status
				if (f.ID == "agent.markdown" || f.ID == "agent.markdown_404" || f.ID == "agent.robots") && (f.Weight != 3 || f.Tier != "essential") {
					t.Fatalf("missing readiness scoring metadata: %+v", f)
				}
			}
			if got["agent.markdown"] != tt.wantMarkdown {
				t.Fatalf("Markdown negotiation status=%q want %q", got["agent.markdown"], tt.wantMarkdown)
			}
			if got["agent.markdown_404"] != tt.want404 {
				t.Fatalf("Markdown 404 status=%q want %q", got["agent.markdown_404"], tt.want404)
			}
		})
	}
}

func TestAICrawlerAccessUsesRobotsPolicyAndRetrievalResponses(t *testing.T) {
	homeURL, _ := url.Parse("https://example.com/")
	homepage := response{url: homeURL, code: http.StatusOK, body: []byte(strings.Repeat("x", 100)), contentType: "text/html"}
	for _, tc := range []struct {
		name, robots string
		status       int
		want         string
	}{
		{name: "allowed retrieval", robots: "User-agent: *\nAllow: /\nContent-Signal: search=yes, ai-train=no\n", status: http.StatusOK, want: "pass"},
		{name: "blocked retrieval", robots: "User-agent: *\nAllow: /\nUser-agent: Claude-User\nDisallow: /\n", status: http.StatusOK, want: "fail"},
		{name: "challenge response", robots: "User-agent: *\nAllow: /\n", status: http.StatusForbidden, want: "fail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := parseRobots(tc.robots)
			if err != nil {
				t.Fatal(err)
			}
			s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet {
					t.Errorf("unexpected request method %s", r.Method)
				}
				status := tc.status
				body := strings.Repeat("y", 80)
				if status == http.StatusForbidden {
					body = "captcha challenge-platform"
				}
				return reply(r, status, body, "text/html"), nil
			})
			ss := &session{s: s, ctx: context.Background(), policies: map[string]policy{origin(homeURL): {rules: parsed, present: true, status: http.StatusOK}}}
			got := aiCrawlerAccessCheck(ss, homeURL, homepage)
			if got.Status != tc.want {
				t.Fatalf("status=%q want %q evidence=%s", got.Status, tc.want, got.Evidence)
			}
			if !strings.Contains(got.Evidence, "Content-Signal") {
				t.Fatalf("Content-Signal evidence missing: %s", got.Evidence)
			}
		})
	}
}

func TestTrustPageFindingsUseStatusAndVisibleText(t *testing.T) {
	home, _ := url.Parse("https://example.com/")
	def := trustPageDefinitions()[0]
	for _, tc := range []struct {
		name, body string
		status     int
		fetchErr   bool
		want       string
	}{
		{name: "complete", body: strings.Repeat("Useful visible company information. ", 20), status: http.StatusOK, want: "pass"},
		{name: "short page", body: "Short but visible.", status: http.StatusOK, want: "partial"},
		{name: "missing", body: "Not found", status: http.StatusNotFound, want: "fail"},
		{name: "unobserved", fetchErr: true, want: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
				if tc.fetchErr {
					return nil, errors.New("fixture unavailable")
				}
				return reply(r, tc.status, "<html><body><main>"+tc.body+"</main></body></html>", "text/html"), nil
			})
			ss := &session{s: s, ctx: context.Background(), policies: map[string]policy{origin(home): {rules: robots{groups: []robotGroup{{agents: []string{"*"}, rules: []robotRule{{path: "/", allow: true}}}}}}}}
			got := trustPageCheck(ss, home, readinessPage(t, `<main><a href="/about">About</a></main>`), def)
			if got.Status != tc.want {
				t.Fatalf("status=%q want %q evidence=%s", got.Status, tc.want, got.Evidence)
			}
			if got.Tier != "recommended" || got.Weight != 1 {
				t.Fatalf("unexpected scoring metadata: %+v", got)
			}
		})
	}
}

func TestSocialMetadataCompletenessAndImageFetch(t *testing.T) {
	home, _ := url.Parse("https://example.com/")
	for _, tc := range []struct {
		name, html string
		status     string
	}{
		{name: "complete", status: "pass", html: `<html lang="en"><head><meta property="og:title" content="Example"><meta property="og:description" content="A description"><meta property="og:image" content="https://example.com/preview.png"><meta property="og:type" content="website"><meta property="og:url" content="https://example.com/"><meta name="twitter:card" content="summary_large_image"><link rel="canonical" href="https://example.com/"></head><body></body></html>`},
		{name: "partial", status: "partial", html: `<html lang="en"><head><meta property="og:title" content="Example"></head><body></body></html>`},
		{name: "empty", status: "fail", html: `<html><head></head><body></body></html>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/preview.png" {
					return reply(r, http.StatusOK, "png", "image/png"), nil
				}
				return reply(r, http.StatusNotFound, "", "text/plain"), nil
			})
			ss := &session{s: s, ctx: context.Background(), policies: map[string]policy{origin(home): {rules: robots{groups: []robotGroup{{agents: []string{"*"}, rules: []robotRule{{path: "/", allow: true}}}}}}}}
			got := socialMetadataCheck(ss, home, readinessPage(t, tc.html))
			if got.Status != tc.status {
				t.Fatalf("status=%q want %q evidence=%s", got.Status, tc.status, got.Evidence)
			}
			if tc.status == "pass" && !strings.Contains(got.Evidence, "HTTP 200 image/*") {
				t.Fatalf("image not independently resolved: %s", got.Evidence)
			}
		})
	}
}

func TestSchemaCompletenessSameAsAndBreadth(t *testing.T) {
	for _, tc := range []struct {
		name, script, organization, links, breadth string
	}{
		{name: "complete organization", script: `{"@graph":[{"@type":"Organization","name":"Example","url":"https://example.com/","logo":"https://example.com/logo.png","description":"Example organization","sameAs":["https://www.linkedin.com/company/example","https://www.wikidata.org/wiki/Q1"],"contactPoint":{"@type":"ContactPoint","contactType":"support","email":"help@example.com"},"address":{"@type":"PostalAddress","addressCountry":"US"}},{"@type":"WebSite","name":"Example"},{"@type":"BreadcrumbList","itemListElement":[]}]}`, organization: "pass", links: "pass", breadth: "pass"},
		{name: "partial", script: `{"@type":"Organization","name":"Example","sameAs":["https://www.linkedin.com/company/example"]}`, organization: "partial", links: "partial", breadth: "partial"},
		{name: "no schema", organization: "fail", links: "fail", breadth: "not_applicable"},
		{name: "unrelated schema", script: `{"@type":"WebSite","name":"Example"}`, organization: "fail", links: "fail", breadth: "partial"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `<html><head>`
			if tc.script != "" {
				body += `<script type="application/ld+json">` + tc.script + `</script>`
			}
			body += `</head><body></body></html>`
			got := map[string]string{}
			home, _ := url.Parse("https://example.com/")
			for _, f := range schemaChecks(home, readinessPage(t, body)) {
				got[f.ID] = f.Status
			}
			if got["geo.organization"] != tc.organization || got["geo.entity_links"] != tc.links || got["geo.schema_breadth"] != tc.breadth {
				t.Fatalf("schema statuses: %v", got)
			}
		})
	}
}

func TestLLMSiteInstructionsQualityAndWhenToUse(t *testing.T) {
	home, _ := url.Parse("https://example.com/")
	good := "# Example\n\n> A concise summary.\n\n## When to use this\nUse this site for specific research tasks.\n\n[Docs](/docs)\n[Pricing](/pricing)\n"
	for _, tc := range []struct {
		name, llms, quality, when, full string
	}{
		{name: "complete without non-standard frontmatter", llms: good, quality: "pass", when: "pass", full: "present"},
		{name: "partial guide", llms: "# Example\n\n[Docs](/missing)\n", quality: "partial", when: "fail", full: "absent"},
		{name: "missing guide", quality: "fail", when: "fail", full: "absent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/robots.txt":
					return reply(r, http.StatusNotFound, "", "text/plain"), nil
				case "/llms.txt":
					if tc.llms == "" {
						return reply(r, http.StatusNotFound, "", "text/plain"), nil
					}
					return reply(r, http.StatusOK, tc.llms, "text/plain; charset=utf-8"), nil
				case "/llms-full.txt":
					if tc.full == "present" {
						return reply(r, http.StatusOK, "# Full reference", "text/markdown"), nil
					}
					return reply(r, http.StatusNotFound, "", "text/plain"), nil
				case "/docs", "/pricing":
					return reply(r, http.StatusOK, "Page", "text/html"), nil
				default:
					return reply(r, http.StatusNotFound, "", "text/plain"), nil
				}
			})
			ss := &session{s: s, ctx: context.Background(), policies: map[string]policy{}}
			got := map[string]reportmodel.Finding{}
			for _, f := range agentInstructionChecks(ss, home) {
				got[f.ID] = f
			}
			if got["agent.instructions.quality"].Status != tc.quality || got["agent.instructions.when_to_use"].Status != tc.when {
				t.Fatalf("instruction findings: quality=%+v when=%+v", got["agent.instructions.quality"], got["agent.instructions.when_to_use"])
			}
			full := got["agent.instructions.full"]
			if tc.full == "present" && !strings.Contains(full.Evidence, "HTTP 200") {
				t.Fatalf("full reference not observed: %+v", full)
			}
		})
	}
}

func TestRedirectPrivateRejectedBeforeTransport(t *testing.T) {
	calls := 0
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path == "/robots.txt" {
			return reply(r, 404, "", "text/plain"), nil
		}
		v := reply(r, 302, "", "text/html")
		v.Header.Set("Location", "http://169.254.169.254/")
		return v, nil
	})
	r, e := s.Scan(context.Background(), "example.com")
	if e != nil || calls != 2 || r.Pages[0].Error == "" {
		t.Fatalf("calls=%d err=%v report=%+v", calls, e, r)
	}
}
func TestRequestBudget(t *testing.T) {
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) { return reply(r, 200, "ok", "text/plain"), nil })
	ss := &session{s: s, ctx: context.Background(), policies: map[string]policy{}}
	for i := 0; i < maxRequestCount; i++ {
		if _, e := ss.fetch("https://example.com/", false, ""); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := ss.fetch("https://example.com/", false, ""); e == nil {
		t.Fatal("request budget ignored")
	}
	if got := ss.requestBudgetExhaustions.Load(); got != 1 {
		t.Fatalf("request budget exhaustion metric=%d, want 1", got)
	}
}

func TestFetchBudgetExhaustionMetrics(t *testing.T) {
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		return reply(r, 200, strings.Repeat("x", maxBody+1), "text/html"), nil
	})
	bytesSession := &session{s: s, ctx: context.Background(), policies: map[string]policy{}}
	bytesSession.bytes.Store(maxScanBytes)
	if _, err := bytesSession.fetch("https://example.com/", false, ""); err == nil || !strings.Contains(err.Error(), "byte budget") {
		t.Fatalf("expected scan byte budget error, got %v", err)
	}
	if got := bytesSession.scanByteBudgetExhaustions.Load(); got != 1 {
		t.Fatalf("scan byte budget exhaustion metric=%d, want 1", got)
	}
	sizeSession := &session{s: s, ctx: context.Background(), policies: map[string]policy{}}
	if _, err := sizeSession.fetch("https://example.com/", false, ""); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("expected response size error, got %v", err)
	}
	if got := sizeSession.responseSizeExhaustions.Load(); got != 1 {
		t.Fatalf("response size exhaustion metric=%d, want 1", got)
	}
}
func TestDiscoveryLimited(t *testing.T) {
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) { return reply(r, 404, "", "text/plain"), nil })
	ss := &session{s: s, ctx: context.Background(), policies: map[string]policy{}}
	u, _ := url.Parse("https://example.com/")
	ss.getPolicy(u)
	for i := 0; i < maxDiscoveryCount-1; i++ {
		if _, e := ss.probe("https://example.com/llms.txt", origin(u)); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := ss.probe("https://example.com/llms.txt", origin(u)); e == nil {
		t.Fatal("discovery budget ignored")
	}
	if got := ss.discoveryBudgetExhaustions.Load(); got != 1 {
		t.Fatalf("discovery budget exhaustion metric=%d, want 1", got)
	}
}

func TestOverlongLinkRejected(t *testing.T) {
	u, _ := url.Parse("https://example.com/" + strings.Repeat("x", 3000))
	if validateURL(u) == nil {
		t.Fatal("overlong URL accepted")
	}
}

func TestChallengeAndComments(t *testing.T) {
	if _, e := parseRobots("# only a comment\n"); e != nil {
		t.Fatal(e)
	}
	req, _ := http.NewRequest("GET", "https://example.com/", nil)
	r := response{url: req.URL, code: 200, body: []byte("<title>Just a moment...</title>"), contentType: "text/html", header: make(http.Header)}
	p := analyze(r, nil, req.URL.String(), 0)
	if p.page.Error == "" {
		t.Fatal("challenge scored as observed content")
	}
	for _, f := range evaluate(p) {
		if f.Status != "unknown" {
			t.Fatal(f)
		}
	}
}
