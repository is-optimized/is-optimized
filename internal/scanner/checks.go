package scanner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
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
	htmlBytes    int
	headerRobots string
	hsts         string
	headers      http.Header
	ttfb         time.Duration
	ttfbObserved bool
	tlsNotAfter  time.Time
	tlsVerified  bool
}

func analyze(r response, err error, requested string, duration time.Duration) pageResult {
	p := pageResult{page: reportmodel.Page{URL: requested, RequestedURL: requested, StatusCode: r.code, RedirectCount: r.redirects, DurationMS: duration.Milliseconds()}, htmlBytes: len(r.body), headers: r.header.Clone(), ttfb: r.ttfb, ttfbObserved: r.ttfbObserved, tlsNotAfter: r.tlsNotAfter, tlsVerified: r.tlsVerified}
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
	p.hsts = r.header.Get("Strict-Transport-Security")
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
		if len(links) >= maxSitemapCandidates || n.Type != html.ElementNode || n.Data != "a" || hasAttr(n, "download") {
			return
		}
		raw := strings.TrimSpace(attr(n, "href"))
		if raw == "" {
			return
		}
		u, ok := candidateURL(raw, base)
		if ok && !seen[u.String()] {
			seen[u.String()] = true
			links = append(links, u.String())
		}
	})
	return links
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return true
		}
	}
	return false
}

func candidateURL(raw string, base *url.URL) (*url.URL, bool) {
	u, err := base.Parse(strings.TrimSpace(raw))
	if err != nil || validateURL(u) != nil || origin(u) != origin(base) || u.RawQuery != "" || u.User != nil {
		return nil, false
	}
	u.Fragment = ""
	u.RawFragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	if strings.Contains(u.Path, "//") {
		return nil, false
	}
	ext := strings.ToLower(path.Ext(u.Path))
	if ext != "" && ext != ".html" && ext != ".htm" && ext != ".php" && ext != ".asp" && ext != ".aspx" {
		return nil, false
	}
	lower := strings.ToLower(u.Path)
	if lower == "/cdn-cgi" || strings.HasPrefix(lower, "/cdn-cgi/") {
		return nil, false
	}
	for _, unsafe := range []string{"logout", "log-out", "signout", "sign-out", "delete", "remove", "unsubscribe", "checkout", "cart", "purchase", "buy-now", "/admin", "/login", "/signin"} {
		if strings.Contains(lower, unsafe) {
			return nil, false
		}
	}
	return u, true
}

func selectRepresentative(candidates []string, base *url.URL, limit int) []string {
	if limit <= 0 {
		return nil
	}
	groups := make(map[string][]string)
	var order []string
	seen := map[string]bool{base.String(): true}
	for _, raw := range candidates {
		u, ok := candidateURL(raw, base)
		if !ok || seen[u.String()] {
			continue
		}
		seen[u.String()] = true
		family := pageFamily(u.Path)
		if _, ok := groups[family]; !ok {
			order = append(order, family)
		}
		groups[family] = append(groups[family], u.String())
	}
	selected := make([]string, 0, limit)
	for offset := 0; len(selected) < limit; offset++ {
		added := false
		for _, family := range order {
			if offset < len(groups[family]) {
				selected = append(selected, groups[family][offset])
				added = true
				if len(selected) == limit {
					break
				}
			}
		}
		if !added {
			break
		}
	}
	return selected
}

func pageFamily(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "/"
	}
	return strings.ToLower(parts[0])
}

type Check struct {
	ID, Pillar, Title, Remediation string
	Weight                         float64
	Foundational                   bool
	Tier                           string
}

// Registry is the per-page weighted check set. Site-level weighted checks live
// in site_checks.go. Changes require a new MethodologyVersion.
func Registry() []Check {
	return []Check{
		{ID: "seo.access", Pillar: "seo", Title: "Public HTML response", Remediation: "Ensure intended public pages return useful HTML with a successful HTTP status.", Weight: 3, Foundational: true, Tier: "essential"},
		{ID: "seo.index", Pillar: "seo", Title: "Index directives", Remediation: "Review robots directives against your intended indexing policy; remove unintended noindex directives.", Weight: 2, Tier: "recommended"},
		{ID: "seo.title", Pillar: "seo", Title: "Page title", Remediation: "Give this page a concise, descriptive title that distinguishes its purpose.", Weight: 2, Tier: "recommended"},
		{ID: "seo.description", Pillar: "seo", Title: "Search description", Remediation: "Write an accurate page-specific meta description; do not promise that search engines will display it.", Weight: 1, Tier: "recommended"},
		{ID: "seo.canonical", Pillar: "seo", Title: "Canonical reference", Remediation: "Provide one valid canonical URL that reflects the intended public version of this page.", Weight: 1, Tier: "recommended"},
		{ID: "seo.redirect_hygiene", Pillar: "seo", Title: "Redirect and refresh hygiene", Remediation: "Avoid refresh/script redirect stubs on short content pages, redirect chains longer than two hops, and HTTPS downgrades.", Weight: 1, Tier: "recommended"},
		{ID: "seo.headings", Pillar: "seo", Title: "Heading structure", Remediation: "Provide a meaningful H1 and use heading levels in a logical sequence.", Weight: 1, Tier: "recommended"},
		{ID: "seo.viewport", Pillar: "seo", Title: "Mobile viewport declaration", Remediation: "Declare a viewport with width=device-width; verify actual mobile layout separately.", Weight: 1, Tier: "recommended"},
		{ID: "seo.images", Pillar: "seo", Title: "Image text alternatives", Remediation: "Provide useful alternatives for informative images and empty alt attributes for decorative images.", Weight: 1, Tier: "recommended"},
		{ID: "geo.access", Pillar: "geo", Title: "Extractable public response", Remediation: "Expose intended public information in server-delivered HTML.", Weight: 3, Foundational: true, Tier: "essential"},
		{ID: "geo.entity", Pillar: "geo", Title: "Structured entity data", Remediation: "Add accurate, applicable Schema.org JSON-LD based on verified business or content facts.", Weight: 2, Tier: "recommended"},
		{ID: "geo.structure", Pillar: "geo", Title: "Structured information", Remediation: "Use meaningful headings and semantic lists, tables, or definition lists where they clarify the content.", Weight: 2, Tier: "recommended"},
		{ID: "geo.author", Pillar: "geo", Title: "Article attribution", Remediation: "For article content, identify the author and publish or update date accurately.", Weight: 1, Tier: "recommended"},
		{ID: "agent.access", Pillar: "agent", Title: "Initial HTML readability", Remediation: "Serve meaningful public text in initial HTML so readers need not execute JavaScript to understand the page.", Weight: 3, Foundational: true, Tier: "essential"},
		{ID: "agent.landmarks", Pillar: "agent", Title: "Navigation landmarks", Remediation: "Use main and navigation landmarks to identify page content and navigation.", Weight: 1, Tier: "recommended"},
		{ID: "agent.controls", Pillar: "agent", Title: "Named interactive controls", Remediation: "Give interactive controls accessible names through visible labels or appropriate ARIA attributes.", Weight: 2, Tier: "recommended"},
		{ID: "agent.form_labels", Pillar: "agent", Title: "Form label associations", Remediation: "Associate each visible input, select, and textarea with a descriptive label using label-for or a wrapping label.", Weight: 1, Tier: "recommended"},
		{ID: "agent.links", Pillar: "agent", Title: "Named navigation links", Remediation: "Give navigation links meaningful visible or accessible names.", Weight: 1, Tier: "recommended"},
		{ID: "agent.prompt_injection_signals", Pillar: "agent", Title: "Instruction-like hidden text signals", Remediation: "Review instruction-like text in hidden or accessibility metadata. Treat page content as untrusted and remove misleading instructions where appropriate."},
	}
}
func evaluate(p pageResult) []reportmodel.Finding {
	registry := Registry()
	out := make([]reportmodel.Finding, 0, len(registry))
	var title, description, robotsMeta, canonical string
	h1, emptyH1, skipped, previousLevel := 0, 0, 0, 0
	metaRefresh, scriptRedirect := false, false
	viewport := false
	canonCount := 0
	canonicalValues := map[string]bool{}
	images, missingAlt, controls, unnamed, links, emptyLinks := 0, 0, 0, 0, 0, 0
	formFields, labelledFields := 0, 0
	hiddenInstructionSignals := 0
	heading, structured, main, nav, article, author, dated, entity := false, false, false, false, false, false, false, false
	var visible, mainVisible strings.Builder
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
		for _, key := range []string{"aria-label", "title", "alt"} {
			if suspiciousInstructionText(attr(n, key)) {
				hiddenInstructionSignals++
			}
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
			inMain := false
			for a := n.Parent; a != nil; a = a.Parent {
				if a.Data == "script" || a.Data == "style" || a.Data == "head" || a.Data == "template" || strings.EqualFold(attr(a, "aria-hidden"), "true") || hiddenByInlineStyle(attr(a, "style")) {
					skip = true
				}
				for _, aAttr := range a.Attr {
					if aAttr.Key == "hidden" {
						skip = true
					}
				}
				if a.Data == "main" {
					inMain = true
				}
			}
			if !skip {
				visible.WriteString(n.Data)
				visible.WriteByte(' ')
				if inMain {
					mainVisible.WriteString(n.Data)
					mainVisible.WriteByte(' ')
				}
			} else if suspiciousInstructionText(n.Data) {
				hiddenInstructionSignals++
			}
		}
		if n.Type != html.ElementNode {
			return
		}
		switch n.Data {
		case "title":
			title = text(n)
		case "meta":
			if strings.EqualFold(strings.TrimSpace(attr(n, "http-equiv")), "refresh") {
				metaRefresh = true
			}
			if strings.EqualFold(attr(n, "property"), "og:type") && strings.EqualFold(strings.TrimSpace(attr(n, "content")), "article") {
				article = true
			}
			switch strings.ToLower(attr(n, "name")) {
			case "viewport":
				for _, part := range strings.FieldsFunc(strings.ToLower(attr(n, "content")), func(r rune) bool { return r == ',' || r == ';' }) {
					pair := strings.SplitN(part, "=", 2)
					if len(pair) == 2 && strings.TrimSpace(pair[0]) == "width" && strings.TrimSpace(pair[1]) == "device-width" {
						viewport = true
					}
				}
			case "description":
				description = strings.TrimSpace(attr(n, "content"))
			case "robots":
				robotsMeta = attr(n, "content")
			case "author":
				author = attr(n, "content") != ""
			}
		case "link":
			if hasToken(attr(n, "rel"), "canonical") {
				canonical = attr(n, "href")
				canonCount++
				canonicalValues[canonical] = true
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
			level := int(n.Data[1] - '0')
			if level == 1 {
				h1++
				if text(n) == "" {
					emptyH1++
				}
			}
			if previousLevel > 0 && level > previousLevel+1 {
				skipped++
			}
			previousLevel = level
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
			if isLabelableControl(n) {
				formFields++
				if labels[attr(n, "id")] != "" || wrappedInLabel(n) {
					labelledFields++
				}
			}
			controls++
			if !accessibleName(n) {
				unnamed++
			}
		case "script":
			script := strings.ToLower(text(n))
			if strings.Contains(script, "location.href") || strings.Contains(script, "location.replace") {
				scriptRedirect = true
			}
			if strings.EqualFold(attr(n, "type"), "application/ld+json") {
				var value any
				if json.Unmarshal([]byte(text(n)), &value) == nil {
					entity = entity || hasEntity(value)
					article = article || isArticleSchema(value)
				}
			}
		}
	})
	meaningful := len(strings.Fields(visible.String())) >= 20
	for _, c := range registry {
		f := reportmodel.Finding{ID: c.ID, Pillar: c.Pillar, Title: c.Title, URL: p.page.URL, Weight: c.Weight, Tier: c.Tier, Foundational: c.Foundational, Remediation: c.Remediation, Status: "pass"}
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
				f.Evidence = fmt.Sprintf("Expected one absolute public HTTP(S) canonical; observed %d declarations, %d distinct targets. Last target: %s", canonCount, len(canonicalValues), excerpt(canonical))
				if len(canonicalValues) > 1 {
					f.Evidence = "Conflicting HTML canonical targets. " + f.Evidence
				}
				f.Evidence = excerpt(f.Evidence)
			} else if canonical != p.page.URL {
				f.Evidence += "; differs from fetched URL; may be intentional, target not fetched"
			}
		case "seo.redirect_hygiene":
			words := len(strings.Fields(visible.String()))
			issues := []string{}
			if metaRefresh {
				issues = append(issues, "meta refresh observed")
			}
			if scriptRedirect && words < 200 {
				issues = append(issues, "location redirect script on a page under 200 words")
			}
			if p.page.RedirectCount > 2 {
				issues = append(issues, fmt.Sprintf("%d redirect hops", p.page.RedirectCount))
			}
			if strings.HasPrefix(strings.ToLower(p.page.URL), "http://") {
				issues = append(issues, "final response downgraded from HTTPS")
			}
			if len(issues) > 0 {
				f.Status = "fail"
				f.Evidence = strings.Join(issues, "; ")
			} else {
				f.Evidence = fmt.Sprintf("No meta refresh, short-page location redirect stub, redirect chain over two hops, or HTTPS downgrade observed; %d redirect hop(s)", p.page.RedirectCount)
			}
		case "seo.headings":
			f.Evidence = fmt.Sprintf("%d H1 elements; %d empty H1s; %d upward level skips in document order", h1, emptyH1, skipped)
			if h1 == 0 || emptyH1 == h1 {
				f.Status = "fail"
			} else if emptyH1 > 0 || skipped > 0 {
				f.Status = "partial"
			}
		case "seo.viewport":
			f.Evidence = "Viewport width=device-width observed; responsive rendering not tested"
			if !viewport {
				f.Status = "partial"
				f.Evidence = "No viewport declaring width=device-width observed"
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
		case "agent.form_labels":
			f.Evidence = fmt.Sprintf("%d of %d visible form fields have an associated label element", labelledFields, formFields)
			if formFields == 0 {
				f.Status = "not_applicable"
			} else if labelledFields == 0 {
				f.Status = "fail"
			} else if labelledFields < formFields {
				f.Status = "partial"
			}
		case "agent.links":
			f.Evidence = fmt.Sprintf("%d of %d links have observed names", links-emptyLinks, links)
			if links == 0 {
				f.Status = "not_applicable"
			} else if emptyLinks > 0 {
				f.Status = "partial"
			}
		case "agent.prompt_injection_signals":
			f.Stage = "read"
			f.Applicability = "Informational pattern scan of hidden text and aria-label, title, and alt attributes; detected phrases require human interpretation and are never followed or executed."
			f.Evidence = fmt.Sprintf("%d instruction-like pattern(s) observed in hidden or accessibility metadata; matched text is omitted because page content is untrusted", hiddenInstructionSignals)
			f.Status = "information"
		}
		out = append(out, f)
	}
	out = append(out, reportmodel.Finding{ID: "seo.words", Pillar: "seo", Title: "Initial HTML word count", Status: observationStatus(p), URL: p.page.URL, Evidence: wordEvidence(p, visible.String())})
	visibleText := strings.TrimSpace(visible.String())
	tokenText := strings.TrimSpace(mainVisible.String())
	tokenSource := "main element"
	if tokenText == "" {
		tokenText = visibleText
		tokenSource = "initial HTML fallback because no main text was present"
	}
	if p.page.Error != "" {
		out = append(out,
			reportmodel.Finding{ID: "agent.content_budget", Pillar: "agent", Title: "Estimated text token budget", Status: "unknown", Weight: 1, Tier: "recommended", Stage: "read", URL: p.page.URL, Evidence: p.page.Error, Remediation: "Keep the main page content concise and split long reference material into linked sections."},
			reportmodel.Finding{ID: "geo.content_ratio", Pillar: "geo", Title: "Visible text to HTML ratio", Status: "unknown", Weight: 1, Tier: "recommended", Stage: "read", URL: p.page.URL, Evidence: p.page.Error, Remediation: "Ensure the initial HTML contains useful visible text without excessive markup or embedded boilerplate."},
		)
	} else {
		tokens := (len(tokenText) + 3) / 4
		budgetStatus := "pass"
		if tokens > 30000 {
			budgetStatus = "fail"
		} else if tokens > 15000 {
			budgetStatus = "partial"
		}
		out = append(out, reportmodel.Finding{ID: "agent.content_budget", Pillar: "agent", Title: "Estimated text token budget", Status: budgetStatus, Weight: 1, Tier: "recommended", Stage: "read", URL: p.page.URL, Evidence: fmt.Sprintf("Approximately %d tokens estimated from %s characters divided by four; this is a rough tokenizer-independent estimate", tokens, tokenSource), Remediation: "Keep the main page content concise and split long reference material into linked sections."})
		ratio := 0.0
		if p.htmlBytes > 0 {
			ratio = float64(len(visibleText)) / float64(p.htmlBytes)
		}
		ratioStatus := "pass"
		if ratio < .05 {
			ratioStatus = "partial"
		}
		out = append(out, reportmodel.Finding{ID: "geo.content_ratio", Pillar: "geo", Title: "Visible text to HTML ratio", Status: ratioStatus, Weight: 1, Tier: "recommended", Stage: "read", URL: p.page.URL, Evidence: fmt.Sprintf("Visible initial-HTML text is %.1f%% of response bytes; this heuristic does not measure rendered or hidden content", ratio*100), Remediation: "Ensure the initial HTML contains useful visible text without excessive markup or embedded boilerplate."})
	}
	evidence := "HSTS header absent on HTTPS response; security observation, no score penalty"
	if p.hsts != "" {
		evidence = "HSTS header observed (policy not validated): " + excerpt(p.hsts)
	}
	if u, _ := url.Parse(p.page.URL); u == nil || u.Scheme != "https" {
		evidence = "HSTS not assessed on a non-HTTPS URL"
	}
	if p.page.Error != "" {
		evidence = p.page.Error
	}
	out = append(out, reportmodel.Finding{ID: "seo.hsts", Pillar: "seo", Title: "HTTPS transport policy", Status: observationStatus(p), URL: p.page.URL, Evidence: evidence})
	out = append(out, readinessObservations(p)...)
	out = append(out, pageParityChecks(p, p.headers)...)
	annotateReadiness(out)
	return out
}

func isLabelableControl(n *html.Node) bool {
	if n.Data == "select" || n.Data == "textarea" {
		return true
	}
	if n.Data != "input" {
		return false
	}
	switch strings.ToLower(attr(n, "type")) {
	case "button", "submit", "reset", "hidden", "image":
		return false
	default:
		return true
	}
}

func hiddenByInlineStyle(style string) bool {
	style = strings.ToLower(strings.Join(strings.Fields(style), ""))
	if strings.Contains(style, "display:none") || strings.Contains(style, "visibility:hidden") || strings.Contains(style, "opacity:0") {
		return true
	}
	return strings.Contains(style, "position:absolute") && (strings.Contains(style, "left:-9999px") || strings.Contains(style, "left:-10000px") || strings.Contains(style, "top:-9999px") || strings.Contains(style, "top:-10000px"))
}

func wrappedInLabel(n *html.Node) bool {
	for parent := n.Parent; parent != nil; parent = parent.Parent {
		if parent.Type == html.ElementNode && parent.Data == "label" && strings.TrimSpace(text(parent)) != "" {
			return true
		}
	}
	return false
}

func suspiciousInstructionText(value string) bool {
	value = strings.ToLower(strings.Join(strings.Fields(value), " "))
	for _, phrase := range []string{"ignore previous", "ignore all previous", "ai assistant", "you are", "system prompt"} {
		if strings.Contains(value, phrase) {
			return true
		}
	}
	return false
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
func isArticleSchema(value any) bool {
	for _, kind := range []string{"Article", "BlogPosting", "NewsArticle"} {
		if hasType(value, kind) {
			return true
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
	readiness, _ := scoreAgentReadiness(findings)
	pillars = append(pillars, readiness)
	return pillars
}

func scoreAgentReadiness(findings []reportmodel.Finding) (reportmodel.Pillar, reportmodel.AgentReadinessSummary) {
	type accumulator struct {
		total, observed, earned float64
		passing, count          int
	}
	byTier := map[string]*accumulator{
		"essential": {}, "recommended": {}, "bonus": {},
	}
	for _, f := range findings {
		bucket := byTier[f.Tier]
		if bucket == nil || f.Weight <= 0 || f.Status == "not_applicable" {
			continue
		}
		bucket.total += f.Weight
		bucket.count++
		if f.Status == "unknown" {
			continue
		}
		bucket.observed += f.Weight
		if f.Status == "pass" {
			bucket.earned += f.Weight
			bucket.passing++
		} else if f.Status == "partial" {
			bucket.earned += f.Weight * .5
		}
	}
	ess, rec := byTier["essential"], byTier["recommended"]
	applicable, observed := ess.total+rec.total, ess.observed+rec.observed
	summary := reportmodel.AgentReadinessSummary{}
	if applicable > 0 {
		summary.Coverage = observed / applicable
	}
	summary.Essential = reportmodel.ReadinessTierSummary{Available: 80, Passing: ess.passing, Total: ess.count}
	summary.Recommended = reportmodel.ReadinessTierSummary{Available: 20, Passing: rec.passing, Total: rec.count}
	if ess.observed > 0 {
		summary.Essential.Earned = 80 * ess.earned / ess.observed
	}
	if rec.observed > 0 {
		summary.Recommended.Earned = 20 * rec.earned / rec.observed
	}
	for _, f := range findings {
		if f.Tier != "bonus" || f.Weight <= 0 || f.Status == "unknown" || f.Status == "not_applicable" {
			continue
		}
		if f.Status == "pass" {
			summary.BonusPoints += f.Weight
			summary.PositiveSignals++
		} else if f.Status == "partial" {
			summary.BonusPoints += f.Weight * .5
			summary.PositiveSignals++
		}
	}
	if summary.BonusPoints > 5 {
		summary.BonusPoints = 5
	}
	var value *float64
	if applicable > 0 && summary.Coverage >= .8 && ess.observed > 0 && rec.observed > 0 {
		n := math.Round(math.Min(100, summary.Essential.Earned+summary.Recommended.Earned+summary.BonusPoints)*10) / 10
		value = &n
		summary.ScoreLabel = readinessLabel(n)
	}
	return reportmodel.Pillar{ID: "agent_readiness", Name: "Agent readiness", Score: value, Coverage: summary.Coverage}, summary
}

func readinessLabel(score float64) string {
	switch {
	case score >= 90:
		return "Strong technical baseline"
	case score >= 70:
		return "Several improvements available"
	case score >= 50:
		return "Important blockers remain"
	default:
		return "Agents are likely to struggle"
	}
}
