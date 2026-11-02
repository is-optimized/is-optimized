package scanner

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
)

type sitemapLocations struct {
	root          string
	pages         []string
	files         []string
	pageEntries   int
	pageLastmod   int
	newestLastmod time.Time
}

func parseSitemap(body []byte, limit int) (sitemapLocations, error) {
	var out sitemapLocations
	if limit < 1 {
		return out, errors.New("invalid sitemap candidate limit")
	}
	d := xml.NewDecoder(bytes.NewReader(body))
	var stack []string
	for {
		tok, err := d.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return out, errors.New("invalid sitemap XML")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if len(stack) == 0 {
				out.root = t.Name.Local
				if t.Name.Space != "http://www.sitemaps.org/schemas/sitemap/0.9" {
					return out, errors.New("unsupported sitemap namespace")
				}
			}
			if len(stack) >= 32 {
				return out, errors.New("sitemap nesting limit exceeded")
			}
			parent := ""
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			if t.Name.Local == "url" && parent == "urlset" && out.root == "urlset" {
				out.pageEntries++
			}
			if t.Name.Local == "lastmod" && parent == "url" && out.root == "urlset" {
				var raw string
				if err := d.DecodeElement(&raw, &t); err != nil {
					return out, errors.New("invalid sitemap lastmod")
				}
				if out.pageEntries <= 500 {
					if value, ok := parseSitemapTime(strings.TrimSpace(raw)); ok {
						out.pageLastmod++
						if value.After(out.newestLastmod) {
							out.newestLastmod = value
						}
					}
				}
				continue
			}
			if t.Name.Local == "loc" && (parent == "url" || parent == "sitemap") {
				var loc string
				if err := d.DecodeElement(&loc, &t); err != nil {
					return out, errors.New("invalid sitemap location")
				}
				loc = strings.TrimSpace(loc)
				if loc != "" {
					if parent == "url" && out.root == "urlset" {
						out.pages = appendLimited(out.pages, loc, limit)
					} else if parent == "sitemap" && out.root == "sitemapindex" {
						out.files = appendLimited(out.files, loc, limit)
					}
				}
				continue
			}
			stack = append(stack, t.Name.Local)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
		if len(out.pages)+len(out.files) >= limit {
			break
		}
	}
	if out.root != "urlset" && out.root != "sitemapindex" {
		return out, errors.New("unsupported sitemap root")
	}
	return out, nil
}

func parseSitemapTime(raw string) (time.Time, bool) {
	if value, err := time.Parse(time.RFC3339, raw); err == nil {
		return value, true
	}
	if value, err := time.Parse("2006-01-02", raw); err == nil {
		return value, true
	}
	return time.Time{}, false
}

func appendLimited(dst []string, value string, limit int) []string {
	if len(dst) >= limit {
		return dst
	}
	return append(dst, value)
}

func sitemapURLs(ss *session, home *url.URL) ([]string, string, []reportmodel.Finding) {
	base := origin(home)
	policy := ss.getPolicy(home)
	quality := reportmodel.Finding{ID: "seo.sitemap_quality", Pillar: "seo", Title: "Sitemap validity and same-site URLs", Status: "unknown", Weight: 2, Tier: "recommended", Stage: "find", URL: base + "/robots.txt", Applicability: "Assesses a public sitemap declared in robots.txt or published at a conventional path.", Remediation: "Publish a valid sitemap XML document containing canonical same-site URLs, and declare it in robots.txt."}
	freshness := reportmodel.Finding{ID: "seo.sitemap_freshness", Pillar: "seo", Title: "Sitemap lastmod freshness", Status: "not_applicable", Weight: 1, Tier: "bonus", Stage: "find", URL: base, Applicability: "Bonus observation when a valid sitemap contains URL entries.", Remediation: "Include truthful lastmod dates for recently updated sitemap entries."}
	if policy.err != nil {
		quality.Evidence = "robots.txt policy unavailable or malformed; sitemap declarations and fallback access could not be checked."
		freshness.Evidence = quality.Evidence
		return nil, quality.Evidence, []reportmodel.Finding{quality, freshness}
	}
	freshness.Evidence = "No valid sitemap URL set was observed; freshness bonus is not applicable."
	originURL := origin(home)
	queue := append([]string(nil), policy.rules.sitemaps...)
	for _, fallback := range []string{originURL + "/sitemap.xml", originURL + "/sitemap_index.xml"} {
		queue = append(queue, fallback)
	}
	visited := map[string]bool{}
	var pages []string
	documents := 0
	validDocuments, sampledEntries, sampledLastmods := 0, 0, 0
	var newestLastmod time.Time
	fetchFailures := 0
	for len(queue) > 0 && documents < maxSitemapDocuments && len(pages) < maxSitemapCandidates && ss.discovery.Load() < maxDiscoveryCount {
		raw := queue[0]
		queue = queue[1:]
		u, err := url.Parse(raw)
		if err != nil || validateURL(u) != nil || origin(u) != originURL || visited[u.String()] {
			continue
		}
		visited[u.String()] = true
		documents++
		resp, err := ss.probe(u.String(), originURL)
		if err != nil {
			fetchFailures++
			continue
		}
		if resp.code != 200 {
			continue
		}
		parsed, err := parseSitemap(resp.body, maxSitemapCandidates-len(pages))
		if err != nil {
			continue
		}
		validDocuments++
		if parsed.root == "urlset" {
			if sampledEntries == 0 {
				sampledEntries = min(parsed.pageEntries, 500)
				sampledLastmods = parsed.pageLastmod
				newestLastmod = parsed.newestLastmod
			}
			for _, candidate := range parsed.pages {
				if pageURL, ok := candidateURL(candidate, home); ok {
					pages = appendLimited(pages, pageURL.String(), maxSitemapCandidates)
				}
			}
		} else {
			for _, child := range parsed.files {
				childURL, err := url.Parse(child)
				if err == nil && validateURL(childURL) == nil && origin(childURL) == originURL && !visited[childURL.String()] {
					queue = appendLimited(queue, childURL.String(), maxSitemapCandidates)
				}
			}
		}
	}
	evidence := "Inspected " + strconv.Itoa(documents) + " sitemap document(s); validated " + strconv.Itoa(validDocuments) + "; found " + strconv.Itoa(len(pages)) + " same-origin page candidates"
	if len(policy.rules.sitemaps) > 0 {
		evidence += "; robots.txt declared " + strconv.Itoa(len(policy.rules.sitemaps)) + " sitemap URL(s)"
	} else {
		evidence += "; no Sitemap directive observed, conventional paths were tried"
	}
	// Do not persist the sitemap URL itself; declared endpoints can contain
	// query credentials. The root page and count-based evidence are sufficient.
	quality.Evidence = evidence
	if validDocuments > 0 && len(pages) > 0 {
		quality.Status = "pass"
	} else if validDocuments > 0 {
		quality.Status = "partial"
	} else if fetchFailures > 0 || documents == 0 {
		quality.Status = "unknown"
	} else {
		quality.Status = "fail"
	}
	if validDocuments > 0 && sampledEntries > 0 {
		freshness.Evidence = fmt.Sprintf("%d of %d entries in the first sample have parseable lastmod values; newest is %s", sampledLastmods, sampledEntries, newestLastmod.UTC().Format("2006-01-02"))
		age := time.Since(newestLastmod)
		if sampledLastmods*100 >= sampledEntries*80 && !newestLastmod.IsZero() && age >= 0 && age <= 90*24*time.Hour {
			freshness.Status = "pass"
		} else {
			freshness.Status = "fail"
		}
	}
	return pages, evidence, []reportmodel.Finding{quality, freshness}
}
