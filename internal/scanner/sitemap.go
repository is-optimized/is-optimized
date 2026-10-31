package scanner

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"
)

type sitemapLocations struct {
	root  string
	pages []string
	files []string
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

func appendLimited(dst []string, value string, limit int) []string {
	if len(dst) >= limit {
		return dst
	}
	return append(dst, value)
}

func sitemapURLs(ss *session, home *url.URL) ([]string, string) {
	policy := ss.getPolicy(home)
	if policy.err != nil || len(policy.rules.sitemaps) == 0 {
		return nil, "No sitemap declared in the retrieved robots policy"
	}

	originURL := origin(home)
	queue := append([]string(nil), policy.rules.sitemaps...)
	visited := map[string]bool{}
	var pages []string
	documents := 0
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
		if err != nil || resp.code != 200 {
			continue
		}
		parsed, err := parseSitemap(resp.body, maxSitemapCandidates-len(pages))
		if err != nil {
			continue
		}
		if parsed.root == "urlset" {
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
	return pages, "Inspected " + strconv.Itoa(documents) + " declared sitemap document(s); found " + strconv.Itoa(len(pages)) + " same-origin page candidates"
}
