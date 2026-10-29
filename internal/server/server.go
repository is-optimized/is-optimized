package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/is-optimized/is-optimized/internal/messaging"
	"github.com/is-optimized/is-optimized/internal/render"
	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"github.com/is-optimized/is-optimized/internal/scanner"
	"github.com/is-optimized/is-optimized/internal/store"
)

type Scanner interface {
	Scan(context.Context, string) (reportmodel.Report, error)
}
type Config struct {
	TrustedProxyCIDRs                                                                            string
	PublicURL, TwilioAccount, TwilioToken, TwilioFrom, Disclosure, DisclosureVersion, SMSWording string
	SMS                                                                                          bool
}
type Server struct {
	db          store.Store
	scan        Scanner
	sender      messaging.Sender
	cfg         Config
	mux         *http.ServeMux
	slots       chan struct{}
	mu          sync.Mutex
	rates       map[string][]time.Time
	wg          sync.WaitGroup
	scanWake    chan struct{}
	messageWake chan struct{}
}
type Job struct {
	ID, Domain, State, Lease, Error string
	Created, Updated, LeaseUntil    time.Time
	Attempts                        int
}
type Queue struct{ Jobs map[string]Job }
type Snapshot struct {
	Report   reportmodel.Report
	Expires  time.Time
	Artifact string
}
type Request struct {
	Token, ReportID, Disclosure, DisclosureVersion, Wording string
	IntendedPhone                                           string
	Marketing                                               bool
	Created, Expires                                        time.Time
	Sender, InboundID, ProviderID, State                    string
	Updated                                                 time.Time
}

var errConflict = errors.New("conflict")

func id() string {
	b := make([]byte, 18)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func New(db store.Store, scan Scanner, sender messaging.Sender, cfg Config) (*Server, error) {
	u, e := url.Parse(cfg.PublicURL)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, errors.New("PUBLIC_URL must be an origin")
	}
	if cfg.SMSWording == "" {
		cfg.SMSWording = ApprovedRequestWording
	}
	if !strings.Contains(cfg.SMSWording, "{token}") {
		cfg.SMSWording += "\nReport code: {token}"
	}
	if cfg.SMS && (u.Scheme != "https" || cfg.TwilioAccount == "" || cfg.TwilioToken == "" || cfg.TwilioFrom == "" || cfg.Disclosure == "" || cfg.DisclosureVersion == "" || sender == nil) {
		return nil, errors.New("SMS needs HTTPS, provider configuration, versioned disclosure, and sender")
	}
	s := &Server{db: db, scan: scan, sender: sender, cfg: cfg, mux: http.NewServeMux(), slots: make(chan struct{}, 4), rates: map[string][]time.Time{}}
	s.scanWake = make(chan struct{}, 1)
	s.messageWake = make(chan struct{}, 1)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	s.mux.HandleFunc("GET /{$}", s.home)
	s.mux.HandleFunc("GET /assets/{name}", render.Asset)
	s.mux.HandleFunc("POST /api/v1/scans", s.createScan)
	s.mux.HandleFunc("GET /api/v1/scans/{id}", s.scanStatus)
	s.mux.HandleFunc("POST /api/v1/report-requests", s.requestReport)
	s.mux.HandleFunc("POST /webhooks/twilio/inbound", s.inbound)
	s.mux.HandleFunc("POST /webhooks/twilio/status", s.callback)
	s.mux.HandleFunc("GET /reports/{id}/pdf", s.pdf)
	s.mux.HandleFunc("GET /reports/{id}", s.report)
	s.mux.HandleFunc("GET /{domain}", s.latest)
	return s, nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if strings.HasPrefix(r.URL.Path, "/api/") && r.Method == "POST" {
		origin := r.Header.Get("Origin")
		if origin != "" && origin != s.cfg.PublicURL {
			http.Error(w, "origin denied", 403)
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return errors.New("extra input")
	}
	return nil
}
func (s *Server) allow(r *http.Request) bool {
	host := s.clientIP(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	a := s.rates[host]
	cut := 0
	for cut < len(a) && now.Sub(a[cut]) > 10*time.Minute {
		cut++
	}
	a = a[cut:]
	if len(a) >= 5 {
		return false
	}
	if len(s.rates) >= 10000 {
		for k, v := range s.rates {
			if len(v) == 0 || now.Sub(v[len(v)-1]) > 10*time.Minute {
				delete(s.rates, k)
			}
		}
		if len(s.rates) >= 10000 {
			return false
		}
	}
	s.rates[host] = append(a, now)
	return true
}
func (s *Server) createScan(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Domain string `json:"domain"`
	}
	if decode(r, &in) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	domain, e := scanner.NormalizeDomain(in.Domain)
	if e != nil {
		http.Error(w, "enter a public domain", 400)
		return
	}
	if !s.allow(r) {
		http.Error(w, "scan limit reached; try later", 429)
		return
	}
	var latest struct{ ID string }
	if s.db.Get(r.Context(), "latest_"+hash(domain), &latest) == nil {
		var snap Snapshot
		if s.db.Get(r.Context(), "report_"+latest.ID, &snap) == nil && time.Since(snap.Report.CompletedAt) < 24*time.Hour && snap.Report.MethodologyVersion == scanner.MethodologyVersion {
			jsonOut(w, 200, map[string]any{"id": latest.ID, "state": "completed", "report": snap.Report})
			return
		}
	}
	var job Job
	now := time.Now().UTC()
	e = s.db.Update(r.Context(), "queue", func(raw json.RawMessage) (any, error) {
		q := Queue{}
		if len(raw) > 0 {
			if e := json.Unmarshal(raw, &q); e != nil {
				return nil, e
			}
		}
		if q.Jobs == nil {
			q.Jobs = map[string]Job{}
		}
		for _, j := range q.Jobs {
			if j.Domain == domain && (j.State == "queued" || j.State == "running") {
				job = j
				return q, nil
			}
		}
		for k, j := range q.Jobs {
			if (j.State == "completed" || j.State == "failed") && now.Sub(j.Updated) > time.Hour {
				delete(q.Jobs, k)
			}
		}
		// Terminal history must not consume the bounded active queue.
		for len(q.Jobs) >= 64 {
			oldest := ""
			var at time.Time
			for k, j := range q.Jobs {
				if j.State == "completed" || j.State == "failed" {
					if oldest == "" || j.Updated.Before(at) {
						oldest = k
						at = j.Updated
					}
				}
			}
			if oldest == "" {
				return nil, errConflict
			}
			delete(q.Jobs, oldest)
		}
		job = Job{ID: id(), Domain: domain, State: "queued", Created: now, Updated: now}
		q.Jobs[job.ID] = job
		return q, nil
	})
	if e != nil {
		http.Error(w, "scan capacity unavailable", 503)
		return
	}
	wake(s.scanWake)
	jsonOut(w, 202, map[string]string{"id": job.ID, "state": job.State})
}
func (s *Server) scanStatus(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("id")
	if !store.KeyOK(key) {
		http.NotFound(w, r)
		return
	}
	var snap Snapshot
	if s.db.Get(r.Context(), "report_"+key, &snap) == nil && time.Now().Before(snap.Expires) {
		jsonOut(w, 200, map[string]any{"id": key, "state": "completed", "report": snap.Report})
		return
	}
	var q Queue
	if s.db.Get(r.Context(), "queue", &q) != nil {
		http.NotFound(w, r)
		return
	}
	j, ok := q.Jobs[key]
	if !ok {
		http.NotFound(w, r)
		return
	}
	jsonOut(w, 200, map[string]any{"id": j.ID, "state": j.State, "error": j.Error})
}
func (s *Server) Run(ctx context.Context) {
	s.run(ctx, 30*time.Second)
}

// Wakeups handle local work immediately; periodic polling recovers persisted
// work after a crash, expired leases, failed writes, or another instance's work.
// Timers are independent of wakeups, so steady traffic cannot starve recovery.
func (s *Server) run(ctx context.Context, recoveryInterval time.Duration) {
	// A single independently scheduled message worker bounds provider traffic
	// without allowing SMS latency to delay scan admission/recovery.
	messageDone := make(chan struct{})
	go func() {
		defer close(messageDone)
		runDispatcher(ctx, recoveryInterval, s.messageWake, s.dispatchMessages)
	}()
	runDispatcher(ctx, recoveryInterval, s.scanWake, func(ctx context.Context) {
		s.recoverPublishing(ctx)
		s.dispatch(ctx)
	})
	s.wg.Wait()
	<-messageDone
}

func wake(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func runDispatcher(ctx context.Context, interval time.Duration, wake <-chan struct{}, dispatch func(context.Context)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// Recover immediately on startup, without waiting for the first tick.
	if ctx.Err() == nil {
		dispatch(ctx)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-wake:
		}
		if ctx.Err() != nil {
			return
		}
		dispatch(ctx)
	}
}
func (s *Server) dispatch(ctx context.Context) {
	for i := 0; i < 4; i++ {
		select {
		case s.slots <- struct{}{}:
		default:
			return
		}
		var claimed Job
		now := time.Now().UTC()
		e := s.db.Update(ctx, "queue", func(raw json.RawMessage) (any, error) {
			claimed = Job{}
			q := Queue{}
			if len(raw) > 0 {
				if e := json.Unmarshal(raw, &q); e != nil {
					return nil, e
				}
			}
			for k, j := range q.Jobs {
				if j.State == "queued" || (j.State == "running" && now.After(j.LeaseUntil)) {
					if j.Attempts >= 3 {
						j.State = "failed"
						j.Error = "scan interrupted repeatedly"
						j.Updated = now
						q.Jobs[k] = j
						continue
					}
					j.State = "running"
					j.Lease = id()
					j.LeaseUntil = now.Add(2 * time.Minute)
					j.Attempts++
					j.Updated = now
					q.Jobs[k] = j
					claimed = j
					break
				}
			}
			return q, nil
		})
		if e != nil || claimed.ID == "" {
			<-s.slots
			return
		}
		s.wg.Add(1)
		go func(j Job) {
			defer s.wg.Done()
			defer func() { <-s.slots; wake(s.scanWake) }()
			s.work(ctx, j)
		}(claimed)
	}
}
func (s *Server) work(ctx context.Context, j Job) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	// Scan evidence survives rendering/storage failures. A retry only rerenders.
	var rep reportmodel.Report
	e := s.db.Get(ctx, "evidence_"+j.ID, &rep)
	if e == store.ErrNotFound {
		rep, e = s.scan.Scan(ctx, j.Domain)
		if e == nil {
			rep.ID = j.ID
			e = s.db.Update(ctx, "evidence_"+j.ID, func(raw json.RawMessage) (any, error) {
				if len(raw) > 0 {
					var prior reportmodel.Report
					if er := json.Unmarshal(raw, &prior); er != nil {
						return nil, er
					}
					rep = prior
					return prior, nil
				}
				return rep, nil
			})
		}
	}
	artifact := j.ID + "_" + j.Lease
	if e == nil {
		var b []byte
		b, e = render.PDF(rep)
		if e == nil {
			e = s.db.PutPDF(ctx, artifact, b)
		}
	}
	snap := Snapshot{Report: rep, Expires: time.Now().Add(30 * 24 * time.Hour), Artifact: artifact}
	// Attempt artifacts are private until the queue lease transaction succeeds.
	if e == nil {
		e = s.db.Update(ctx, "attempt_"+artifact, func(json.RawMessage) (any, error) { return snap, nil })
	}
	published := false
	err := s.db.Update(ctx, "queue", func(raw json.RawMessage) (any, error) {
		published = false
		var q Queue
		if er := json.Unmarshal(raw, &q); er != nil {
			return nil, er
		}
		current, ok := q.Jobs[j.ID]
		if !ok || current.Lease != j.Lease || time.Now().After(current.LeaseUntil) {
			return nil, errConflict
		}
		if e != nil {
			current.State = "queued"
			current.Error = "Report rendering unavailable; retrying saved evidence"
			if current.Attempts >= 3 {
				current.State = "failed"
			}
		} else {
			current.State = "publishing"
			published = true
		}
		current.Updated = time.Now().UTC()
		q.Jobs[j.ID] = current
		return q, nil
	})
	if err == nil && published {
		s.publish(ctx, j, snap)
	}
}
func (s *Server) publish(ctx context.Context, j Job, snap Snapshot) {
	// Only a fenced publishing generation reaches this method. Publishing jobs
	// are recovered by replaying these idempotent writes, never by rescanning.
	if e := s.db.Update(ctx, "report_"+j.ID, func(json.RawMessage) (any, error) { return snap, nil }); e != nil {
		return
	}
	if e := s.db.Update(ctx, "latest_"+hash(j.Domain), func(raw json.RawMessage) (any, error) {
		var old struct {
			ID string
			At time.Time
		}
		if len(raw) > 0 {
			if e := json.Unmarshal(raw, &old); e != nil {
				return nil, e
			}
			if old.At.After(snap.Report.CompletedAt) {
				return old, nil
			}
		}
		return struct {
			ID string
			At time.Time
		}{j.ID, snap.Report.CompletedAt}, nil
	}); e != nil {
		return
	}
	if e := s.db.Update(ctx, "queue", func(raw json.RawMessage) (any, error) {
		var q Queue
		if e := json.Unmarshal(raw, &q); e != nil {
			return nil, e
		}
		cur, ok := q.Jobs[j.ID]
		if !ok || cur.Lease != j.Lease {
			return nil, errConflict
		}
		cur.State = "completed"
		cur.Updated = time.Now().UTC()
		q.Jobs[j.ID] = cur
		return q, nil
	}); e != nil {
		return
	}
	// Authoritative completion is already durable. A directory outage must
	// neither hold active admission nor monopolize the recovery scheduler.
	directoryCtx, cancel := context.WithTimeout(ctx, directoryWriteTimeout)
	defer cancel()
	if e := s.indexPublicReport(directoryCtx, snap); e != nil {
		slog.Warn("public report directory update unavailable")
	}
}
func (s *Server) load(w http.ResponseWriter, r *http.Request, key string) (Snapshot, bool) {
	var snap Snapshot
	if !store.KeyOK(key) || s.db.Get(r.Context(), "report_"+key, &snap) != nil {
		http.NotFound(w, r)
		return snap, false
	}
	if time.Now().After(snap.Expires) {
		http.Error(w, "Report expired. Visit the homepage to request a new scan.", 410)
		return snap, false
	}
	w.Header().Set("X-Robots-Tag", "noindex")
	return snap, true
}
func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	snap, ok := s.load(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	render.HTML(w, snap.Report)
}
func (s *Server) pdf(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("id")
	if _, ok := s.load(w, r, key); !ok {
		return
	}
	snap, ok := s.load(w, r, key)
	if !ok {
		return
	}
	b, e := s.db.PDF(r.Context(), snap.Artifact)
	if e != nil {
		http.Error(w, "PDF temporarily unavailable; HTML report remains available", 503)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="website-readiness.pdf"`)
	w.Write(b)
}
func (s *Server) latest(w http.ResponseWriter, r *http.Request) {
	domain, e := scanner.NormalizeDomain(r.PathValue("domain"))
	if e != nil {
		http.NotFound(w, r)
		return
	}
	var latest struct{ ID string }
	if s.db.Get(r.Context(), "latest_"+hash(domain), &latest) != nil {
		http.NotFound(w, r)
		return
	}
	snap, ok := s.load(w, r, latest.ID)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	render.HTML(w, snap.Report)
}

func (s *Server) recoverPublishing(ctx context.Context) {
	var q Queue
	if s.db.Get(ctx, "queue", &q) != nil {
		return
	}
	for _, j := range q.Jobs {
		if j.State == "publishing" {
			var snap Snapshot
			if s.db.Get(ctx, "attempt_"+j.ID+"_"+j.Lease, &snap) == nil {
				s.publish(ctx, j, snap)
			}
		}
	}
}

func (s *Server) clientIP(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	trusted := func(ip string) bool {
		a, e := netip.ParseAddr(strings.TrimSpace(ip))
		if e != nil {
			return false
		}
		for _, raw := range strings.Split(s.cfg.TrustedProxyCIDRs, ",") {
			p, e := netip.ParsePrefix(strings.TrimSpace(raw))
			if e == nil && p.Contains(a) {
				return true
			}
		}
		return false
	}
	if !trusted(host) {
		return host
	}
	chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	if len(chain) > 16 {
		return host
	}
	for i := len(chain) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(chain[i])
		if _, e := netip.ParseAddr(candidate); e != nil {
			return host
		}
		if !trusted(candidate) {
			return candidate
		}
	}
	return host
}
