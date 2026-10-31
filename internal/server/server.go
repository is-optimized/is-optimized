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
	"runtime"
	"sort"
	"strconv"
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
	ID, Domain, State, Lease, Error       string
	Created, Started, Updated, LeaseUntil time.Time
	Attempts                              int
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
var errAdmissionFull = errors.New("scan admission limit reached")

const (
	scanWorkers            = 4
	maxQueuedJobs          = 64 // Temporary storage guard; load testing must replace this with a measured limit.
	minQueueServiceSamples = 8
	queueServiceSamples    = 16
	maxQueueWait           = 5 * time.Minute
)

func reportState(r reportmodel.Report) string {
	for _, page := range r.Pages {
		if page.StatusCode != http.StatusOK || page.Error != "" {
			return "partial"
		}
	}
	return "completed"
}

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
	s := &Server{db: db, scan: scan, sender: sender, cfg: cfg, mux: http.NewServeMux(), slots: make(chan struct{}, scanWorkers), rates: map[string][]time.Time{}}
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
func (s *Server) allow(r *http.Request) (bool, int) {
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
		retryAfter := int((a[0].Add(10*time.Minute).Sub(now) + time.Second - 1) / time.Second)
		if retryAfter < 1 {
			retryAfter = 1
		}
		return false, retryAfter
	}
	if len(s.rates) >= 10000 {
		for k, v := range s.rates {
			if len(v) == 0 || now.Sub(v[len(v)-1]) > 10*time.Minute {
				delete(s.rates, k)
			}
		}
		if len(s.rates) >= 10000 {
			return false, 600
		}
	}
	s.rates[host] = append(a, now)
	return true, 0
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
	allowed, rateRetryAfter := s.allow(r)
	if !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(rateRetryAfter))
		slog.Warn("scan request rate limited", "retry_after_seconds", rateRetryAfter)
		http.Error(w, "Scan limit reached; please retry in about "+strconv.Itoa(rateRetryAfter)+" seconds.", 429)
		return
	}
	var latest struct{ ID string }
	if s.db.Get(r.Context(), "latest_"+hash(domain), &latest) == nil {
		var snap Snapshot
		if s.db.Get(r.Context(), "report_"+latest.ID, &snap) == nil && time.Since(snap.Report.CompletedAt) < 24*time.Hour && snap.Report.MethodologyVersion == scanner.MethodologyVersion {
			snap.Report = reportmodel.ExpandPageReferences(snap.Report)
			jsonOut(w, 200, map[string]any{"id": latest.ID, "state": reportState(snap.Report), "report": snap.Report})
			return
		}
	}
	var job Job
	retryAfter := 0
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
			if (j.State == "completed" || j.State == "partial" || j.State == "failed") && now.Sub(j.Updated) > time.Hour {
				delete(q.Jobs, k)
			}
		}
		// Terminal history must not consume the bounded active queue.
		if estimate, known := projectedQueueWait(q); known && estimate > maxQueueWait {
			retryAfter = int((estimate + time.Second - 1) / time.Second)
			return nil, errAdmissionFull
		}
		for len(q.Jobs) >= maxQueuedJobs {
			oldest := ""
			var at time.Time
			for k, j := range q.Jobs {
				if j.State == "completed" || j.State == "partial" || j.State == "failed" {
					if oldest == "" || j.Updated.Before(at) {
						oldest = k
						at = j.Updated
					}
				}
			}
			if oldest == "" {
				return nil, errAdmissionFull
			}
			delete(q.Jobs, oldest)
		}
		job = Job{ID: id(), Domain: domain, State: "queued", Created: now, Updated: now}
		q.Jobs[job.ID] = job
		return q, nil
	})
	if e != nil {
		if retryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		}
		if errors.Is(e, errAdmissionFull) {
			message := "We’re busy completing scans; please try again in a few minutes."
			if retryAfter > 0 {
				message = "We’re busy completing scans; please try again in about " + strconv.Itoa(retryAfter) + " seconds."
			}
			http.Error(w, message, http.StatusServiceUnavailable)
			slog.Warn("scan admission unavailable", "reason", "measured_wait_or_storage_guard")
		} else {
			http.Error(w, "We couldn’t save your scan request. Please try again shortly.", http.StatusServiceUnavailable)
			slog.Error("scan admission unavailable", "reason", "durable_store_unavailable")
		}
		return
	}
	slog.Info("scan admitted", "state", job.State)
	wake(s.scanWake)
	jsonOut(w, 202, map[string]string{"id": job.ID, "state": job.State})
}

// projectedQueueWait is unknown until there are enough completed service-time
// observations. Before then, admission uses only the provisional durable-job
// guard; it does not guess instance capacity from the worker count.
func projectedQueueWait(q Queue) (time.Duration, bool) {
	var running, queued int
	var completed []Job
	for _, j := range q.Jobs {
		switch j.State {
		case "running", "publishing":
			running++
		case "queued":
			queued++
		case "completed", "partial":
			if !j.Started.IsZero() && j.Updated.After(j.Started) {
				completed = append(completed, j)
			}
		}
	}
	service, known := queueServiceTime(completed)
	if !known {
		return 0, false
	}
	freeWorkers := scanWorkers - running
	if freeWorkers < 0 {
		freeWorkers = 0
	}
	waves := queued + 1 - freeWorkers
	if waves <= 0 {
		return 0, true
	}
	return time.Duration((waves+scanWorkers-1)/scanWorkers) * service, true
}

func queueServiceTime(completed []Job) (time.Duration, bool) {
	if len(completed) < minQueueServiceSamples {
		return 0, false
	}
	sort.Slice(completed, func(i, j int) bool { return completed[i].Updated.Before(completed[j].Updated) })
	if len(completed) > queueServiceSamples {
		completed = completed[len(completed)-queueServiceSamples:]
	}
	samples := make([]time.Duration, 0, len(completed))
	for _, j := range completed {
		samples = append(samples, j.Updated.Sub(j.Started))
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	index := (len(samples)*9+9)/10 - 1 // nearest-rank p90
	service := samples[index]
	if service < time.Second {
		service = time.Second
	}
	return service, true
}

func sortJobsFIFO(jobs []Job) {
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].Created.Equal(jobs[j].Created) {
			return jobs[i].ID < jobs[j].ID
		}
		return jobs[i].Created.Before(jobs[j].Created)
	})
}

// claimOldest uses the same FIFO order that the status endpoint reports.
// Expired running leases remain eligible at their original queue position.
func claimOldest(q *Queue, now time.Time) Job {
	candidates := make([]Job, 0, len(q.Jobs))
	for _, j := range q.Jobs {
		if j.State == "queued" || (j.State == "running" && now.After(j.LeaseUntil)) {
			candidates = append(candidates, j)
		}
	}
	sortJobsFIFO(candidates)
	for _, candidate := range candidates {
		j := q.Jobs[candidate.ID]
		if j.Attempts >= 3 {
			j.State = "failed"
			j.Error = "scan interrupted repeatedly"
			j.Updated = now
			q.Jobs[candidate.ID] = j
			continue
		}
		j.State = "running"
		if j.Started.IsZero() {
			j.Started = now
		}
		j.Lease = id()
		j.LeaseUntil = now.Add(2 * time.Minute)
		j.Attempts++
		j.Updated = now
		q.Jobs[candidate.ID] = j
		return j
	}
	return Job{}
}

func queuedJobWait(q Queue, key string) (time.Duration, bool) {
	job, ok := q.Jobs[key]
	if !ok || job.State != "queued" {
		return 0, false
	}
	queued := make([]Job, 0)
	running, completed := 0, make([]Job, 0)
	for _, candidate := range q.Jobs {
		switch candidate.State {
		case "queued":
			queued = append(queued, candidate)
		case "running", "publishing":
			running++
		case "completed", "partial":
			if !candidate.Started.IsZero() && candidate.Updated.After(candidate.Started) {
				completed = append(completed, candidate)
			}
		}
	}
	sortJobsFIFO(queued)
	position := 0
	for i, candidate := range queued {
		if candidate.ID == key {
			position = i + 1
			break
		}
	}
	if position == 0 {
		return 0, false
	}
	service, known := queueServiceTime(completed)
	if !known {
		return 0, false
	}
	freeWorkers := scanWorkers - running
	if freeWorkers < 0 {
		freeWorkers = 0
	}
	waves := position - freeWorkers
	if waves <= 0 {
		return 0, true
	}
	return time.Duration((waves+scanWorkers-1)/scanWorkers) * service, true
}

func queuePosition(q Queue, key string) int {
	queued := make([]Job, 0)
	for _, candidate := range q.Jobs {
		if candidate.State == "queued" {
			queued = append(queued, candidate)
		}
	}
	sortJobsFIFO(queued)
	for i, candidate := range queued {
		if candidate.ID == key {
			return i + 1
		}
	}
	return 0
}

func (s *Server) scanStatus(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("id")
	if !store.KeyOK(key) {
		http.NotFound(w, r)
		return
	}
	var snap Snapshot
	if s.db.Get(r.Context(), "report_"+key, &snap) == nil && time.Now().Before(snap.Expires) {
		snap.Report = reportmodel.ExpandPageReferences(snap.Report)
		jsonOut(w, 200, map[string]any{"id": key, "state": reportState(snap.Report), "report": snap.Report})
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
	status := map[string]any{"id": j.ID, "state": j.State, "error": j.Error}
	if j.State == "queued" {
		status["position"] = queuePosition(q, key)
		if wait, known := queuedJobWait(q, key); known {
			status["estimated_wait_seconds"] = int((wait + time.Second - 1) / time.Second)
		}
	}
	jsonOut(w, 200, status)
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
	metricsDone := make(chan struct{})
	go func() {
		defer close(metricsDone)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.logRuntimeMetrics(ctx)
			}
		}
	}()
	runDispatcher(ctx, recoveryInterval, s.scanWake, func(ctx context.Context) {
		s.recoverPublishing(ctx)
		s.dispatch(ctx)
	})
	s.wg.Wait()
	<-messageDone
	<-metricsDone
}

// logRuntimeMetrics uses bounded dimensions only; identifiers, phone numbers,
// client addresses, and target domains are deliberately excluded.
func (s *Server) logRuntimeMetrics(ctx context.Context) {
	var q Queue
	if err := s.db.Get(ctx, "queue", &q); err != nil && !errors.Is(err, store.ErrNotFound) {
		slog.Warn("runtime_metrics", "store_available", false)
		return
	}
	counts := map[string]int{"queued": 0, "running": 0, "publishing": 0, "completed": 0, "partial": 0, "failed": 0}
	oldestAge := time.Duration(0)
	now := time.Now().UTC()
	for _, j := range q.Jobs {
		if _, ok := counts[j.State]; ok {
			counts[j.State]++
		}
		if j.State == "queued" {
			age := now.Sub(j.Created)
			if age > oldestAge {
				oldestAge = age
			}
		}
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	slog.Info("runtime_metrics", "store_available", true,
		"queued_jobs", counts["queued"], "running_jobs", counts["running"],
		"publishing_jobs", counts["publishing"], "completed_jobs_retained", counts["completed"],
		"partial_jobs_retained", counts["partial"], "failed_jobs_retained", counts["failed"],
		"oldest_queued_age_ms", oldestAge.Milliseconds(), "active_workers", len(s.slots), "worker_limit", cap(s.slots),
		"heap_alloc_bytes", memory.HeapAlloc, "heap_objects", memory.HeapObjects, "gc_cycles", memory.NumGC, "goroutines", runtime.NumGoroutine())
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
	for i := 0; i < scanWorkers; i++ {
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
			claimed = claimOldest(&q, now)
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
	started := time.Now()
	slog.Info("scan worker started", "queue_wait_ms", started.Sub(j.Created).Milliseconds(), "attempt", j.Attempts)
	// Leave time after the scanner's 60-second budget to persist partial
	// evidence, render the artifact, and publish a terminal queue state.
	ctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	// Scan evidence survives rendering/storage failures. A retry only rerenders.
	var rep reportmodel.Report
	var stored reportmodel.Report
	e := s.db.Get(ctx, "evidence_"+j.ID, &stored)
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
					rep = reportmodel.ExpandPageReferences(prior)
					return prior, nil
				}
				return reportmodel.CompactPageReferences(rep), nil
			})
		}
	} else if e == nil {
		rep = reportmodel.ExpandPageReferences(stored)
	}
	artifact := j.ID + "_" + j.Lease
	if e == nil {
		pdfStarted := time.Now()
		var b []byte
		b, e = render.PDF(rep)
		slog.Info("pdf_render_metrics", "duration_ms", time.Since(pdfStarted).Milliseconds(), "artifact_bytes", len(b), "success", e == nil)
		if e == nil {
			e = s.db.PutPDF(ctx, artifact, b)
		}
	}
	snap := Snapshot{Report: reportmodel.CompactPageReferences(rep), Expires: time.Now().Add(30 * 24 * time.Hour), Artifact: artifact}
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
	pageOK, pageError := 0, 0
	for _, page := range rep.Pages {
		if page.StatusCode == http.StatusOK && page.Error == "" {
			pageOK++
		} else {
			pageError++
		}
	}
	slog.Info("scan worker finished", "duration_ms", time.Since(started).Milliseconds(), "pages_selected", len(rep.Pages), "pages_successful", pageOK, "pages_with_error", pageError, "result_available", err == nil && published)
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
		cur.State = reportState(snap.Report)
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
	snap.Report = reportmodel.ExpandPageReferences(snap.Report)
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
