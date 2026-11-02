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
)

func siteAgentChecks(ss *session, home *url.URL, homepage response) []reportmodel.Finding {
	results := make(chan reportmodel.Finding, 2)
	go func() { results <- markdownNegotiationCheck(ss, home, homepage) }()
	go func() { results <- markdownNotFoundCheck(ss, home) }()
	out := []reportmodel.Finding{<-results, <-results}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
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
