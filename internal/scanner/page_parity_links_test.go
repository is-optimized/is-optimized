package scanner

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestPageParityLinkTargetsReuseSampleAndCheckIcon(t *testing.T) {
	home, _ := url.Parse("https://example.com/")
	calls := 0
	scanner := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/robots.txt" {
			return reply(r, 200, "User-agent: *\nAllow: /", "text/plain"), nil
		}
		calls++
		if r.URL.Path == "/favicon.ico" {
			return reply(r, 200, "icon bytes", "image/x-icon"), nil
		}
		t.Fatalf("unexpected extra probe %s", r.URL)
		return nil, nil
	})
	ss := &session{s: scanner, ctx: context.Background(), policies: map[string]policy{}}
	pages := []pageResult{parityPage(t, home.String(), `<link rel="canonical" href="https://example.com/"><a href="/docs">Docs</a>`), parityPage(t, "https://example.com/docs", `<link rel="canonical" href="https://example.com/docs"><main>Docs</main>`)}
	findings := pageLinkParityChecks(ss, pages, home)
	for _, id := range []string{"seo.canonical_target", "seo.internal_link_health", "seo.favicon"} {
		if f := parityByID(t, findings, id); f.Status != "pass" {
			t.Fatalf("%+v", f)
		}
	}
	if calls != 1 {
		t.Fatalf("%d extra probes, want only icon", calls)
	}
}

func TestPageParityBrokenCanonicalAndLinks(t *testing.T) {
	home, _ := url.Parse("https://example.com/")
	scanner := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/robots.txt" {
			return reply(r, 200, "User-agent: *\nAllow: /", "text/plain"), nil
		}
		return reply(r, 404, "Missing", "text/html"), nil
	})
	ss := &session{s: scanner, ctx: context.Background(), policies: map[string]policy{}}
	page := parityPage(t, home.String(), `<link rel="canonical" href="/missing"><a href="/missing">Missing</a>`)
	for _, f := range pageLinkParityChecks(ss, []pageResult{page}, home) {
		if f.Status != "fail" {
			t.Fatalf("%+v", f)
		}
	}
}

func TestPageParityProbeLimitsAndActionExclusions(t *testing.T) {
	home, _ := url.Parse("https://example.com/")
	calls := 0
	scanner := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatalf("unexpected method %s", r.Method)
		}
		if r.URL.Path == "/robots.txt" {
			return reply(r, 200, "User-agent: *\nAllow: /", "text/plain"), nil
		}
		calls++
		if strings.Contains(r.URL.Path, "logout") || r.URL.RawQuery != "" {
			t.Fatalf("unsafe probe %s", r.URL)
		}
		if r.URL.Path == "/favicon.ico" {
			return reply(r, 200, "icon", "image/png"), nil
		}
		return reply(r, 200, `<main>Content</main>`, "text/html"), nil
	})
	ss := &session{s: scanner, ctx: context.Background(), policies: map[string]policy{}}
	page := parityPage(t, home.String(), `<link rel="canonical" href="/logout"><a href="/a">A</a><a href="/b">B</a><a href="/c">C</a><a href="/d">D</a><a href="/logout">Logout</a><a href="/?delete=yes">Delete</a>`)
	findings := pageLinkParityChecks(ss, []pageResult{page}, home)
	if calls != 4 {
		t.Fatalf("%d probes, want 3 content targets + reserved icon", calls)
	}
	if f := parityByID(t, findings, "seo.canonical_target"); f.Status != "unknown" {
		t.Fatalf("%+v", f)
	}
	if f := parityByID(t, findings, "seo.internal_link_health"); f.Status != "unknown" {
		t.Fatalf("unobserved link counted as healthy: %+v", f)
	}
	if f := parityByID(t, findings, "seo.favicon"); f.Status != "pass" {
		t.Fatalf("reserved icon probe missing: %+v", f)
	}
}

func TestPageParityExternalQueryAndExhaustedTargetsAreUnknown(t *testing.T) {
	home, _ := url.Parse("https://example.com/")
	for _, canonical := range []string{"https://other.example.com/page", "https://example.com/?lang=en", "https://example.com/target"} {
		scanner := fixtureScanner(func(r *http.Request) (*http.Response, error) {
			t.Fatalf("budget/boundary must prevent probe: %s", r.URL)
			return nil, nil
		})
		ss := &session{s: scanner, ctx: context.Background(), policies: map[string]policy{}}
		ss.discovery.Store(maxDiscoveryCount)
		page := parityPage(t, home.String(), `<link rel="canonical" href="`+canonical+`">`)
		findings := pageLinkParityChecks(ss, []pageResult{page}, home)
		for _, id := range []string{"seo.canonical_target", "seo.favicon"} {
			if f := parityByID(t, findings, id); f.Status != "unknown" {
				t.Fatalf("%+v", f)
			}
		}
	}
}

func TestPageParityIconHTMLFallbackAndServerError(t *testing.T) {
	home, _ := url.Parse("https://example.com/")
	for _, tc := range []struct {
		code             int
		kind, body, want string
	}{{200, "text/html", "<h1>Home fallback</h1>", "fail"}, {200, "image/png", "", "fail"}, {503, "text/plain", "Unavailable", "unknown"}, {200, "image/png", "PNG", "pass"}} {
		scanner := fixtureScanner(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/robots.txt" {
				return reply(r, 200, "User-agent: *\nAllow: /", "text/plain"), nil
			}
			return reply(r, tc.code, tc.body, tc.kind), nil
		})
		ss := &session{s: scanner, ctx: context.Background(), policies: map[string]policy{}}
		f := parityByID(t, pageLinkParityChecks(ss, []pageResult{parityPage(t, home.String(), `<main>Home</main>`)}, home), "seo.favicon")
		if f.Status != tc.want {
			t.Fatalf("%+v", f)
		}
	}
}

func TestPageParityCanonicalTargetPartialAndApplicability(t *testing.T) {
	home, _ := url.Parse("https://example.com/")
	for _, tc := range []struct {
		source, target string
		code           int
		want           string
	}{
		{`<link rel="canonical" href="/target">`, `<link rel="canonical" href="/other">`, 200, "partial"},
		{`<link rel="canonical" href="/target">`, `<main>No canonical</main>`, 200, "partial"},
		{`<link rel="canonical" href="/target">`, `Unavailable`, 503, "unknown"},
		{`<link rel="canonical" href="http://127.0.0.1/">`, ``, 200, "fail"},
		{`<main>No declaration</main>`, ``, 200, "not_applicable"},
	} {
		scanner := fixtureScanner(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/robots.txt" {
				return reply(r, 200, "User-agent: *\nAllow: /", "text/plain"), nil
			}
			return reply(r, tc.code, tc.target, "text/html"), nil
		})
		ss := &session{s: scanner, ctx: context.Background(), policies: map[string]policy{}}
		f := parityByID(t, pageLinkParityChecks(ss, []pageResult{parityPage(t, home.String(), tc.source)}, home), "seo.canonical_target")
		if f.Status != tc.want {
			t.Fatalf("%+v", f)
		}
	}
}

func TestPageParityDeclaredExternalOrQueryIconNotReplacedByFallback(t *testing.T) {
	home, _ := url.Parse("https://example.com/")
	for _, icon := range []string{"https://cdn.example.com/icon.png", "/icon.png?version=1"} {
		scanner := fixtureScanner(func(r *http.Request) (*http.Response, error) {
			t.Fatalf("unexpected fallback or cross-boundary icon probe: %s", r.URL)
			return nil, nil
		})
		ss := &session{s: scanner, ctx: context.Background(), policies: map[string]policy{}}
		f := parityByID(t, pageLinkParityChecks(ss, []pageResult{parityPage(t, home.String(), `<link rel="icon" href="`+icon+`">`)}, home), "seo.favicon")
		if f.Status != "unknown" {
			t.Fatalf("%+v", f)
		}
	}
}
