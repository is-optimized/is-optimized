// Package scanner performs bounded, read-only public website assessments.
package scanner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
)

const MethodologyVersion = "2026-10-agentready-v3"
const maxBody = 2 << 20
const maxContentPages = 32
const maxRequestCount = 64
const maxDiscoveryCount = 8
const maxScanBytes = 12 << 20
const maxScanDuration = 60 * time.Second
const defaultPageConcurrency = 8
const maxSitemapDocuments = 5
const maxSitemapCandidates = 5000

type Scanner struct {
	transport       http.RoundTripper
	slots           chan struct{}
	pageConcurrency int
}

func New() *Scanner {
	return &Scanner{transport: secureTransport(), slots: make(chan struct{}, 16), pageConcurrency: defaultPageConcurrency}
}

type policy struct {
	rules robots
	err   error
}
type session struct {
	s                          *Scanner
	ctx                        context.Context
	requests                   atomic.Int32
	bytes                      atomic.Int64
	discovery                  atomic.Int32
	pageWorkers                atomic.Int32
	peakPageWorkers            atomic.Int32
	requestBudgetExhaustions   atomic.Int32
	discoveryBudgetExhaustions atomic.Int32
	scanByteBudgetExhaustions  atomic.Int32
	responseSizeExhaustions    atomic.Int32
	timeouts                   atomic.Int32
	mu                         sync.Mutex
	policies                   map[string]policy
}
type response struct {
	url         *url.URL
	code        int
	body        []byte
	contentType string
	header      http.Header
}

var errScanByteBudget = errors.New("scan byte budget exhausted")

func reserveScanBytes(total *atomic.Int64, requested, limit int64) int64 {
	for {
		used := total.Load()
		if used >= limit {
			return 0
		}
		reserved := requested
		if remaining := limit - used; reserved > remaining {
			reserved = remaining
		}
		if total.CompareAndSwap(used, used+reserved) {
			return reserved
		}
	}
}

// readBudgeted reserves shared scan bytes before each read, so concurrent
// responses cannot all consume a full per-response allowance past the total.
func readBudgeted(r io.Reader, total *atomic.Int64, responseLimit, totalLimit int64) ([]byte, error) {
	var body []byte
	buffer := make([]byte, 32<<10)
	for int64(len(body)) < responseLimit {
		want := int64(len(buffer))
		if remaining := responseLimit - int64(len(body)); want > remaining {
			want = remaining
		}
		reserved := reserveScanBytes(total, want, totalLimit)
		if reserved == 0 {
			return body, errScanByteBudget
		}
		n, err := r.Read(buffer[:int(reserved)])
		if n < int(reserved) {
			total.Add(int64(n) - reserved)
		}
		body = append(body, buffer[:n]...)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return body, nil
			}
			return body, err
		}
		if n == 0 {
			return body, io.ErrNoProgress
		}
	}
	return body, nil
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
		if ss.bytes.Load() >= maxScanBytes {
			ss.scanByteBudgetExhaustions.Add(1)
			return out, errScanByteBudget
		}
		if ss.requests.Add(1) > maxRequestCount {
			ss.requestBudgetExhaustions.Add(1)
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
			if errors.Is(e, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				ss.timeouts.Add(1)
			}
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
		data, e := readBudgeted(resp.Body, &ss.bytes, maxBody+1, maxScanBytes)
		resp.Body.Close()
		<-ss.s.slots
		cancel()
		out = response{u, resp.StatusCode, data, resp.Header.Get("Content-Type"), resp.Header.Clone()}
		if errors.Is(e, errScanByteBudget) {
			ss.scanByteBudgetExhaustions.Add(1)
			return out, e
		}
		if len(data) > maxBody {
			ss.responseSizeExhaustions.Add(1)
			return out, errors.New("response exceeded size limit")
		}
		if e != nil {
			if errors.Is(e, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				ss.timeouts.Add(1)
			}
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
	if ss.discovery.Add(1) > maxDiscoveryCount {
		ss.discoveryBudgetExhaustions.Add(1)
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
	if ss.discovery.Add(1) > maxDiscoveryCount {
		ss.discoveryBudgetExhaustions.Add(1)
		return response{}, errors.New("discovery budget exhausted")
	}
	return ss.fetch(raw, true, site)
}
func (s *Scanner) Scan(ctx context.Context, domain string) (reportmodel.Report, error) {
	domain, err := NormalizeDomain(domain)
	if err != nil {
		return reportmodel.Report{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, maxScanDuration)
	defer cancel()
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		return reportmodel.Report{}, err
	}
	report := reportmodel.Report{ID: hex.EncodeToString(id), Domain: domain, StartedAt: time.Now().UTC(), MethodologyVersion: MethodologyVersion, SelectionPolicy: "home-links-sitemap-two-hop-v2", BehavioralEvaluation: "not_run"}
	ss := &session{s: s, ctx: ctx, policies: map[string]policy{}}
	start := time.Now()
	home, fetchErr := ss.fetch("https://"+domain+"/", true, "")
	pages := []pageResult{analyze(home, fetchErr, "https://"+domain+"/", time.Since(start))}
	sitemapEvidence := "Not inspected"
	if fetchErr == nil && pages[0].page.Error == "" {
		links := selectLinks(pages[0].doc, home.url)
		sitemapLinks, evidence := sitemapURLs(ss, home.url)
		sitemapEvidence = evidence
		selected := selectRepresentative(append(links, sitemapLinks...), home.url, maxContentPages-1)
		results := ss.fetchPages(selected, home.url)
		pages = append(pages, results...)
		// Fill only unused sample slots from one additional link hop. Do not
		// recursively follow these pages or expand the sitemap candidate set.
		remaining := maxContentPages - len(pages)
		if remaining > 0 {
			var secondHop []string
			seen := map[string]bool{home.url.String(): true}
			for _, page := range results {
				seen[page.page.URL] = true
			}
			for _, page := range results {
				if page.doc != nil && page.page.Error == "" {
					for _, candidate := range selectLinks(page.doc, pageURL(page.page.URL)) {
						if !seen[candidate] {
							seen[candidate] = true
							secondHop = append(secondHop, candidate)
						}
					}
				}
			}
			selectedSecond := selectRepresentative(secondHop, home.url, remaining)
			pages = append(pages, ss.fetchPages(selectedSecond, home.url)...)
		}
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
			report.Findings = append(report.Findings, reportmodel.Finding{ID: "seo.sitemap", Pillar: "seo", Title: "Declared sitemap", Status: "information", URL: p.rules.sitemaps[0], Evidence: sitemapEvidence, Remediation: "Maintain a valid sitemap containing canonical public URLs."})
		}
	}
	for _, p := range pages {
		report.Pages = append(report.Pages, p.page)
		report.Findings = append(report.Findings, evaluate(p)...)
	}
	report.Findings = markDuplicates(report.Findings, pages)
	annotateReadiness(report.Findings)
	report.Pillars = score(report.Findings)
	report.CompletedAt = time.Now().UTC()
	failedPages := 0
	for _, page := range report.Pages {
		if page.StatusCode != http.StatusOK || page.Error != "" {
			failedPages++
		}
	}
	concurrency := s.pageConcurrency
	if concurrency < 1 {
		concurrency = defaultPageConcurrency
	}
	slog.Info("scanner_metrics",
		"requests_attempted", ss.requests.Load(), "discovery_requests", ss.discovery.Load(), "response_bytes", ss.bytes.Load(),
		"pages_selected", len(report.Pages), "pages_with_error", failedPages,
		"page_concurrency_limit", concurrency, "peak_page_workers", ss.peakPageWorkers.Load(),
		"outbound_requests_in_flight", len(s.slots), "outbound_request_limit", cap(s.slots),
		"request_budget_exhaustions", ss.requestBudgetExhaustions.Load(), "discovery_budget_exhaustions", ss.discoveryBudgetExhaustions.Load(),
		"scan_byte_budget_exhaustions", ss.scanByteBudgetExhaustions.Load(), "response_size_exhaustions", ss.responseSizeExhaustions.Load(),
		"timeouts", ss.timeouts.Load(), "duration_ms", time.Since(start).Milliseconds(), "deadline_reached", deadlineReached(ctx))
	return report, nil
}

func pageURL(raw string) *url.URL {
	u, _ := url.Parse(raw)
	return u
}

func (ss *session) fetchPages(selected []string, home *url.URL) []pageResult {
	results := make([]pageResult, len(selected))
	limit := defaultPageConcurrency
	if ss.s.pageConcurrency > 0 && ss.s.pageConcurrency < limit {
		limit = ss.s.pageConcurrency
	}
	workers := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i, link := range selected {
		wg.Add(1)
		go func(i int, link string) {
			defer wg.Done()
			select {
			case workers <- struct{}{}:
			case <-ss.ctx.Done():
				results[i] = analyze(response{}, ss.ctx.Err(), link, 0)
				return
			}
			active := ss.pageWorkers.Add(1)
			for peak := ss.peakPageWorkers.Load(); active > peak; peak = ss.peakPageWorkers.Load() {
				if ss.peakPageWorkers.CompareAndSwap(peak, active) {
					break
				}
			}
			defer func() { ss.pageWorkers.Add(-1); <-workers }()
			started := time.Now()
			r, err := ss.fetch(link, true, origin(home))
			results[i] = analyze(r, err, link, time.Since(started))
		}(i, link)
	}
	wg.Wait()
	return results
}

func deadlineReached(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
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
