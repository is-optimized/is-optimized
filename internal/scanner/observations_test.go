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
