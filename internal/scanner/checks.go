package scanner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"golang.org/x/net/html"
)

type pageResult struct {
	page         reportmodel.Page
	doc          *html.Node
	headerRobots string
}

func analyze(r response, err error, requested string, duration time.Duration) pageResult {
	p := pageResult{page: reportmodel.Page{URL: requested, StatusCode: r.code, DurationMS: duration.Milliseconds()}}
	if r.url != nil {
		p.page.URL = r.url.String()
	}
	if err != nil {
		p.page.Error = err.Error()
		return p
	}
	if r.code != 200 {
		p.page.Error = fmt.Sprintf("HTTP %d; page content not evaluated", r.code)
		return p
	}
	if strings.EqualFold(r.header.Get("Cf-Mitigated"), "challenge") {
		p.page.Error = "Bot challenge response; content unobserved"
		return p
	}
	if !htmlType(r.contentType) {
		p.page.Error = "Response is not HTML"
		return p
	}
	// Bound the parser's token count and nesting before constructing its tree.
	z := html.NewTokenizer(bytes.NewReader(r.body))
	depth, count := 0, 0
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			break
		}
		count++
		if count > 50000 {
			p.page.Error = "HTML token limit exceeded"
			return p
		}
		if kind == html.StartTagToken {
			name, _ := z.TagName()
			switch string(name) {
			case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr":
			default:
				depth++
			}
		}
		if kind == html.EndTagToken && depth > 0 {
			depth--
		}
		if depth > 512 {
			p.page.Error = "HTML nesting limit exceeded"
			return p
		}
	}
	p.doc, err = html.Parse(bytes.NewReader(r.body))
	if err != nil {
		p.page.Error = "HTML parse failed"
	}
	if err == nil {
		walk(p.doc, func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "title" {
				title := strings.ToLower(strings.TrimSpace(text(n)))
				if title == "just a moment..." || title == "access denied" || title == "attention required! | cloudflare" {
					p.page.Error = "Possible access challenge; content unobserved"
				}
			}
		})
	}
	p.headerRobots = r.header.Get("X-Robots-Tag")
	return p
}
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func walk(n *html.Node, f func(*html.Node)) {
	if n == nil {
		return
	}
	stack := []*html.Node{n}
	for len(stack) > 0 {
		x := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		f(x)
		for c := x.LastChild; c != nil; c = c.PrevSibling {
			stack = append(stack, c)
		}
	}
}
func text(n *html.Node) string {
	var b strings.Builder
	walk(n, func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
			b.WriteByte(' ')
		}
	})
	return strings.Join(strings.Fields(b.String()), " ")
}
func selectLinks(doc *html.Node, base *url.URL) []string {
	var links []string
	seen := map[string]bool{base.String(): true}
	walk(doc, func(n *html.Node) {
		if len(links) == 4 || n.Type != html.ElementNode || n.Data != "a" || attr(n, "download") != "" {
			return
		}
		raw := strings.TrimSpace(attr(n, "href"))
		if raw == "" {
			return
		}
		u, e := base.Parse(raw)
		if e != nil || validateURL(u) != nil || origin(u) != origin(base) || u.RawQuery != "" {
			return
		}
		u.Fragment = ""
		u.RawFragment = ""
		if u.Path == "" {
			u.Path = "/"
		}
		if strings.Contains(u.Path, "//") {
			return
		}
		ext := strings.ToLower(path.Ext(u.Path))
		if ext != "" && ext != ".html" && ext != ".htm" && ext != ".php" && ext != ".asp" && ext != ".aspx" {
			return
		}
		lower := strings.ToLower(u.Path)
		for _, unsafe := range []string{"logout", "log-out", "signout", "sign-out", "delete", "remove", "unsubscribe", "checkout", "cart", "purchase", "buy-now", "/admin", "/login", "/signin"} {
			if strings.Contains(lower, unsafe) {
				return
			}
		}
		// Treat an explicit download attribute, including the empty boolean form, as a download.
		for _, a := range n.Attr {
			if a.Key == "download" {
				return
			}
		}
		if !seen[u.String()] {
			seen[u.String()] = true
			links = append(links, u.String())
		}
	})
	return links
}

type Check struct {
	ID, Pillar, Title, Remediation string
	Weight                         float64
	Foundational                   bool
}

// Registry is the complete MVP weighted check set. Applicability and evaluation
// are encoded below; changes require a new MethodologyVersion.
func Registry() []Check {
	return []Check{
		{"seo.access", "seo", "Public HTML response", "Ensure intended public pages return useful HTML with a successful HTTP status.", 3, true},
		{"seo.index", "seo", "Index directives", "Review robots directives against your intended indexing policy; remove unintended noindex directives.", 2, false},
		{"seo.title", "seo", "Page title", "Give this page a concise, descriptive title that distinguishes its purpose.", 2, false},
		{"seo.description", "seo", "Search description", "Write an accurate page-specific meta description; do not promise that search engines will display it.", 1, false},
		{"seo.canonical", "seo", "Canonical reference", "Provide one valid canonical URL that reflects the intended public version of this page.", 1, false},
		{"seo.images", "seo", "Image text alternatives", "Provide useful alternatives for informative images and empty alt attributes for decorative images.", 1, false},
		{"geo.access", "geo", "Extractable public response", "Expose intended public information in server-delivered HTML.", 3, true},
		{"geo.entity", "geo", "Structured entity data", "Add accurate, applicable Schema.org JSON-LD based on verified business or content facts.", 2, false},
		{"geo.structure", "geo", "Structured information", "Use meaningful headings and semantic lists, tables, or definition lists where they clarify the content.", 2, false},
		{"geo.author", "geo", "Article attribution", "For article content, identify the author and publish or update date accurately.", 1, false},
		{"agent.access", "agent", "Initial HTML readability", "Serve meaningful public text in initial HTML so readers need not execute JavaScript to understand the page.", 3, true},
		{"agent.landmarks", "agent", "Navigation landmarks", "Use main and navigation landmarks to identify page content and navigation.", 1, false},
		{"agent.controls", "agent", "Named interactive controls", "Give interactive controls accessible names through visible labels or appropriate ARIA attributes.", 2, false},
		{"agent.links", "agent", "Named navigation links", "Give navigation links meaningful visible or accessible names.", 1, false},
	}
}
func evaluate(p pageResult) []reportmodel.Finding {
	registry := Registry()
	out := make([]reportmodel.Finding, 0, len(registry))
	var title, description, robotsMeta, canonical string
	canonCount := 0
	images, missingAlt, controls, unnamed, links, emptyLinks := 0, 0, 0, 0, 0, 0
	heading, structured, main, nav, article, author, dated, entity := false, false, false, false, false, false, false, false
	var visible strings.Builder
	labels := map[string]string{}
	ids := map[string]string{}
	walk(p.doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if id := attr(n, "id"); id != "" {
			ids[id] = text(n)
		}
		if n.Data == "label" && attr(n, "for") != "" {
			labels[attr(n, "for")] = text(n)
		}
	})
	accessibleName := func(n *html.Node) bool {
		if strings.TrimSpace(attr(n, "aria-label")) != "" {
			return true
		}
		for _, id := range strings.Fields(attr(n, "aria-labelledby")) {
			if strings.TrimSpace(ids[id]) != "" {
				return true
			}
		}
		if labels[attr(n, "id")] != "" {
			return true
		}
		if (n.Data == "button" || n.Data == "a") && text(n) != "" {
			return true
		}
		if n.Data == "input" && (attr(n, "type") == "submit" || attr(n, "type") == "button") && attr(n, "value") != "" {
			return true
		}
		for a := n.Parent; a != nil; a = a.Parent {
			if a.Type == html.ElementNode && a.Data == "label" && text(a) != "" {
				return true
			}
		}
		return false
	}
	walk(p.doc, func(n *html.Node) {
		if n.Type == html.TextNode {
			skip := false
			for a := n.Parent; a != nil; a = a.Parent {
				if a.Data == "script" || a.Data == "style" || a.Data == "head" {
					skip = true
					break
				}
			}
			if !skip {
				visible.WriteString(n.Data)
				visible.WriteByte(' ')
			}
		}
		if n.Type != html.ElementNode {
			return
		}
		switch n.Data {
		case "title":
			title = text(n)
		case "meta":
			switch strings.ToLower(attr(n, "name")) {
			case "description":
				description = attr(n, "content")
			case "robots":
				robotsMeta = attr(n, "content")
			case "author":
				author = attr(n, "content") != ""
			}
		case "link":
			if strings.EqualFold(attr(n, "rel"), "canonical") {
				canonical = attr(n, "href")
				canonCount++
			}
		case "img":
			images++
			present := false
			for _, a := range n.Attr {
				if a.Key == "alt" {
					present = true
				}
			}
			if !present {
				missingAlt++
			}
		case "h1", "h2", "h3", "h4", "h5", "h6":
			if text(n) != "" {
				heading = true
			}
		case "ul", "ol", "table", "dl":
			if text(n) != "" {
				structured = true
			}
		case "main":
			main = true
		case "nav":
			nav = true
		case "article":
			article = true
		case "time":
			dated = attr(n, "datetime") != ""
		case "a":
			if attr(n, "href") != "" {
				links++
				if !accessibleName(n) {
					emptyLinks++
				}
			}
		case "button", "input", "select", "textarea":
			if n.Data == "input" && strings.EqualFold(attr(n, "type"), "hidden") {
				return
			}
			controls++
			if !accessibleName(n) {
				unnamed++
			}
		case "script":
			if strings.EqualFold(attr(n, "type"), "application/ld+json") {
				var value any
				if json.Unmarshal([]byte(text(n)), &value) == nil {
					entity = entity || hasEntity(value)
					article = article || hasType(value, "Article")
				}
			}
		}
	})
	meaningful := len(strings.Fields(visible.String())) >= 20
	for _, c := range registry {
		f := reportmodel.Finding{ID: c.ID, Pillar: c.Pillar, Title: c.Title, URL: p.page.URL, Weight: c.Weight, Foundational: c.Foundational, Remediation: c.Remediation, Status: "pass"}
		if p.page.Error != "" {
			f.Status = "unknown"
			f.Evidence = p.page.Error
			out = append(out, f)
			continue
		}
		switch c.ID {
		case "seo.access", "geo.access":
			f.Evidence = "HTTP 200 HTML response"
		case "seo.index":
			f.Evidence = excerpt("Meta: " + robotsMeta + "; HTTP: " + p.headerRobots)
			if strings.Contains(strings.ToLower(robotsMeta+","+p.headerRobots), "noindex") {
				f.Status = "partial"
				f.Evidence += "; confirm whether exclusion is intentional"
			}
		case "seo.title":
			f.Evidence = excerpt(title)
			if title == "" {
				f.Status = "fail"
				f.Evidence = "No title text found"
			}
		case "seo.description":
			f.Evidence = excerpt(description)
			if description == "" {
				f.Status = "fail"
				f.Evidence = "No meta description found"
			}
		case "seo.canonical":
			f.Evidence = excerpt(canonical)
			u, e := url.Parse(canonical)
			if canonCount != 1 || e != nil || !u.IsAbs() || validateURL(u) != nil {
				f.Status = "partial"
				f.Evidence = "Expected one absolute HTTP(S) canonical reference; observed " + fmt.Sprint(canonCount)
			}
		case "seo.images":
			f.Evidence = fmt.Sprintf("%d of %d images have an alt attribute; semantic accuracy not verified", images-missingAlt, images)
			if images == 0 {
				f.Status = "not_applicable"
			} else if missingAlt > 0 {
				f.Status = "partial"
			}
		case "geo.entity":
			f.Evidence = "Schema.org entity data observed; factual accuracy not verified"
			if !entity {
				f.Status = "partial"
				f.Evidence = "No recognized structured entity found"
			}
		case "geo.structure":
			f.Evidence = fmt.Sprintf("Meaningful headings: %t; semantic information structures: %t", heading, structured)
			if !heading {
				f.Status = "fail"
			} else if !structured {
				f.Status = "partial"
			}
		case "geo.author":
			f.Evidence = fmt.Sprintf("Article detected: %t; author metadata: %t; machine-readable date: %t", article, author, dated)
			if !article {
				f.Status = "not_applicable"
			} else if !author || !dated {
				f.Status = "partial"
			}
		case "agent.access":
			f.Evidence = fmt.Sprintf("%d initial-HTML words observed; visual rendering and hidden-text accuracy not verified", len(strings.Fields(visible.String())))
			if !meaningful {
				f.Status = "fail"
			}
		case "agent.landmarks":
			f.Evidence = fmt.Sprintf("Main landmark: %t; navigation landmark: %t", main, nav)
			if !main || !nav {
				f.Status = "partial"
			}
		case "agent.controls":
			f.Evidence = fmt.Sprintf("%d of %d controls have observed names; interactions not executed", controls-unnamed, controls)
			if controls == 0 {
				f.Status = "not_applicable"
			} else if unnamed > 0 {
				f.Status = "partial"
			}
		case "agent.links":
			f.Evidence = fmt.Sprintf("%d of %d links have observed names", links-emptyLinks, links)
			if links == 0 {
				f.Status = "not_applicable"
			} else if emptyLinks > 0 {
				f.Status = "partial"
			}
		}
		out = append(out, f)
	}
	return out
}
func hasType(v any, kind string) bool {
	switch x := v.(type) {
	case map[string]any:
		if t, ok := x["@type"].(string); ok && (t == kind || strings.HasSuffix(t, "/"+kind)) {
			return true
		}
		if t, ok := x["@type"].([]any); ok {
			for _, a := range t {
				if a == kind {
					return true
				}
			}
		}
		if g, ok := x["@graph"]; ok {
			return hasType(g, kind)
		}
	case []any:
		for _, a := range x {
			if hasType(a, kind) {
				return true
			}
		}
	}
	return false
}
func hasEntity(v any) bool {
	for _, kind := range []string{"Organization", "LocalBusiness", "Product", "SoftwareApplication", "Article", "Person", "WebSite", "FAQPage"} {
		if hasType(v, kind) {
			return true
		}
	}
	return false
}
func score(findings []reportmodel.Finding) []reportmodel.Pillar {
	var pillars []reportmodel.Pillar
	for _, p := range []reportmodel.Pillar{{ID: "seo", Name: "SEO"}, {ID: "geo", Name: "GEO"}, {ID: "agent", Name: "AI-agent readiness (AEO)"}} {
		total, observed, earned := 0.0, 0.0, 0.0
		foundationUnknown := false
		for _, f := range findings {
			if f.Pillar != p.ID || f.Weight <= 0 || f.Status == "not_applicable" {
				continue
			}
			total += f.Weight
			if f.Status == "unknown" {
				foundationUnknown = foundationUnknown || f.Foundational
				continue
			}
			observed += f.Weight
			switch f.Status {
			case "pass":
				earned += f.Weight
			case "partial":
				earned += f.Weight * .5
			}
		}
		if total > 0 {
			p.Coverage = observed / total
		}
		if observed > 0 && p.Coverage >= .8 && !foundationUnknown {
			n := math.Round(1000*earned/observed) / 10
			p.Score = &n
		}
		pillars = append(pillars, p)
	}
	return pillars
}
