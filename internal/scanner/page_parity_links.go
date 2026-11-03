package scanner

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"golang.org/x/net/html"
)

// pageLinkParityChecks reuses page samples and permits at most four additional
// same-origin safe GET probes. It runs after essential site checks, never
// recursively crawls targets, and shares all existing request/byte/robots limits.
func pageLinkParityChecks(ss *session, pages []pageResult, home *url.URL) []reportmodel.Finding {
	if home == nil || len(pages) == 0 {
		return nil
	}
	const maxExtraProbes = 4
	const maxLinkSample = 20
	cache := map[string]pageResult{}
	for _, p := range pages {
		cache[p.page.URL] = p
		if p.page.RequestedURL != "" {
			cache[p.page.RequestedURL] = p
		}
	}
	extra := 0
	get := func(target *url.URL) (pageResult, bool) {
		if p, ok := cache[target.String()]; ok {
			return p, true
		}
		_, safeTarget := candidateURL(target.String(), home)
		if !safeTarget || origin(target) != origin(home) || extra >= maxExtraProbes-1 || ss.discovery.Load() >= maxDiscoveryCount {
			return pageResult{}, false
		}
		extra++
		r, err := ss.probe(target.String(), origin(home))
		p := analyze(r, err, target.String(), 0)
		cache[target.String()] = p
		return p, true
	}
	var out []reportmodel.Finding
	for _, p := range pages {
		f := pageParityFinding(p, "seo.canonical_target", "seo", "Canonical target response", `Point the canonical at the intended successful public URL and keep the target self-consistent: <link rel="canonical" href="https://example.com/page">.`, "https://www.rfc-editor.org/rfc/rfc6596", 1)
		f.Applicability = "Applies to declared same-origin HTML canonical targets; checks sampled targets or a bounded safe GET, not search-engine canonical selection."
		if p.doc == nil || p.page.Error != "" {
			f.Evidence = "Source page HTML not observed."
			out = append(out, f)
			continue
		}
		canonical := pageHTMLCanonical(p)
		if canonical == "" {
			f.Status = "not_applicable"
			f.Evidence = "No HTML canonical target declared; the existing canonical-declaration check covers absence."
			out = append(out, f)
			continue
		}
		target, ok := pageDeclaredURL(canonical, pageURL(p.page.URL))
		if !ok {
			f.Status = "fail"
			f.Evidence = "Declared canonical is not a valid public HTTP(S) URL."
			out = append(out, f)
			continue
		}
		targetPage, observed := get(target)
		f.EvidenceLevel = ""
		if !observed {
			f.Evidence = "Canonical target not observed: outside same-origin boundary or bounded probe budget exhausted."
			out = append(out, f)
			continue
		}
		f.Evidence = fmt.Sprintf("Declared target %s returned HTTP %d.", excerpt(target.String()), targetPage.page.StatusCode)
		f.EvidenceLevel = "observed_response"
		switch {
		case targetPage.page.StatusCode == http.StatusNotFound || targetPage.page.StatusCode == http.StatusGone:
			f.Status = "fail"
		case targetPage.page.StatusCode != http.StatusOK || targetPage.page.Error != "":
			f.Status = "unknown"
			f.Evidence += " Target content unavailable: " + excerpt(targetPage.page.Error)
		default:
			f.Status = "partial"
			if declared := pageHTMLCanonical(targetPage); declared != "" {
				if own, ok := pageDeclaredURL(declared, pageURL(targetPage.page.URL)); ok && own.String() == targetPage.page.URL {
					f.Status = "pass"
					f.Evidence += " Target HTML canonical points to its final URL."
				} else {
					f.Evidence += " Target HTML canonical points elsewhere or is invalid."
				}
			} else {
				f.Evidence += " Target HTML has no canonical; self-consistency not established."
			}
		}
		out = append(out, f)
	}
	broken := pageParityFinding(pages[0], "seo.internal_link_health", "seo", "Sampled internal link responses", `Repair or remove broken navigation links; link directly to successful public pages, for example <a href="/docs">Documentation</a>.`, "https://html.spec.whatwg.org/multipage/links.html", 1)
	broken.Applicability = "Checks up to twenty safe same-origin HTML links from sampled pages; excludes action-like/query links and does not establish whole-site link health."
	seen := map[string]bool{}
	var links []string
	for _, p := range pages {
		if p.doc != nil && p.page.Error == "" {
			for _, raw := range selectLinks(p.doc, pageURL(p.page.URL)) {
				if len(links) >= maxLinkSample {
					break
				}
				if !seen[raw] {
					seen[raw] = true
					links = append(links, raw)
				}
			}
		}
	}
	good, bad, unknown := 0, 0, 0
	for _, raw := range links {
		u, _ := url.Parse(raw)
		p, observed := get(u)
		if !observed {
			unknown++
			continue
		}
		switch {
		case p.page.StatusCode == 404 || p.page.StatusCode == 410:
			bad++
		case p.page.StatusCode == 200 && p.page.Error == "":
			good++
		default:
			unknown++
		}
	}
	broken.Status = "unknown"
	if len(links) == 0 {
		broken.Status = "not_applicable"
	} else if bad > 0 {
		broken.Status = "fail"
		if good > 0 {
			broken.Status = "partial"
		}
	} else if unknown == 0 {
		broken.Status = "pass"
	}
	broken.Evidence = fmt.Sprintf("Among %d selected internal links: %d successful, %d HTTP 404/410, %d unobserved/restricted. Reused sampled responses and at most %d additional same-origin GET probes shared with canonical/icon checks.", len(links), good, bad, unknown, maxExtraProbes)
	if broken.Status == "unknown" {
		broken.EvidenceLevel = ""
	}
	out = append(out, broken)
	icon := pageParityFinding(pages[0], "seo.favicon", "seo", "Public site icon", `Publish a real icon and advertise it: <link rel="icon" href="/favicon.ico">.`, "https://html.spec.whatwg.org/multipage/links.html#rel-icon", 1)
	icon.Applicability = "Checks the first advertised same-origin icon or /favicon.ico for an image Content-Type and nonempty body; visual quality not assessed."
	iconURL := origin(home) + "/favicon.ico"
	advertised := false
	walk(pages[0].doc, func(n *html.Node) {
		if advertised || n.Type != html.ElementNode || n.Data != "link" {
			return
		}
		if hasToken(attr(n, "rel"), "icon") {
			if u, ok := pageDeclaredURL(attr(n, "href"), home); ok {
				iconURL = u.String()
				advertised = true
			}
		}
	})
	u, _ := url.Parse(iconURL)
	if pages[0].doc == nil || pages[0].page.Error != "" {
		icon.Evidence = "Homepage HTML unavailable; icon not probed."
	} else if origin(u) != origin(home) || u.RawQuery != "" || extra >= maxExtraProbes || ss.discovery.Load() >= maxDiscoveryCount {
		icon.Evidence = "Icon probe not observed: same-origin boundary or bounded probe budget."
	} else {
		extra++
		r, err := ss.probe(iconURL, origin(home))
		icon.URL = iconURL
		if err != nil {
			icon.Evidence = "Icon fetch not observed: " + err.Error()
		} else {
			icon.EvidenceLevel = "observed_response"
			icon.Evidence = fmt.Sprintf("Icon returned HTTP %d, Content-Type %s, %d bytes.", r.code, excerpt(r.contentType), len(r.body))
			switch {
			case r.code == 404 || r.code == 410:
				icon.Status = "fail"
			case r.code >= 500 || r.code == 401 || r.code == 403:
				icon.Status = "unknown"
			case r.code == 200 && strings.HasPrefix(strings.ToLower(r.contentType), "image/") && len(r.body) > 0:
				icon.Status = "pass"
			default:
				icon.Status = "fail"
			}
		}
	}
	if icon.Status == "unknown" {
		icon.EvidenceLevel = ""
	}
	return append(out, icon)
}

func pageDeclaredURL(raw string, base *url.URL) (*url.URL, bool) {
	if base == nil || strings.TrimSpace(raw) == "" {
		return nil, false
	}
	u, err := base.Parse(strings.TrimSpace(raw))
	if err != nil || validateURL(u) != nil {
		return nil, false
	}
	u.Fragment = ""
	u.RawFragment = ""
	return u, true
}

func pageHTMLCanonical(p pageResult) string {
	canonical := ""
	walk(p.doc, func(n *html.Node) {
		if canonical == "" && n.Type == html.ElementNode && n.Data == "link" && hasToken(attr(n, "rel"), "canonical") {
			canonical = strings.TrimSpace(attr(n, "href"))
		}
	})
	return canonical
}
