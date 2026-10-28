// Package scanner performs bounded, read-only public website assessments.
package scanner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
)

const MethodologyVersion = "2026-10-v2"
const maxBody = 2 << 20

type Scanner struct {
	transport http.RoundTripper
	slots     chan struct{}
}

func New() *Scanner { return &Scanner{transport: secureTransport(), slots: make(chan struct{}, 16)} }

type policy struct {
	rules robots
	err   error
}
type session struct {
	s         *Scanner
	ctx       context.Context
	requests  atomic.Int32
	bytes     atomic.Int64
	discovery atomic.Int32
	mu        sync.Mutex
	policies  map[string]policy
}
type response struct {
	url         *url.URL
	code        int
	body        []byte
	contentType string
	header      http.Header
}

func (ss *session) fetch(raw string, content bool, site string) (response, error) {
	var out response
	u, err := url.Parse(raw)
	if err != nil {
		return out, errors.New("invalid URL")
	}
	for redirects := 0; redirects <= 3; redirects++ {
		if err = validateURL(u); err != nil {
			return out, err
		}
		if site != "" && origin(u) != site {
			return out, errors.New("redirect leaves site boundary")
		}
		if content {
			p := ss.getPolicy(u)
			if p.err != nil {
				return out, p.err
			}
			robotPath := u.EscapedPath()
			if u.RawQuery != "" {
				robotPath += "?" + u.RawQuery
			}
			if !p.rules.allowed(robotPath) {
				return out, errors.New("fetch disallowed by robots policy")
			}
		}
		if ss.requests.Add(1) > 32 {
			return out, errors.New("scan request budget exhausted")
		}
		ctx, cancel := context.WithTimeout(ss.ctx, 5*time.Second)
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if e != nil {
			cancel()
			return out, e
		}
		req.Header.Set("User-Agent", userAgent+"/1.0 (+https://is-optimized.com)")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json,text/plain,application/xml;q=0.8")
		select {
		case ss.s.slots <- struct{}{}:
		case <-ctx.Done():
			cancel()
			return out, ctx.Err()
		}
		resp, e := ss.s.transport.RoundTrip(req)
		if e != nil {
			<-ss.s.slots
			cancel()
			return out, errors.New("target fetch failed")
		}
		if resp.StatusCode >= 300 && resp.StatusCode <= 399 && resp.StatusCode != 304 {
			loc, e := resp.Location()
			resp.Body.Close()
			<-ss.s.slots
			cancel()
			if e != nil {
				return out, errors.New("invalid redirect")
			}
			if redirects == 3 {
				return out, errors.New("redirect limit reached")
			}
			u = loc
			continue
		}
		data, e := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
		resp.Body.Close()
		<-ss.s.slots
		cancel()
		out = response{u, resp.StatusCode, data, resp.Header.Get("Content-Type"), resp.Header.Clone()}
		if ss.bytes.Add(int64(len(data))) > 12<<20 {
			return out, errors.New("scan byte budget exhausted")
		}
		if len(data) > maxBody {
			return out, errors.New("response exceeded size limit")
		}
		if e != nil {
			return out, errors.New("incomplete response body")
		}
		return out, nil
	}
	return out, errors.New("redirect limit reached")
}
func (ss *session) getPolicy(u *url.URL) policy {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	key := origin(u)
	if p, ok := ss.policies[key]; ok {
		return p
	}
	p := policy{}
	if ss.discovery.Add(1) > 8 {
		p.err = errors.New("discovery budget exhausted")
	} else {
		r, e := ss.fetch(key+"/robots.txt", false, key)
		switch {
		case e != nil:
			p.err = errors.New("robots policy unavailable")
		case r.code == 404 || r.code == 410: // absent policy permits crawling
		case r.code != 200:
			p.err = errors.New("robots policy unavailable")
		default:
			p.rules, p.err = parseRobots(string(r.body))
		}
	}
	ss.policies[key] = p
	return p
}
func (ss *session) probe(raw, site string) (response, error) {
	if ss.discovery.Add(1) > 8 {
		return response{}, errors.New("discovery budget exhausted")
	}
	return ss.fetch(raw, true, site)
}
func (s *Scanner) Scan(ctx context.Context, domain string) (reportmodel.Report, error) {
	domain, err := NormalizeDomain(domain)
	if err != nil {
		return reportmodel.Report{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		return reportmodel.Report{}, err
	}
	report := reportmodel.Report{ID: hex.EncodeToString(id), Domain: domain, StartedAt: time.Now().UTC(), MethodologyVersion: MethodologyVersion, SelectionPolicy: "home-links-document-order-v1"}
	ss := &session{s: s, ctx: ctx, policies: map[string]policy{}}
	start := time.Now()
	home, fetchErr := ss.fetch("https://"+domain+"/", true, "")
	pages := []pageResult{analyze(home, fetchErr, "https://"+domain+"/", time.Since(start))}
	if fetchErr == nil && pages[0].page.Error == "" {
		links := selectLinks(pages[0].doc, home.url)
		results := make([]pageResult, len(links))
		var wg sync.WaitGroup
		for i, link := range links {
			wg.Add(1)
			go func(i int, link string) {
				defer wg.Done()
				start := time.Now()
				r, e := ss.fetch(link, true, origin(home.url))
				results[i] = analyze(r, e, link, time.Since(start))
			}(i, link)
		}
		wg.Wait()
		pages = append(pages, results...)
		// Emerging conventions are informational, not weighted ranking requirements.
		for _, path := range []string{"/llms.txt", "/llms-full.txt"} {
			r, e := ss.probe(origin(home.url)+path, origin(home.url))
			evidence := "Not observed"
			if e == nil {
				evidence = fmt.Sprintf("HTTP %d; %d bytes", r.code, len(r.body))
			}
			report.Findings = append(report.Findings, reportmodel.Finding{ID: "agent.discovery" + path, Pillar: "agent", Title: "Emerging machine-readable content", Status: "information", URL: origin(home.url) + path, Evidence: evidence, Remediation: "Consider a concise, accurate machine-readable summary if useful to your audience; presence alone does not prove agent compatibility."})
		}
		p := ss.getPolicy(home.url)
		if len(p.rules.sitemaps) > 0 {
			u, e := url.Parse(p.rules.sitemaps[0])
			if e == nil && validateURL(u) == nil && origin(u) == origin(home.url) {
				r, e := ss.probe(u.String(), origin(home.url))
				evidence := "Declared sitemap could not be inspected"
				if e == nil {
					evidence = fmt.Sprintf("Declared sitemap returned HTTP %d; contents were not recursively crawled", r.code)
				}
				report.Findings = append(report.Findings, reportmodel.Finding{ID: "seo.sitemap", Pillar: "seo", Title: "Declared sitemap", Status: "information", URL: u.String(), Evidence: evidence, Remediation: "Maintain a valid sitemap containing canonical public URLs."})
			}
		}
	}
	for _, p := range pages {
		report.Pages = append(report.Pages, p.page)
		report.Findings = append(report.Findings, evaluate(p)...)
	}
	report.Findings = markDuplicates(report.Findings, pages)
	report.Pillars = score(report.Findings)
	report.CompletedAt = time.Now().UTC()
	return report, nil
}
func htmlType(s string) bool {
	t, _, err := mime.ParseMediaType(s)
	return err == nil && (t == "text/html" || t == "application/xhtml+xml")
}
func excerpt(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > 300 {
		return string(r[:300]) + "…"
	}
	return s
}
