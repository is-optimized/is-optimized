package scanner

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParseSitemapURLSetAndLimit(t *testing.T) {
	body := []byte(`<?xml version="1.0"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://example.com/a</loc></url><url><loc>https://example.com/b</loc></url></urlset>`)
	got, err := parseSitemap(body, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.root != "urlset" || len(got.pages) != 1 || got.pages[0] != "https://example.com/a" {
		t.Fatalf("unexpected bounded sitemap parse: %#v", got)
	}
}

func TestParseSitemapIndex(t *testing.T) {
	body := []byte(`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><sitemap><loc>https://example.com/child.xml</loc></sitemap></sitemapindex>`)
	got, err := parseSitemap(body, 5)
	if err != nil {
		t.Fatal(err)
	}
	if got.root != "sitemapindex" || len(got.files) != 1 || got.files[0] != "https://example.com/child.xml" {
		t.Fatalf("unexpected sitemap index parse: %#v", got)
	}
}

func TestParseSitemapFreshnessSample(t *testing.T) {
	body := []byte(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://example.com/a</loc><lastmod>2026-11-01</lastmod></url><url><loc>https://example.com/b</loc></url></urlset>`)
	got, err := parseSitemap(body, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got.pageEntries != 2 || got.pageLastmod != 1 || got.newestLastmod.Format("2006-01-02") != "2026-11-01" {
		t.Fatalf("unexpected freshness sample: %+v", got)
	}
}

func TestSitemapFallbackProducesQualityAndFreshnessFindings(t *testing.T) {
	home, _ := url.Parse("https://example.com/")
	modified := time.Now().UTC().Format(time.RFC3339)
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/robots.txt":
			return reply(r, http.StatusNotFound, "", "text/plain"), nil
		case "/sitemap.xml":
			body := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://example.com/a</loc><lastmod>` + modified + `</lastmod></url></urlset>`
			return reply(r, http.StatusOK, body, "application/xml"), nil
		default:
			return reply(r, http.StatusNotFound, "", "text/plain"), nil
		}
	})
	ss := &session{s: s, ctx: context.Background(), policies: map[string]policy{}}
	pages, evidence, findings := sitemapURLs(ss, home)
	if len(pages) != 1 || pages[0] != "https://example.com/a" || !strings.Contains(evidence, "conventional paths were tried") {
		t.Fatalf("fallback sitemap was not sampled: pages=%v evidence=%s", pages, evidence)
	}
	statuses := map[string]string{}
	for _, f := range findings {
		statuses[f.ID] = f.Status
	}
	if statuses["seo.sitemap_quality"] != "pass" || statuses["seo.sitemap_freshness"] != "pass" {
		t.Fatalf("sitemap findings=%v", statuses)
	}
}
