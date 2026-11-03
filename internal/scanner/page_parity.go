package scanner

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"golang.org/x/net/html"
	"golang.org/x/text/language"
)

// pageParityChecks uses only already-fetched HTML and final-response headers.
// It performs no additional requests and never executes page instructions.
func pageParityChecks(p pageResult, headers http.Header) []reportmodel.Finding {
	const htmlSpec = "https://html.spec.whatwg.org/multipage/"
	out := []reportmodel.Finding{
		pageParityFinding(p, "seo.hreflang", "seo", "Declared language alternatives", `Declare valid BCP 47 tags and public alternate URLs: <link rel="alternate" hreflang="en" href="https://example.com/en/">. Verify reciprocal declarations on translated pages.`, htmlSpec+"links.html#attr-hyperlink-hreflang", 1),
		pageParityFinding(p, "seo.mixed_content", "seo", "Insecure embedded resources", `Use HTTPS resource URLs: <script src="https://example.com/app.js" defer></script>.`, "https://www.w3.org/TR/mixed-content/", 1),
		pageParityFinding(p, "seo.internal_nofollow", "seo", "Internal nofollow links", `Review intentional exclusions; normal navigation can use <a href="/docs">Documentation</a> without rel="nofollow".`, htmlSpec+"links.html#link-type-nofollow", 0),
		pageParityFinding(p, "seo.image_dimensions", "seo", "Declared image dimensions", `Declare intrinsic dimensions: <img src="/photo.webp" width="800" height="600" alt="Description">.`, htmlSpec+"embedded-content.html#attr-dim-width", 1),
		pageParityFinding(p, "seo.image_loading", "seo", "Image loading hints", `Use loading="lazy" for below-the-fold images and keep primary visible images eager; verify placement in a browser.`, htmlSpec+"embedded-content.html#attr-img-loading", 0),
		pageParityFinding(p, "seo.structured_validity", "seo", "JSON-LD syntax and context", `Publish valid JSON-LD: <script type="application/ld+json">{"@context":"https://schema.org","@type":"WebSite","name":"Example","url":"https://example.com/"}</script>.`, "https://www.w3.org/TR/json-ld11/", 1),
		pageParityFinding(p, "agent.native_controls", "agent", "Native interactive controls", `Prefer <button type="button">Action</button> over <div onclick="...">Action</div>; native semantics supply keyboard behavior.`, htmlSpec+"interaction.html", 1),
		pageParityFinding(p, "geo.content_signals", "geo", "Answer-oriented content signals", `Where useful, add visible Q&A, sourced facts, comparison tables, and truthful <time datetime="2026-01-01">Updated January 1, 2026</time> markup.`, "", 0),
		pageParityFinding(p, "geo.brand_consistency", "geo", "Declared brand consistency", `Use the verified brand consistently in title, <meta property="og:site_name" content="Example"> and Organization JSON-LD name; review legal/trading-name variations.`, "https://schema.org/Organization", 1),
		pageParityFinding(p, "seo.render_blocking", "seo", "Potential render-blocking resources", `Use defer or type="module" for scripts that can wait; keep critical styles small and verify changes with browser measurements.`, htmlSpec+"scripting.html#attr-script-defer", 0),
	}
	if p.doc == nil || p.page.Error != "" {
		for i := range out {
			out[i].Evidence = "Page HTML not observed: " + p.page.Error
			out[i].EvidenceLevel = ""
		}
		return append(out, pageHeaderChecks(p, headers)...)
	}
	base, _ := url.Parse(p.page.URL)
	resolutionBase := base
	baseSeen := false
	walk(p.doc, func(n *html.Node) {
		if baseSeen || n.Type != html.ElementNode || n.Data != "base" || !hasAttr(n, "href") {
			return
		}
		baseSeen = true
		if base != nil {
			if target, err := base.Parse(strings.TrimSpace(attr(n, "href"))); err == nil && (target.Scheme == "http" || target.Scheme == "https") {
				resolutionBase = target
			}
		}
	})
	langs, validLangs, conflicts := 0, 0, 0
	langTargets := map[string]string{}
	mixed, internal, nofollow, images, dimensions, lazy := 0, 0, 0, 0, 0, 0
	native, scripted, jsonBlocks, validJSON, contexts := 0, 0, 0, 0, 0
	questions, details, tables, times, citations := 0, 0, 0, 0, 0
	blockingScripts, blockingStyles := 0, 0
	title, siteName := "", ""
	var brands []string
	walk(p.doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		switch n.Data {
		case "title":
			title = text(n)
		case "meta":
			if strings.EqualFold(attr(n, "property"), "og:site_name") {
				siteName = strings.TrimSpace(attr(n, "content"))
			}
		case "link":
			if hasToken(attr(n, "rel"), "alternate") && hasAttr(n, "hreflang") {
				langs++
				tag := strings.TrimSpace(attr(n, "hreflang"))
				target, ok := readinessLink(attr(n, "href"), resolutionBase)
				_, err := language.Parse(tag)
				if tag != "" && !strings.ContainsAny(tag, "_ \t\r\n") && (strings.EqualFold(tag, "x-default") || err == nil) && ok {
					validLangs++
					key := strings.ToLower(tag)
					if old, exists := langTargets[key]; exists && old != target.String() {
						conflicts++
					}
					langTargets[key] = target.String()
				}
			}
		case "a":
			if strings.TrimSpace(attr(n, "href")) != "" {
				native++
			}
			if target, ok := readinessLink(attr(n, "href"), resolutionBase); ok {
				if origin(target) == origin(base) {
					internal++
					if hasToken(attr(n, "rel"), "nofollow") {
						nofollow++
					}
				} else {
					citations++
				}
			}
		case "img":
			images++
			w, we := strconv.Atoi(strings.TrimSpace(attr(n, "width")))
			h, he := strconv.Atoi(strings.TrimSpace(attr(n, "height")))
			if we == nil && he == nil && w > 0 && h > 0 {
				dimensions++
			}
			if strings.EqualFold(attr(n, "loading"), "lazy") {
				lazy++
			}
		case "button", "input", "select", "textarea":
			if n.Data != "input" || !strings.EqualFold(attr(n, "type"), "hidden") {
				native++
			}
		case "div", "span":
			if hasAttr(n, "onclick") {
				scripted++
			}
		case "table":
			tables++
		case "time":
			if strings.TrimSpace(attr(n, "datetime")) != "" {
				times++
			}
		case "h2", "h3", "summary":
			if strings.HasSuffix(strings.TrimSpace(text(n)), "?") {
				questions++
			}
		case "details":
			details++
		case "script":
			if attr(n, "src") != "" && !hasAttr(n, "async") && !hasAttr(n, "defer") && !strings.EqualFold(attr(n, "type"), "module") && pageNodeInHead(n) {
				blockingScripts++
			}
			if strings.EqualFold(strings.TrimSpace(attr(n, "type")), "application/ld+json") {
				jsonBlocks++
				var value any
				if json.Unmarshal([]byte(text(n)), &value) == nil {
					valid, context := pageJSONLDShape(value)
					if valid {
						validJSON++
					}
					if context {
						contexts++
					}
					var entities []map[string]any
					walkJSONLD(value, &entities, map[string]bool{})
					for _, entity := range entities {
						kinds := schemaTypes(entity)
						if kinds["organization"] || kinds["localbusiness"] {
							if name := schemaString(entity["name"]); name != "" {
								brands = append(brands, name)
							}
						}
					}
				}
			}
		}
		if n.Data == "link" && hasToken(attr(n, "rel"), "stylesheet") && pageNodeInHead(n) {
			media := strings.TrimSpace(strings.ToLower(attr(n, "media")))
			if media == "" || media == "all" || media == "screen" {
				blockingStyles++
			}
		}
		if base != nil && base.Scheme == "https" {
			resourceAttr := ""
			switch n.Data {
			case "script", "img", "iframe", "audio", "video", "source", "embed":
				resourceAttr = "src"
			case "object":
				resourceAttr = "data"
			case "link":
				if hasToken(attr(n, "rel"), "stylesheet") || hasToken(attr(n, "rel"), "preload") || hasToken(attr(n, "rel"), "modulepreload") {
					resourceAttr = "href"
				}
			}
			if resourceAttr != "" {
				if u, e := resolutionBase.Parse(strings.TrimSpace(attr(n, resourceAttr))); e == nil && u.Scheme == "http" {
					mixed++
				}
			}
			if n.Data == "img" || n.Data == "source" {
				for _, part := range strings.Split(attr(n, "srcset"), ",") {
					fields := strings.Fields(part)
					if len(fields) > 0 {
						if u, e := resolutionBase.Parse(fields[0]); e == nil && u.Scheme == "http" {
							mixed++
						}
					}
				}
			}
		}
	})
	out[0].Status = pageParityProportion(validLangs, langs)
	if conflicts > 0 {
		out[0].Status = "partial"
	}
	out[0].Evidence = fmt.Sprintf("%d of %d language alternatives have parseable tags and public URLs; %d conflicting language targets. Reciprocity and destination content not fetched.", validLangs, langs, conflicts)
	out[1].Status = "pass"
	if base == nil || base.Scheme != "https" {
		out[1].Status = "not_applicable"
	}
	if mixed > 0 {
		out[1].Status = "fail"
	}
	out[1].Evidence = fmt.Sprintf("%d explicit HTTP resource references in HTTPS initial HTML; CSS/runtime requests and browser upgrades not evaluated.", mixed)
	out[2].Status = "information"
	out[2].Evidence = fmt.Sprintf("%d of %d same-origin navigation links declare nofollow; exclusions may be intentional.", nofollow, internal)
	out[3].Status = pageParityProportion(dimensions, images)
	out[3].Evidence = fmt.Sprintf("%d of %d img elements declare positive integer width and height; CSS aspect ratios and rendered layout not evaluated.", dimensions, images)
	out[4].Status = "information"
	if images == 0 {
		out[4].Status = "not_applicable"
	}
	out[4].Evidence = fmt.Sprintf("%d lazy images; %d eager or unspecified images. Above/below-fold placement cannot be determined from HTML.", lazy, images-lazy)
	out[5].Status = pageParityProportion(validJSON, jsonBlocks)
	if jsonBlocks > 0 && validJSON == jsonBlocks && contexts < jsonBlocks {
		out[5].Status = "partial"
	}
	out[5].Evidence = fmt.Sprintf("%d of %d JSON-LD blocks parse as non-empty objects/arrays; %d declare contexts. No remote context resolution or property certification.", validJSON, jsonBlocks, contexts)
	out[6].Status = pageParityProportion(native, native+scripted)
	out[6].Evidence = fmt.Sprintf("%d native links/form controls; %d div/span onclick controls. Keyboard behavior and runtime listeners not tested.", native, scripted)
	out[7].Status = "information"
	out[7].Evidence = fmt.Sprintf("%d question-shaped headings/summaries, %d details sections, %d tables, %d machine-readable time elements, %d external links. Structural signals do not establish answer quality, factual accuracy or citation relevance.", questions, details, tables, times, citations)
	out[8].Status = "not_applicable"
	compared, matches := 0, 0
	normalize := func(v string) string { return strings.ToLower(strings.Join(strings.Fields(v), " ")) }
	for _, brand := range brands {
		for _, label := range []string{title, siteName} {
			if label != "" {
				compared++
				if strings.Contains(normalize(label), normalize(brand)) {
					matches++
				}
			}
		}
	}
	if compared > 0 {
		out[8].Status = "pass"
		if matches < compared {
			out[8].Status = "partial"
		}
	}
	out[8].Evidence = fmt.Sprintf("%d of %d title/social-name comparisons contain a declared Organization name (case/space normalized). Legal/trading names need review; identity not independently verified.", matches, compared)
	out[9].Status = "information"
	out[9].Evidence = fmt.Sprintf("Initial head declares %d external classic scripts without async/defer and %d screen/all stylesheets. These are potential blockers; browser execution, cache and critical-path timing not measured.", blockingScripts, blockingStyles)
	return append(out, pageHeaderChecks(p, headers)...)
}

func pageNodeInHead(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && p.Data == "head" {
			return true
		}
	}
	return false
}

func pageParityFinding(p pageResult, id, pillar, title, fix, spec string, weight float64) reportmodel.Finding {
	f := reportmodel.Finding{ID: id, Pillar: pillar, Title: title, Status: "unknown", URL: p.page.URL, Weight: weight, Stage: "read", EvidenceLevel: "observed_html", Applicability: "Assesses fetched initial HTML; does not execute scripts or infer rendered behavior.", Remediation: fix}
	if weight > 0 {
		f.Tier = "recommended"
	}
	if spec != "" {
		f.Standards = []reportmodel.StandardReference{{ID: title, URL: spec, Coverage: "partial"}}
	}
	return f
}

func pageParityProportion(good, total int) string {
	if total == 0 {
		return "not_applicable"
	}
	if good == total {
		return "pass"
	}
	if good == 0 {
		return "fail"
	}
	return "partial"
}

func pageJSONLDShape(value any) (bool, bool) {
	switch v := value.(type) {
	case map[string]any:
		return len(v) > 0, schemaValuePresent(v["@context"])
	case []any:
		if len(v) == 0 {
			return false, false
		}
		context := false
		for _, item := range v {
			obj, ok := item.(map[string]any)
			if !ok || len(obj) == 0 {
				return false, false
			}
			context = context || schemaValuePresent(obj["@context"])
		}
		return true, context
	}
	return false, false
}

func pageHeaderChecks(p pageResult, headers http.Header) []reportmodel.Finding {
	type headerCheck struct{ id, title, name, fix, spec string }
	specs := []headerCheck{
		{"seo.transport_security", "HSTS policy", "Strict-Transport-Security", "Strict-Transport-Security: max-age=31536000", "https://www.rfc-editor.org/rfc/rfc6797"},
		{"seo.csp", "Content Security Policy", "Content-Security-Policy", "Content-Security-Policy: default-src 'self'; object-src 'none'; base-uri 'self'", "https://www.w3.org/TR/CSP3/"},
		{"seo.nosniff", "MIME sniffing protection", "X-Content-Type-Options", "X-Content-Type-Options: nosniff", "https://fetch.spec.whatwg.org/#x-content-type-options-header"},
		{"seo.referrer_policy", "Referrer policy", "Referrer-Policy", "Referrer-Policy: strict-origin-when-cross-origin", "https://www.w3.org/TR/referrer-policy/"},
	}
	out := make([]reportmodel.Finding, 0, 8)
	for _, spec := range specs {
		f := pageParityFinding(p, spec.id, "seo", spec.title, "Configure a policy appropriate to your site. Example: "+spec.fix, spec.spec, 1)
		f.EvidenceLevel = "observed_response"
		f.Applicability = "Evaluates the final response header; policy suitability and complete browser protection need separate review."
		raw := headers.Get(spec.name)
		f.Evidence = fmt.Sprintf("%s: %s", spec.name, excerpt(raw))
		if p.page.Error != "" || p.page.StatusCode != 200 {
			f.Evidence = "Successful page response not observed; header not scored."
			f.EvidenceLevel = ""
			out = append(out, f)
			continue
		}
		f.Status = "fail"
		switch spec.id {
		case "seo.transport_security":
			if !strings.HasPrefix(strings.ToLower(p.page.URL), "https://") {
				f.Status = "not_applicable"
				break
			}
			maxAgeDirectives := 0
			for _, part := range strings.Split(raw, ";") {
				k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
				if ok && strings.EqualFold(k, "max-age") {
					maxAgeDirectives++
					if strings.HasPrefix(v, `"`) {
						unquoted, e := strconv.Unquote(v)
						if e != nil {
							continue
						}
						v = unquoted
					}
					age, e := strconv.ParseUint(v, 10, 64)
					if e == nil && age > 0 {
						f.Status = "pass"
					}
				}
			}
			if maxAgeDirectives != 1 {
				f.Status = "fail"
			}
		case "seo.csp":
			if raw != "" {
				f.Status = "partial"
				for _, part := range strings.Split(raw, ";") {
					fields := strings.Fields(part)
					if len(fields) > 1 && (fields[0] == "default-src" || fields[0] == "script-src") {
						f.Status = "pass"
					}
				}
			}
		case "seo.nosniff":
			if strings.EqualFold(strings.TrimSpace(raw), "nosniff") {
				f.Status = "pass"
			}
		case "seo.referrer_policy":
			for _, part := range strings.Split(raw, ",") {
				switch strings.TrimSpace(part) {
				case "no-referrer", "same-origin", "strict-origin", "strict-origin-when-cross-origin":
					f.Status = "pass"
				case "origin", "origin-when-cross-origin", "no-referrer-when-downgrade", "unsafe-url":
					f.Status = "partial"
				}
			}
		}
		out = append(out, f)
	}
	observation := func(id, title, evidence, fix, spec string) {
		f := pageParityFinding(p, id, "seo", title, fix, spec, 0)
		f.Status = "information"
		f.EvidenceLevel = "observed_response"
		f.Evidence = evidence
		f.Applicability = "Informational observation of this scanner response, not a browser performance score."
		if p.page.Error != "" || p.page.StatusCode != 200 {
			f.Status = "unknown"
			f.Evidence = "Successful page response not observed."
			f.EvidenceLevel = ""
		}
		out = append(out, f)
	}
	observation("seo.html_weight", "Observed HTML body size", fmt.Sprintf("%d decoded HTML body bytes; wire transfer and subresources excluded.", p.htmlBytes), "Reduce unnecessary markup and embedded data while retaining useful server-rendered content.", "")
	observation("seo.compression", "Observed compression metadata", fmt.Sprintf("Content-Encoding=%q. Missing encoding does not establish missing compression because the transport may decompress responses.", excerpt(headers.Get("Content-Encoding"))), "Enable negotiated gzip/Brotli for compressible content; verify the wire response independently.", "https://www.rfc-editor.org/rfc/rfc9110#section-8.4")
	observation("seo.cache_policy", "Observed cache policy", fmt.Sprintf("Cache-Control=%q; ETag=%t; Last-Modified=%t. Private/no-store can be correct for sensitive/dynamic responses.", excerpt(headers.Get("Cache-Control")), headers.Get("ETag") != "", headers.Get("Last-Modified") != ""), "Choose appropriate caching and validators for reusable public content.", "https://www.rfc-editor.org/rfc/rfc9111")
	observation("seo.permissions_policy", "Observed permissions policy", fmt.Sprintf("Permissions-Policy present=%t; suitability and syntax not certified.", headers.Get("Permissions-Policy") != ""), "Restrict unused capabilities where appropriate: Permissions-Policy: camera=(), microphone=(), geolocation=().", "https://www.w3.org/TR/permissions-policy/")
	ttfb := pageParityFinding(p, "seo.ttfb", "seo", "Observed response first byte", "Inspect origin latency, connection setup, cache behavior and network path before optimizing; compare repeated measurements from representative regions.", "https://www.w3.org/TR/resource-timing-2/", 0)
	ttfb.Evidence = "First-response-byte timing was not observed by the HTTP transport."
	if p.ttfbObserved {
		ttfb.Status = "information"
		ttfb.EvidenceLevel = "observed_response"
		ttfb.Evidence = fmt.Sprintf("Final HTTP hop first response byte observed after %.2f ms from RoundTrip start, including connection setup but excluding robots checks, slot wait, earlier redirects and body download. This is one scanner-region observation, not Core Web Vitals.", float64(p.ttfb)/float64(time.Millisecond))
	} else {
		ttfb.EvidenceLevel = ""
	}
	out = append(out, ttfb)
	tls := pageParityFinding(p, "seo.tls_expiry", "seo", "Observed TLS certificate expiry", "Renew and serve a valid certificate before expiry; automate renewal and monitor it independently.", "https://www.rfc-editor.org/rfc/rfc5280#section-4.1.2.5", 1)
	tls.Evidence = "Certificate metadata was not observed on the final HTTPS response."
	if !strings.HasPrefix(strings.ToLower(p.page.URL), "https://") {
		tls.Status = "not_applicable"
		tls.Evidence = "Final response is not HTTPS; TLS expiry is not applicable."
	} else if !p.tlsNotAfter.IsZero() {
		tls.Status = "unknown"
		tls.Evidence = fmt.Sprintf("Peer leaf certificate expires %s; verified TLS chain=%t.", p.tlsNotAfter.UTC().Format(time.RFC3339), p.tlsVerified)
		if p.tlsVerified {
			tls.Status = "pass"
			if !p.tlsNotAfter.After(time.Now()) {
				tls.Status = "fail"
			} else if time.Until(p.tlsNotAfter) < 30*24*time.Hour {
				tls.Status = "partial"
			}
		}
	}
	if tls.Status != "unknown" {
		tls.EvidenceLevel = "observed_response"
	} else {
		tls.EvidenceLevel = ""
	}
	out = append(out, tls)
	return out
}
