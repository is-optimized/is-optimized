package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"github.com/is-optimized/is-optimized/internal/store"
)

func awaitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatcher did not respond promptly")
	}
}

func TestDispatcherIdleWakeRecoveryAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wakes, calls, done := make(chan struct{}, 1), make(chan struct{}, 10), make(chan struct{})
	go func() {
		defer close(done)
		runDispatcher(ctx, time.Hour, wakes, func(context.Context) { calls <- struct{}{} })
	}()
	awaitSignal(t, calls) // startup recovery
	select {
	case <-calls:
		t.Fatal("idle loop polled before recovery interval")
	case <-time.After(50 * time.Millisecond):
	}
	wake(wakes)
	awaitSignal(t, calls)
	cancel()
	awaitSignal(t, done)

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	done = make(chan struct{})
	go func() {
		defer close(done)
		runDispatcher(ctx, 20*time.Millisecond, nil, func(context.Context) { calls <- struct{}{} })
	}()
	awaitSignal(t, calls)
	awaitSignal(t, calls) // persisted work is checked even with no local wakeup
	cancel()
	awaitSignal(t, done)
}

func TestIdleDispatchDoesNotPersistQueueAgain(t *testing.T) {
	s, _, _ := setup(t)
	dir := t.TempDir()
	db, err := store.NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.db = db
	s.dispatch(context.Background())
	before, err := os.Stat(filepath.Join(dir, "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		s.recoverPublishing(context.Background())
		s.dispatch(context.Background())
	}
	after, err := os.Stat(filepath.Join(dir, "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("idle dispatch rewrote queue")
	}
}

type gatedScanner struct {
	started chan struct{}
	release chan struct{}
}

func (g *gatedScanner) Scan(ctx context.Context, domain string) (reportmodel.Report, error) {
	g.started <- struct{}{}
	select {
	case <-g.release:
	case <-ctx.Done():
		return reportmodel.Report{}, ctx.Err()
	}
	return (&fakeScanner{domain: domain}).Scan(ctx, domain)
}

func startDispatcher(t *testing.T, s *Server, interval time.Duration) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.run(ctx, interval) }()
	return func() { cancel(); awaitSignal(t, done) }
}

func TestNewScansAndFreedSlotsWakeWithoutPolling(t *testing.T) {
	s, _, _ := setup(t)
	g := &gatedScanner{started: make(chan struct{}, 5), release: make(chan struct{}, 1)}
	s.scan = g
	stop := startDispatcher(t, s, time.Hour)
	defer stop()
	for i := range 5 {
		w := perform(s, "POST", "/api/v1/scans", fmt.Sprintf(`{"domain":"site%d.example.com"}`, i))
		if w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for range 4 {
		awaitSignal(t, g.started)
	}
	select {
	case <-g.started:
		t.Fatal("worker bound exceeded")
	default:
	}
	g.release <- struct{}{}
	awaitSignal(t, g.started) // completion wakes fifth job, without a timer tick
	close(g.release)
}

func TestPeriodicRecoveryClaimsExpiredLease(t *testing.T) {
	s, _, _ := setup(t)
	g := &gatedScanner{started: make(chan struct{}, 1), release: make(chan struct{})}
	s.scan = g
	stop := startDispatcher(t, s, 20*time.Millisecond)
	defer stop()
	j := Job{ID: id(), Domain: "example.com", State: "running", Lease: "stale", LeaseUntil: time.Now().Add(-time.Second)}
	err := s.db.Update(context.Background(), "queue", func(json.RawMessage) (any, error) { return Queue{Jobs: map[string]Job{j.ID: j}}, nil })
	if err != nil {
		t.Fatal(err)
	}
	awaitSignal(t, g.started)
	close(g.release)
}

func TestInboundWakesMessagesWithoutPolling(t *testing.T) {
	s, _, sender := setup(t)
	key := completed(t, s)
	token := requestToken(t, s, key)
	stop := startDispatcher(t, s, time.Hour)
	form := url.Values{"AccountSid": {"ACtest"}, "From": {"+15555550123"}, "To": {s.cfg.TwilioFrom}, "MessageSid": {"SMinbound"}, "Body": {"REPORT " + token}}
	if w := signed(s, "/webhooks/twilio/inbound", form); w.Code != 200 {
		stop()
		t.Fatal(w.Code)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		var req Request
		err := s.db.Get(context.Background(), "request_"+token, &req)
		if err == nil && req.State == "accepted" {
			break
		}
		if time.Now().After(deadline) {
			stop()
			t.Fatal("inbound did not wake message dispatcher", req.State, err)
		}
		time.Sleep(time.Millisecond)
	}
	stop()
	if sender.calls != 1 {
		t.Fatal("unexpected send count", sender.calls)
	}
}

func TestPeriodicRecoveryFinishesPublicationAndExpiresSending(t *testing.T) {
	s, _, sender := setup(t)
	key := completed(t, s)
	// Simulate a crash after writing the attempt but before terminal publication.
	err := s.db.Update(context.Background(), "queue", func(raw json.RawMessage) (any, error) {
		var q Queue
		if err := json.Unmarshal(raw, &q); err != nil {
			return nil, err
		}
		j := q.Jobs[key]
		j.State = "publishing"
		q.Jobs[key] = j
		return q, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	req := Request{Token: id(), State: "sending", Updated: time.Now().Add(-2 * time.Minute)}
	if err := s.db.Update(context.Background(), "request_"+req.Token, func(json.RawMessage) (any, error) { return req, nil }); err != nil {
		t.Fatal(err)
	}
	stop := startDispatcher(t, s, 20*time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for {
		var q Queue
		var current Request
		qErr := s.db.Get(context.Background(), "queue", &q)
		rErr := s.db.Get(context.Background(), "request_"+req.Token, &current)
		if qErr == nil && rErr == nil && q.Jobs[key].State == "completed" && current.State == "outcome-unknown" {
			break
		}
		if time.Now().After(deadline) {
			stop()
			t.Fatal("persisted recovery did not complete", qErr, rErr, current.State)
		}
		time.Sleep(time.Millisecond)
	}
	stop()
	if sender.calls != 0 {
		t.Fatal("ambiguous sending request was resent")
	}
}
