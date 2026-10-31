package scanner

import "testing"

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
