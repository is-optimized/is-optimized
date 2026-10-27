package scanner

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func reply(r *http.Request, status int, body, kind string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{kind}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}
func fixtureScanner(f roundTrip) *Scanner {
	return &Scanner{transport: f, slots: make(chan struct{}, 16)}
}
func TestBoundedDepthAndConcurrency(t *testing.T) {
	var mu sync.Mutex
	var requests []string
	var active, peak atomic.Int32
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requests = append(requests, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/robots.txt" {
			return reply(r, 404, "", "text/plain"), nil
		}
		if strings.HasPrefix(r.URL.Path, "/llms") {
			return reply(r, 404, "", "text/plain"), nil
		}
		if r.URL.Path == "/" {
			return reply(r, 200, `<main><h1>Home</h1><a href="/a#x">A</a><a href="/a#y">Duplicate</a><a href="https://other.com/">Other</a><a href="/logout">Log out</a><a href="/search?q=1">Query</a><a href="/download.pdf">PDF</a><a href="/b">B</a><a href="/c">C</a><a href="/d">D</a><a href="/e">E</a></main>`, "text/html"), nil
		}
		n := active.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		return reply(r, 200, `<h1>Child</h1><a href="/deep">Deeper</a>`, "text/html"), nil
	})
	report, err := s.Scan(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Pages) != 5 {
		t.Fatalf("pages=%d", len(report.Pages))
	}
	if peak.Load() != 4 {
		t.Fatalf("want four concurrent children; got %d", peak.Load())
	}
	for i, p := range report.Pages {
		want := []string{"/", "/a", "/b", "/c", "/d"}[i]
		if !strings.HasSuffix(p.URL, want) {
			t.Errorf("page %d: %s", i, p.URL)
		}
	}
	for _, p := range requests {
		if p == "/deep" || p == "/e" || p == "/logout" {
			t.Errorf("unexpected fetch %s", p)
		}
	}
	if requests[0] != "/robots.txt" {
		t.Fatalf("first fetch was %s", requests[0])
	}
}
func TestRobotsBeforeRedirectOrigin(t *testing.T) {
	var requests []string
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.String())
		if r.URL.Host == "example.com" {
			if r.URL.Path == "/robots.txt" {
				return reply(r, 404, "", "text/plain"), nil
			}
			res := reply(r, 302, "", "text/html")
			res.Header.Set("Location", "https://other.com/")
			return res, nil
		}
		if r.URL.Path == "/robots.txt" {
			return reply(r, 200, "User-agent: *\nDisallow: /", "text/plain"), nil
		}
		t.Fatal("fetched disallowed redirected homepage")
		return nil, nil
	})
	r, e := s.Scan(context.Background(), "example.com")
	if e != nil {
		t.Fatal(e)
	}
	if len(requests) != 3 {
		t.Fatal(requests)
	}
	if !strings.Contains(r.Pages[0].Error, "disallowed") {
		t.Fatal(r.Pages[0])
	}
	for _, p := range r.Pillars {
		if p.Score != nil {
			t.Fatal("unknown page published score")
		}
	}
}
func TestRobotsUnavailableStopsContent(t *testing.T) {
	for _, code := range []int{403, 429, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			calls := 0
			s := fixtureScanner(func(r *http.Request) (*http.Response, error) { calls++; return reply(r, code, "", "text/plain"), nil })
			r, e := s.Scan(context.Background(), "example.com")
			if e != nil || calls != 1 || r.Pages[0].Error == "" {
				t.Fatalf("calls=%d error=%v report=%+v", calls, e, r)
			}
		})
	}
}
func TestRobotsMatching(t *testing.T) {
	r, e := parseRobots("User-agent: *\nDisallow: /\nUser-agent: IsOptimizedBot\nDisallow: /secret*\nAllow: /secret/public$\n")
	if e != nil {
		t.Fatal(e)
	}
	for p, want := range map[string]bool{"/": true, "/secret": false, "/secret/x": false, "/secret/public": true, "/secret/public/extra": false} {
		if got := r.allowed(p); got != want {
			t.Errorf("%s=%t want %t", p, got, want)
		}
	}
	if _, e = parseRobots("<html>oops</html>"); e == nil {
		t.Fatal("HTML accepted as robots")
	}
}
func TestPublicAddressValidation(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "192.0.2.1", "::1", "::ffff:8.8.8.8", "fc00::1", "2001:db8::1", "64:ff9b::a00:1", "2002:a00:1::"} {
		if publicIP(netip.MustParseAddr(raw)) {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		if !publicIP(netip.MustParseAddr(raw)) {
			t.Errorf("rejected public %s", raw)
		}
	}
}
func TestNormalizeDomain(t *testing.T) {
	for _, bad := range []string{"localhost", "https://user:pass@example.com", "file:///tmp/x", "https://example.com:8080/", "https://example.com/a", "127.0.0.1", "example.com?x=y", "example.com#x", "example.com."} {
		if _, e := NormalizeDomain(bad); e == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	if d, e := NormalizeDomain(" HTTPS://Example.COM/ "); e != nil || d != "example.com" {
		t.Fatalf("%q %v", d, e)
	}
}
func TestByteLimitIsUnknown(t *testing.T) {
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/robots.txt" {
			return reply(r, 404, "", "text/plain"), nil
		}
		return reply(r, 200, strings.Repeat("a", maxBody+1), "text/html"), nil
	})
	r, e := s.Scan(context.Background(), "example.com")
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(r.Pages[0].Error, "size limit") {
		t.Fatal(r.Pages[0])
	}
	if r.Pillars[0].Score != nil {
		t.Fatal("oversize page received score")
	}
}
func TestScoreCoverageAndApplicability(t *testing.T) {
	f := []reportmodel.Finding{{Pillar: "seo", Status: "pass", Weight: 8}, {Pillar: "seo", Status: "unknown", Weight: 2}, {Pillar: "seo", Status: "not_applicable", Weight: 100}}
	p := score(f)[0]
	if p.Score == nil || *p.Score != 100 || p.Coverage != .8 {
		t.Fatalf("%+v", p)
	}
	f[1].Foundational = true
	if score(f)[0].Score != nil {
		t.Fatal("foundational unknown scored")
	}
	f[1].Foundational = false
	f[1].Weight = 3
	if score(f)[0].Score != nil {
		t.Fatal("insufficient coverage scored")
	}
}
func TestRedirectPrivateRejectedBeforeTransport(t *testing.T) {
	calls := 0
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path == "/robots.txt" {
			return reply(r, 404, "", "text/plain"), nil
		}
		v := reply(r, 302, "", "text/html")
		v.Header.Set("Location", "http://169.254.169.254/")
		return v, nil
	})
	r, e := s.Scan(context.Background(), "example.com")
	if e != nil || calls != 2 || r.Pages[0].Error == "" {
		t.Fatalf("calls=%d err=%v report=%+v", calls, e, r)
	}
}
func TestRequestBudget(t *testing.T) {
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) { return reply(r, 200, "ok", "text/plain"), nil })
	ss := &session{s: s, ctx: context.Background(), policies: map[string]policy{}}
	for i := 0; i < 32; i++ {
		if _, e := ss.fetch("https://example.com/", false, ""); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := ss.fetch("https://example.com/", false, ""); e == nil {
		t.Fatal("request budget ignored")
	}
}
func TestDiscoveryLimited(t *testing.T) {
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) { return reply(r, 404, "", "text/plain"), nil })
	ss := &session{s: s, ctx: context.Background(), policies: map[string]policy{}}
	u, _ := url.Parse("https://example.com/")
	ss.getPolicy(u)
	for i := 0; i < 7; i++ {
		if _, e := ss.probe("https://example.com/llms.txt", origin(u)); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := ss.probe("https://example.com/llms.txt", origin(u)); e == nil {
		t.Fatal("discovery budget ignored")
	}
}

func TestOverlongLinkRejected(t *testing.T) {
	u, _ := url.Parse("https://example.com/" + strings.Repeat("x", 3000))
	if validateURL(u) == nil {
		t.Fatal("overlong URL accepted")
	}
}

func TestChallengeAndComments(t *testing.T) {
	if _, e := parseRobots("# only a comment\n"); e != nil {
		t.Fatal(e)
	}
	req, _ := http.NewRequest("GET", "https://example.com/", nil)
	r := response{url: req.URL, code: 200, body: []byte("<title>Just a moment...</title>"), contentType: "text/html", header: make(http.Header)}
	p := analyze(r, nil, req.URL.String(), 0)
	if p.page.Error == "" {
		t.Fatal("challenge scored as observed content")
	}
	for _, f := range evaluate(p) {
		if f.Status != "unknown" {
			t.Fatal(f)
		}
	}
}
