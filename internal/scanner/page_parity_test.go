package scanner

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
)

func parityPage(t *testing.T, raw, body string) pageResult {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return analyze(response{url: u, code: 200, body: []byte(body), contentType: "text/html", header: http.Header{}}, nil, raw, 0)
}

func TestPageParityFetchMetadataReachesEvaluation(t *testing.T) {
	cert := &x509.Certificate{NotAfter: time.Now().Add(90 * 24 * time.Hour)}
	scanner := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/robots.txt" {
			return reply(r, 200, "User-agent: *\nAllow: /", "text/plain"), nil
		}
		if trace := httptrace.ContextClientTrace(r.Context()); trace != nil && trace.GotFirstResponseByte != nil {
			trace.GotFirstResponseByte()
		}
		resp := reply(r, 200, `<main><button>Open</button></main>`, "text/html")
		resp.Header.Set("X-Content-Type-Options", "nosniff")
		resp.TLS = &tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}
		return resp, nil
	})
	ss := &session{s: scanner, ctx: context.Background(), policies: map[string]policy{}}
	r, err := ss.fetchPage("https://example.com/", "")
	if err != nil {
		t.Fatal(err)
	}
	p := analyze(r, nil, "https://example.com/", 900*time.Millisecond)
	findings := evaluate(p)
	for id, want := range map[string]string{"seo.ttfb": "information", "seo.tls_expiry": "pass", "seo.nosniff": "pass"} {
		if f := parityByID(t, findings, id); f.Status != want {
			t.Fatalf("%+v", f)
		}
	}
	if !p.ttfbObserved || p.ttfb <= 0 || p.ttfb >= 900*time.Millisecond || !p.tlsVerified || !p.tlsNotAfter.Equal(cert.NotAfter) {
		t.Fatalf("timing/TLS metadata missing: %+v", p)
	}
}

func TestPageParityDoesNotMislabelFetchDurationAsTTFB(t *testing.T) {
	p := parityPage(t, "https://example.com/", `<main>Content</main>`)
	p.page.DurationMS = 2500
	f := parityByID(t, pageParityChecks(p, nil), "seo.ttfb")
	if f.Status != "unknown" || strings.Contains(f.Evidence, "2500") {
		t.Fatalf("%+v", f)
	}
}

func TestPageParityTLSExpiryUsesVerifiedLeaf(t *testing.T) {
	for _, tc := range []struct {
		days     int
		verified bool
		want     string
	}{{90, true, "pass"}, {10, true, "partial"}, {-1, true, "fail"}, {90, false, "unknown"}} {
		p := parityPage(t, "https://example.com/", `<main>Content</main>`)
		p.tlsNotAfter = time.Now().Add(time.Duration(tc.days) * 24 * time.Hour)
		p.tlsVerified = tc.verified
		if f := parityByID(t, pageParityChecks(p, nil), "seo.tls_expiry"); f.Status != tc.want {
			t.Fatalf("%+v", f)
		}
	}
}

func parityByID(t *testing.T, findings []reportmodel.Finding, id string) reportmodel.Finding {
	t.Helper()
	for _, f := range findings {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("missing %s", id)
	return reportmodel.Finding{}
}

func TestPageParityDeclaredAlternatives(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"valid", `<link rel="alternate" hreflang="en-US" href="https://example.com/en/"><link rel="alternate" hreflang="x-default" href="/">`, "pass"},
		{"partly invalid", `<link rel="alternate" hreflang="en" href="/en"><link rel="alternate" hreflang="not_a_tag" href="/bad">`, "partial"},
		{"conflicting", `<link rel="alternate" hreflang="en" href="/en"><link rel="alternate" hreflang="EN" href="/different">`, "partial"},
		{"unsafe", `<link rel="alternate" hreflang="en" href="http://127.0.0.1/en">`, "fail"},
		{"no translations", `<main>One language</main>`, "not_applicable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := parityByID(t, pageParityChecks(parityPage(t, "https://example.com/", tc.body), nil), "seo.hreflang")
			if f.Status != tc.want {
				t.Fatalf("%+v", f)
			}
		})
	}
}

func TestPageParityMixedResourcesNotOrdinaryHTTPLinks(t *testing.T) {
	for _, tc := range []struct{ name, raw, body, want string }{
		{"safe resources", "https://example.com/", `<script src="/app.js"></script><img src="//example.com/image.png"><a href="http://archive.example.com">Archive</a>`, "pass"},
		{"script", "https://example.com/", `<script src="http://cdn.example.com/app.js"></script>`, "fail"},
		{"srcset", "https://example.com/", `<img src="/photo.webp" srcset="/small.webp 1x, http://cdn.example.com/large.webp 2x">`, "fail"},
		{"stylesheet", "https://example.com/", `<link rel="stylesheet" href="http://cdn.example.com/app.css">`, "fail"},
		{"HTTP base", "https://example.com/", `<base href="http://cdn.example.com/"><img src="image.png">`, "fail"},
		{"first base wins", "https://example.com/", `<base href="https://cdn.example.com/"><base href="http://cdn.example.com/"><img src="image.png">`, "pass"},
		{"plain HTTP", "http://example.com/", `<script src="http://example.com/app.js"></script>`, "not_applicable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := parityByID(t, pageParityChecks(parityPage(t, tc.raw, tc.body), nil), "seo.mixed_content")
			if f.Status != tc.want {
				t.Fatalf("%+v", f)
			}
		})
	}
}

func TestPageParityImageAndControlDeclarations(t *testing.T) {
	for _, tc := range []struct{ body, dimensions, controls string }{
		{`<img width="640" height="480" loading="lazy"><button>Open</button>`, "pass", "pass"},
		{`<img width="640" height="480"><img width="0" height="auto"><button>Open</button><div onclick="x()">Open</div>`, "partial", "partial"},
		{`<img><span onclick="x()">Open</span>`, "fail", "fail"},
		{`<main>Content</main><input type="hidden">`, "not_applicable", "not_applicable"},
		{`<a href="/docs">Docs</a>`, "not_applicable", "pass"},
	} {
		findings := pageParityChecks(parityPage(t, "https://example.com/", tc.body), nil)
		if f := parityByID(t, findings, "seo.image_dimensions"); f.Status != tc.dimensions {
			t.Fatalf("%+v", f)
		}
		if f := parityByID(t, findings, "agent.native_controls"); f.Status != tc.controls {
			t.Fatalf("%+v", f)
		}
	}
}

func TestPageParityJSONLDAndBrandEvidence(t *testing.T) {
	for _, tc := range []struct{ name, body, syntax, brand string }{
		{"consistent", `<title>Acme - Home</title><meta property="og:site_name" content="ACME"><script type="application/ld+json">{"@context":"https://schema.org","@type":"Organization","name":"Acme"}</script>`, "pass", "pass"},
		{"name variation", `<title>Different brand</title><script type="application/ld+json">{"@context":"https://schema.org","@type":"Organization","name":"Acme"}</script>`, "pass", "partial"},
		{"no context", `<script type="application/ld+json">{"@type":"WebSite","name":"Acme"}</script>`, "partial", "not_applicable"},
		{"malformed", `<script type="application/ld+json">{"@context":</script>`, "fail", "not_applicable"},
		{"invalid array", `<script type="application/ld+json">["not a node"]</script>`, "fail", "not_applicable"},
		{"none", `<title>Acme</title>`, "not_applicable", "not_applicable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			findings := pageParityChecks(parityPage(t, "https://example.com/", tc.body), nil)
			if f := parityByID(t, findings, "seo.structured_validity"); f.Status != tc.syntax {
				t.Fatalf("%+v", f)
			}
			if f := parityByID(t, findings, "geo.brand_consistency"); f.Status != tc.brand {
				t.Fatalf("%+v", f)
			}
		})
	}
}

func TestPageParityHeadersAndUnknown(t *testing.T) {
	p := parityPage(t, "https://example.com/", `<main>Content</main>`)
	headers := http.Header{"Strict-Transport-Security": []string{"max-age=31536000; includeSubDomains"}, "Content-Security-Policy": []string{"default-src 'self'"}, "X-Content-Type-Options": []string{"nosniff"}, "Referrer-Policy": []string{"strict-origin-when-cross-origin"}, "Cache-Control": []string{"private, no-store"}}
	for _, id := range []string{"seo.transport_security", "seo.csp", "seo.nosniff", "seo.referrer_policy"} {
		if f := parityByID(t, pageParityChecks(p, headers), id); f.Status != "pass" {
			t.Fatalf("%+v", f)
		}
		if f := parityByID(t, pageParityChecks(p, nil), id); f.Status != "fail" {
			t.Fatalf("missing header: %+v", f)
		}
	}
	headers.Set("Strict-Transport-Security", "max-age=0")
	headers.Set("Content-Security-Policy", "report-uri /reports")
	headers.Set("Referrer-Policy", "unsafe-url")
	for id, want := range map[string]string{"seo.transport_security": "fail", "seo.csp": "partial", "seo.referrer_policy": "partial", "seo.compression": "information", "seo.cache_policy": "information"} {
		if f := parityByID(t, pageParityChecks(p, headers), id); f.Status != want {
			t.Fatalf("%+v", f)
		}
	}
	p.page.URL = "http://example.com/"
	if f := parityByID(t, pageParityChecks(p, headers), "seo.transport_security"); f.Status != "not_applicable" {
		t.Fatalf("%+v", f)
	}
	p = analyze(response{}, errors.New("request timed out"), "https://example.com/", 0)
	for _, f := range pageParityChecks(p, headers) {
		if f.Status != "unknown" || f.EvidenceLevel != "" {
			t.Fatalf("unobserved response scored: %+v", f)
		}
	}
}

func TestPageParityRenderBlockingCandidates(t *testing.T) {
	body := `<head><script src="/blocking.js"></script><script src="/deferred.js" defer></script><script src="/module.js" type="module"></script><link rel="stylesheet" href="/main.css"><link rel="stylesheet" href="/print.css" media="print"></head><body><script src="/late.js"></script></body>`
	f := parityByID(t, pageParityChecks(parityPage(t, "https://example.com/", body), nil), "seo.render_blocking")
	if f.Status != "information" || !strings.Contains(f.Evidence, "1 external classic scripts") || !strings.Contains(f.Evidence, "1 screen/all stylesheets") {
		t.Fatalf("%+v", f)
	}
}

func TestPageParityHSTSDuplicateIsNotValidPolicy(t *testing.T) {
	p := parityPage(t, "https://example.com/", `<main>Content</main>`)
	for _, value := range []string{"max-age=garbage", "max-age=1; max-age=0", "max-age=0; max-age=31536000", `max-age="""31536000`} {
		f := parityByID(t, pageParityChecks(p, http.Header{"Strict-Transport-Security": []string{value}}), "seo.transport_security")
		if f.Status != "fail" {
			t.Fatalf("%s: %+v", value, f)
		}
	}
}

func TestPageParityInformationalSignalsDoNotClaimQuality(t *testing.T) {
	body := `<h2>How does it work?</h2><details><summary>What is the price?</summary>Ten dollars.</details><table><tr><td>A</td></tr></table><time datetime="2026-01-01">Updated</time><a href="https://sources.example.com">Source</a><a href="/private" rel="nofollow">Private</a>`
	findings := pageParityChecks(parityPage(t, "https://example.com/", body), nil)
	for _, id := range []string{"geo.content_signals", "seo.internal_nofollow", "seo.html_weight", "seo.compression", "seo.cache_policy", "seo.permissions_policy"} {
		f := parityByID(t, findings, id)
		if f.Weight != 0 || f.Status != "information" || f.Applicability == "" || f.Remediation == "" {
			t.Fatalf("%+v", f)
		}
	}
	f := parityByID(t, findings, "geo.content_signals")
	if !strings.Contains(f.Evidence, "2 question-shaped") || !strings.Contains(f.Evidence, "not establish answer quality") {
		t.Fatalf("%+v", f)
	}
}
