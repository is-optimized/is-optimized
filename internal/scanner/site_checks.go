package scanner

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"golang.org/x/net/html"
)

func siteAgentChecks(ss *session, home *url.URL, homepage response, homepagePage pageResult) []reportmodel.Finding {
	if homepage.code == 0 {
		return []reportmodel.Finding{
			{ID: "agent.markdown", Pillar: "agent", Title: "Markdown content negotiation", Status: "unknown", Weight: 3, Tier: "essential", URL: origin(home) + "/", Evidence: "Homepage response unavailable; site-level representation was not probed."},
			{ID: "agent.markdown_404", Pillar: "agent", Title: "Markdown-aware not-found response", Status: "unknown", Weight: 3, Tier: "essential", URL: origin(home), Evidence: "Homepage response unavailable; not-found behavior was not probed."},
			{ID: "agent.robots", Pillar: "agent", Title: "AI crawler access policy", Status: "unknown", Weight: 3, Tier: "essential", URL: origin(home) + "/robots.txt", Evidence: "Homepage response unavailable; retrieval behavior was not probed."},
			trustFinding("geo.trust.about", "About page", "/about", "unknown", "Homepage response unavailable; trust-page discovery was not probed."),
			trustFinding("geo.trust.contact", "Contact page", "/contact", "unknown", "Homepage response unavailable; trust-page discovery was not probed."),
			trustFinding("geo.trust.privacy", "Privacy page", "/privacy", "unknown", "Homepage response unavailable; trust-page discovery was not probed."),
			trustFinding("geo.trust.terms", "Terms page", "/terms", "unknown", "Homepage response unavailable; trust-page discovery was not probed."),
		}
	}
	results := make(chan reportmodel.Finding, 7)
	go func() { results <- markdownNegotiationCheck(ss, home, homepage) }()
	go func() { results <- markdownNotFoundCheck(ss, home) }()
	go func() { results <- aiCrawlerAccessCheck(ss, home, homepage) }()
	for _, trust := range trustPageDefinitions() {
		trust := trust
		go func() { results <- trustPageCheck(ss, home, homepagePage, trust) }()
	}
	out := make([]reportmodel.Finding, 0, 7)
	for i := 0; i < cap(results); i++ {
		out = append(out, <-results)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

type trustPageDefinition struct {
	ID, Title, Route, LinkTerm string
}

func trustPageDefinitions() []trustPageDefinition {
	return []trustPageDefinition{
		{ID: "geo.trust.about", Title: "About page", Route: "/about", LinkTerm: "about"},
		{ID: "geo.trust.contact", Title: "Contact page", Route: "/contact", LinkTerm: "contact"},
		{ID: "geo.trust.privacy", Title: "Privacy page", Route: "/privacy", LinkTerm: "privacy"},
		{ID: "geo.trust.terms", Title: "Terms page", Route: "/terms", LinkTerm: "terms"},
	}
}

func trustFinding(id, title, route, status, evidence string) reportmodel.Finding {
	return reportmodel.Finding{
		ID: id, Pillar: "geo", Title: title, Status: status, Weight: 1, Tier: "recommended", Stage: "read",
		URL: route, Evidence: evidence,
		Applicability: "Assesses whether the site publishes a public first-party trust page; specific legal or business claims are not verified.",
		Remediation:   "Publish a useful first-party page for this topic and link it from the homepage or footer.",
	}
}

func trustPageCheck(ss *session, home *url.URL, homepage pageResult, def trustPageDefinition) reportmodel.Finding {
	f := trustFinding(def.ID, def.Title, origin(home)+def.Route, "unknown", "Trust-page response was not observed.")
	if homepage.doc == nil {
		f.Evidence = "Homepage HTML unavailable; trust-page links were not inspected."
		return f
	}
	probeURL := origin(home) + def.Route
	var linked string
	walk(homepage.doc, func(n *html.Node) {
		if linked != "" || n.Type != html.ElementNode || n.Data != "a" {
			return
		}
		u, ok := readinessLink(attr(n, "href"), home)
		if ok && origin(u) == origin(home) && strings.Contains(strings.ToLower(u.Path), def.LinkTerm) {
			linked = u.String()
		}
	})
	if linked != "" {
		probeURL = linked
	}
	r, err := ss.probe(probeURL, origin(home))
	if err != nil {
		f.Evidence = "Trust-page fetch was not observed: " + err.Error()
		return f
	}
	f.URL = r.url.String()
	page := analyze(r, nil, probeURL, 0)
	visibleBytes := 0
	if page.doc != nil {
		visibleBytes = len(initialText(page.doc))
	}
	f.Evidence = fmt.Sprintf("%s returned HTTP %d with %d visible-text bytes%s", def.Title, r.code, visibleBytes, map[bool]string{true: "; linked from homepage", false: "; standard path probed"}[linked != ""])
	if r.code != http.StatusOK || page.doc == nil {
		f.Status = "fail"
	} else if visibleBytes >= 500 {
		f.Status = "pass"
	} else if visibleBytes > 0 {
		f.Status = "partial"
	} else {
		f.Status = "fail"
	}
	return f
}

func aiCrawlerAccessCheck(ss *session, home *url.URL, homepage response) reportmodel.Finding {
	f := reportmodel.Finding{
		ID: "agent.robots", Pillar: "agent", Title: "AI crawler access policy", Status: "unknown",
		Weight: 3, Tier: "essential", Stage: "find", URL: origin(home) + "/robots.txt",
		Applicability: "Reports the published robots policy and read-only retrieval behavior; crawler policy choices are controlled by the site owner.",
		Remediation:   "Allow retrieval agents you intend to serve in robots.txt. Training-bot restrictions are a valid policy choice and are not penalized.",
	}
	policy := ss.getPolicy(home)
	if policy.err != nil {
		f.Evidence = "robots.txt policy was unavailable or malformed; agent rules and Content-Signal directives could not be evaluated."
		return f
	}
	retrieval := []string{"ChatGPT-User", "OAI-SearchBot", "Claude-User", "PerplexityBot"}
	type result struct {
		ua        string
		allowed   bool
		specified bool
		resp      response
		err       error
	}
	results := make(chan result, len(retrieval))
	for _, ua := range retrieval {
		go func(ua string) {
			allowed, specified := policy.rules.decision(ua, "/")
			if !allowed {
				results <- result{ua: ua, allowed: false, specified: specified, err: fmt.Errorf("disallowed by robots policy")}
				return
			}
			headers := make(http.Header)
			headers.Set("User-Agent", ua)
			r, err := ss.probeHeaders(origin(home)+"/", origin(home), headers)
			results <- result{ua: ua, allowed: allowed, specified: specified, resp: r, err: err}
		}(ua)
	}
	var observed []result
	blocked := false
	unobserved := false
	for range retrieval {
		observed = append(observed, <-results)
	}
	sort.Slice(observed, func(i, j int) bool { return observed[i].ua < observed[j].ua })
	var robotMatrix, liveMatrix []string
	for _, r := range observed {
		if r.specified {
			state := "allowed"
			if !r.allowed {
				state = "blocked"
				blocked = true
			}
			robotMatrix = append(robotMatrix, fmt.Sprintf("%s=%s", r.ua, state))
		} else {
			robotMatrix = append(robotMatrix, fmt.Sprintf("%s=unspecified", r.ua))
		}
		if r.err != nil {
			unobserved = true
			liveMatrix = append(liveMatrix, r.ua+"=unobserved")
			continue
		}
		ratio := 0.0
		if len(homepage.body) > 0 {
			ratio = float64(len(r.resp.body)) / float64(len(homepage.body))
		}
		liveMatrix = append(liveMatrix, fmt.Sprintf("%s=%d/%d%%", r.ua, r.resp.code, int(ratio*100)))
		if r.resp.code == http.StatusForbidden || r.resp.code == http.StatusUnauthorized || challengeBody(r.resp.body) {
			blocked = true
		}
		if r.resp.code != http.StatusOK || ratio < .8 {
			unobserved = true
		}
	}
	var lines []string
	if len(policy.rules.contentSignals) > 0 {
		lines = append(lines, "Content-Signal="+strings.Join(policy.rules.contentSignals, ";"))
	} else {
		lines = append(lines, "Content-Signal=not observed")
	}
	if !policy.present {
		lines = append(lines, "robots.txt has no rules: everything is allowed by default")
	}
	lines = append(lines, "robots retrieval "+strings.Join(robotMatrix, ","))
	lines = append(lines, "live GET "+strings.Join(liveMatrix, ","))
	training := []string{"GPTBot", "ClaudeBot", "anthropic-ai", "Google-Extended", "Applebot-Extended", "CCBot", "Bytespider", "meta-externalagent"}
	var trainingMatrix []string
	for _, ua := range training {
		allowed, specified := policy.rules.decision(ua, "/")
		state := "unspecified (allowed by default)"
		if specified {
			state = "allowed"
			if !allowed {
				state = "blocked"
			}
		}
		trainingMatrix = append(trainingMatrix, ua+"="+state)
	}
	lines = append(lines, "training (informational) "+strings.Join(trainingMatrix, ","))
	f.Evidence = excerptLimit(strings.Join(lines, "; "), 600)
	if blocked {
		f.Status = "fail"
	} else if unobserved {
		f.Status = "unknown"
	} else {
		f.Status = "pass"
	}
	return f
}

func challengeBody(body []byte) bool {
	text := strings.ToLower(string(body))
	return strings.Contains(text, "captcha") || strings.Contains(text, "challenge-platform") || strings.Contains(text, "access denied")
}

func markdownNegotiationCheck(ss *session, home *url.URL, homepage response) reportmodel.Finding {
	f := reportmodel.Finding{
		ID: "agent.markdown", Pillar: "agent", Title: "Markdown content negotiation", Status: "unknown",
		Weight: 3, Tier: "essential", Stage: "read", URL: origin(home) + "/",
		Applicability: "Applies to a site's public page representation; Markdown is optional but helps text-first clients.",
		Remediation:   "Serve Markdown when Accept: text/markdown is requested, include Vary: Accept, or publish a working Markdown alternate such as /index.md.",
	}
	r, err := ss.probeAccept(f.URL, origin(home), "text/markdown")
	if err != nil {
		f.Evidence = "Markdown representation was not observed: " + err.Error()
		return f
	}
	mediaType, _, _ := mime.ParseMediaType(r.contentType)
	isMarkdown := strings.EqualFold(mediaType, "text/markdown") && len(strings.TrimSpace(string(r.body))) > 0 && !looksLikeHTML(r.body)
	varyAccept := hasToken(r.header.Get("Vary"), "Accept")
	f.Evidence = fmt.Sprintf("GET / with Accept: text/markdown returned HTTP %d, Content-Type %s, Vary %q, %d body bytes", r.code, mediaType, r.header.Get("Vary"), len(r.body))
	homeType, _, _ := mime.ParseMediaType(homepage.contentType)
	htmlAvailable := homepage.code == http.StatusOK && strings.EqualFold(homeType, "text/html")
	if isMarkdown && varyAccept && htmlAvailable {
		f.Status = "pass"
		return f
	}
	if isMarkdown || varyAccept || !htmlAvailable {
		f.Status = "partial"
		return f
	}
	alt, altErr := ss.probe(origin(home)+"/index.md", origin(home))
	if altErr == nil && alt.code == http.StatusOK && markdownResponse(alt) {
		f.Status = "partial"
		f.Evidence += "; /index.md fallback returns Markdown"
		return f
	}
	f.Status = "fail"
	if altErr != nil {
		f.Evidence += "; /index.md fallback was not observed: " + altErr.Error()
	} else {
		f.Evidence += fmt.Sprintf("; /index.md fallback returned HTTP %d and Content-Type %s", alt.code, alt.contentType)
	}
	return f
}

func markdownNotFoundCheck(ss *session, home *url.URL) reportmodel.Finding {
	f := reportmodel.Finding{
		ID: "agent.markdown_404", Pillar: "agent", Title: "Markdown-aware not-found response", Status: "unknown",
		Weight: 3, Tier: "essential", Stage: "read", URL: origin(home),
		Applicability: "Applies to nonexistent public paths requested by text-first clients.",
		Remediation:   "Keep a 404 or 410 status and return a short text/markdown explanation with a link to documentation, a sitemap, or llms.txt when Markdown is requested.",
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		f.Evidence = "Could not create a unique not-found probe path."
		return f
	}
	probeURL := origin(home) + "/__is-optimized-404-" + hex.EncodeToString(nonce[:])
	html, htmlErr := ss.probeAccept(probeURL, origin(home), "text/html")
	markdown, markdownErr := ss.probeAccept(probeURL, origin(home), "text/markdown")
	if htmlErr != nil || markdownErr != nil {
		f.Evidence = fmt.Sprintf("Not-found probes were incomplete (HTML: %v; Markdown: %v)", htmlErr, markdownErr)
		return f
	}
	mediaType, _, _ := mime.ParseMediaType(markdown.contentType)
	isMarkdown := strings.EqualFold(mediaType, "text/markdown") && len(strings.TrimSpace(string(markdown.body))) >= 20 && !looksLikeHTML(markdown.body) && containsMarkdownLink(string(markdown.body))
	f.Evidence = fmt.Sprintf("Unique path returned HTML HTTP %d and Markdown HTTP %d with Content-Type %s and %d Markdown bytes", html.code, markdown.code, mediaType, len(markdown.body))
	if (html.code == http.StatusNotFound || html.code == http.StatusGone) &&
		(markdown.code == http.StatusNotFound || markdown.code == http.StatusGone) && isMarkdown {
		f.Status = "pass"
		return f
	}
	if (html.code == http.StatusNotFound || html.code == http.StatusGone) &&
		(markdown.code == http.StatusNotFound || markdown.code == http.StatusGone) {
		f.Status = "partial"
		return f
	}
	f.Status = "fail"
	return f
}

func markdownResponse(r response) bool {
	t, _, _ := mime.ParseMediaType(r.contentType)
	return strings.EqualFold(t, "text/markdown") && len(strings.TrimSpace(string(r.body))) > 0 && !looksLikeHTML(r.body)
}

func looksLikeHTML(body []byte) bool {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	return strings.HasPrefix(text, "<!doctype html") || strings.HasPrefix(text, "<html") || strings.Contains(text, "<body")
}

func containsMarkdownLink(body string) bool {
	open := strings.Index(body, "](")
	if open < 1 {
		return false
	}
	end := strings.Index(body[open+2:], ")")
	return end > 0
}
