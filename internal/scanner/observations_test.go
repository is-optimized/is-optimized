package scanner

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func observed(t *testing.T, raw, body string) pageResult {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return analyze(response{url: u, code: 200, body: []byte(body), contentType: "text/html", header: http.Header{}}, nil, raw, 0)
}
func TestSEOObservations(t *testing.T) {
	p := observed(t, "https://example.com/", `<title>Home</title><meta name="viewport" content="width = device-width,initial-scale=1"><link rel="alternate canonical" href="https://example.com/"><h1>Home</h1><h3>Skipped</h3><script>not counted</script><p>one two</p>`)
	got := map[string]string{}
	for _, f := range evaluate(p) {
		got[f.ID] = f.Status
	}
	if got["seo.headings"] != "partial" || got["seo.viewport"] != "pass" || got["seo.canonical"] != "pass" || got["seo.words"] != "information" || got["seo.hsts"] != "information" {
		t.Fatalf("statuses: %v", got)
	}
	for _, body := range []string{`<h2>Only subtitle</h2>`, `<h1> </h1>`} {
		for _, f := range evaluate(observed(t, "https://example.com/", body)) {
			if f.ID == "seo.headings" && f.Status != "fail" {
				t.Fatalf("missing/empty H1: %+v", f)
			}
		}
	}
	p = analyze(response{}, errors.New("blocked by policy"), "https://example.com/", 0)
	for _, f := range evaluate(p) {
		if f.Status != "unknown" {
			t.Fatalf("unobserved page: %+v", f)
		}
	}
}

func TestContentBudgetAndTextRatioStatuses(t *testing.T) {
	for _, tc := range []struct {
		name, body, budget, ratio string
	}{
		{name: "small readable page", body: `<main><p>` + strings.Repeat("useful ", 40) + `</p></main>`, budget: "pass", ratio: "pass"},
		{name: "large main text", body: `<main><p>` + strings.Repeat("x", 60004) + `</p></main>`, budget: "partial", ratio: "pass"},
		{name: "oversized main text", body: `<main><p>` + strings.Repeat("x", 120004) + `</p></main>`, budget: "fail", ratio: "pass"},
		{name: "low text ratio", body: `<main>` + strings.Repeat(`<span data-payload="`+strings.Repeat("x", 100)+`">x</span>`, 10) + `</main>`, budget: "pass", ratio: "partial"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := map[string]string{}
			for _, f := range evaluate(observed(t, "https://example.com/", tc.body)) {
				if f.ID == "agent.content_budget" || f.ID == "geo.content_ratio" {
					got[f.ID] = f.Status
				}
			}
			if got["agent.content_budget"] != tc.budget || got["geo.content_ratio"] != tc.ratio {
				t.Fatalf("unexpected content statuses: %v", got)
			}
		})
	}
	p := analyze(response{}, errors.New("blocked"), "https://example.com/", 0)
	for _, f := range evaluate(p) {
		if (f.ID == "agent.content_budget" || f.ID == "geo.content_ratio") && f.Status != "unknown" {
			t.Fatalf("unobserved page generated a content result: %+v", f)
		}
	}
}

func TestArticleAttributionApplicabilityRequiresArticleEvidence(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{name: "generic article container", body: `<article><h1>Product overview</h1><p>Useful copy.</p></article>`, want: "not_applicable"},
		{name: "Open Graph article", body: `<meta property="og:type" content="article"><h1>News</h1>`, want: "partial"},
		{name: "BlogPosting JSON-LD", body: `<script type="application/ld+json">{"@context":"https://schema.org","@type":"BlogPosting","headline":"News"}</script>`, want: "partial"},
		{name: "NewsArticle JSON-LD graph", body: `<script type="application/ld+json">{"@graph":[{"@type":["Thing","NewsArticle"]}]}</script>`, want: "partial"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, f := range evaluate(observed(t, "https://example.com/", tc.body)) {
				if f.ID == "geo.author" {
					if f.Status != tc.want {
						t.Fatalf("geo.author status=%q, want %q (%s)", f.Status, tc.want, f.Evidence)
					}
					return
				}
			}
			t.Fatal("geo.author finding missing")
		})
	}
}

func TestDuplicateMetadataAndRedirectAliases(t *testing.T) {
	a := observed(t, "https://example.com/", `<title>Same TITLE</title><meta name="description" content=" Same text ">`)
	b := observed(t, "https://example.com/about", `<title>same title</title><meta name="description" content="same text">`)
	alias := a
	alias.page.RequestedURL = "https://example.com/old"
	findings := markDuplicates(append(evaluate(a), evaluate(b)...), []pageResult{a, b, alias})
	for _, f := range findings {
		if f.ID == "seo.title" || f.ID == "seo.description" {
			if f.Status != "partial" || !strings.Contains(f.Evidence, "2 distinct") {
				t.Fatalf("duplicate: %+v", f)
			}
		}
	}
	findings = markDuplicates(evaluate(a), []pageResult{a, alias})
	for _, f := range findings {
		if f.ID == "seo.title" && f.Status != "pass" {
			t.Fatalf("alias counted twice: %+v", f)
		}
	}
	// Values equal only after evidence truncation must not be considered duplicates.
	long := strings.Repeat("x", 350)
	a = observed(t, "https://example.com/", "<title>"+long+"a</title>")
	b = observed(t, "https://example.com/about", "<title>"+long+"b</title>")
	for _, f := range markDuplicates(append(evaluate(a), evaluate(b)...), []pageResult{a, b}) {
		if f.ID == "seo.title" && f.Status != "pass" {
			t.Fatalf("truncated metadata compared: %+v", f)
		}
	}
}
func TestCanonicalMultiplicity(t *testing.T) {
	for _, body := range []string{`<link rel="canonical" href="https://example.com/"><link rel="canonical" href="https://example.com/other">`, `<link rel="canonical" href="%%%">`, ``} {
		for _, f := range evaluate(observed(t, "https://example.com/", body)) {
			if f.ID == "seo.canonical" && f.Status != "partial" {
				t.Fatalf("canonical: %+v", f)
			}
		}
	}
}
