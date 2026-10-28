package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"github.com/is-optimized/is-optimized/internal/store"
)

type directoryStore struct {
	store.Store
	reads              []string
	failDirectoryRead  bool
	failDirectoryWrite bool
}

func (d *directoryStore) Get(ctx context.Context, key string, out any) error {
	d.reads = append(d.reads, key)
	if key == directoryKey && d.failDirectoryRead {
		return errors.New("fixture unavailable")
	}
	return d.Store.Get(ctx, key, out)
}
func (d *directoryStore) List(context.Context, string) ([]json.RawMessage, error) {
	return nil, errors.New("unbounded history listing is forbidden")
}
func (d *directoryStore) Update(ctx context.Context, key string, f func(json.RawMessage) (any, error)) error {
	if key == directoryKey && d.failDirectoryWrite {
		return errors.New("fixture unavailable")
	}
	return d.Store.Update(ctx, key, f)
}

func savePublicFixture(t *testing.T, s *Server, n int, completed time.Time) Snapshot {
	t.Helper()
	snap := Snapshot{Report: reportmodel.Report{ID: fmt.Sprintf("%036x", n+1), Domain: fmt.Sprintf("site-%02d.example.com", n), CompletedAt: completed}, Expires: time.Now().Add(time.Hour), Artifact: fmt.Sprintf("fixture-%d", n)}
	if err := s.db.Update(context.Background(), "report_"+snap.Report.ID, func(json.RawMessage) (any, error) { return snap, nil }); err != nil {
		t.Fatal(err)
	}
	return snap
}

func TestRecentDirectoryBoundedAndOrdered(t *testing.T) {
	s, _, _ := setup(t)
	now := time.Now().Add(-time.Minute)
	for i := 0; i < 40; i++ {
		snap := savePublicFixture(t, s, i, now.Add(-time.Duration(i)*time.Minute))
		if err := s.indexPublicReport(context.Background(), snap); err != nil {
			t.Fatal(err)
		}
	}
	var stored publicDirectory
	if err := s.db.Get(context.Background(), directoryKey, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Sites) != directoryCapacity {
		t.Fatal("projection exceeded its fixed capacity", len(stored.Sites))
	}
	counted := &directoryStore{Store: s.db}
	s.db = counted
	recent, err := s.recentPublicReports(context.Background())
	if err != nil || len(recent) != recentLimit {
		t.Fatal(err, len(recent))
	}
	for i, site := range recent {
		if site.Domain != fmt.Sprintf("site-%02d.example.com", i) {
			t.Fatal("not newest first", site.Domain)
		}
	}
	if len(counted.reads) > 1+directoryCapacity {
		t.Fatal("read budget exceeded", len(counted.reads))
	}
	for _, key := range counted.reads {
		if key != directoryKey && !strings.HasPrefix(key, "report_") {
			t.Fatal("read nonpublic record", key)
		}
	}
}

func TestRecentDirectoryExcludesExpiredUnpublishedAndMismatched(t *testing.T) {
	s, _, _ := setup(t)
	ctx := context.Background()
	now := time.Now().Add(-time.Minute)
	good := savePublicFixture(t, s, 0, now.Add(-time.Hour))
	expired := savePublicFixture(t, s, 1, now)
	expired.Expires = time.Now().Add(-time.Second)
	s.db.Update(ctx, "report_"+expired.Report.ID, func(json.RawMessage) (any, error) { return expired, nil })
	attempt := Snapshot{Report: reportmodel.Report{ID: fmt.Sprintf("%036x", 99), Domain: "attempt.example.com", CompletedAt: now}, Expires: time.Now().Add(time.Hour), Artifact: "attempt-only"}
	s.db.Update(ctx, "attempt_"+attempt.Artifact, func(json.RawMessage) (any, error) { return attempt, nil })
	mismatch := savePublicFixture(t, s, 2, now)
	sites := []publicSite{
		{ID: expired.Report.ID, Domain: expired.Report.Domain, CompletedAt: expired.Report.CompletedAt, Expires: time.Now().Add(time.Hour)},
		{ID: attempt.Report.ID, Domain: attempt.Report.Domain, CompletedAt: attempt.Report.CompletedAt, Expires: attempt.Expires},
		{ID: mismatch.Report.ID, Domain: "wrong.example.com", CompletedAt: mismatch.Report.CompletedAt, Expires: mismatch.Expires},
		{ID: good.Report.ID, Domain: good.Report.Domain, CompletedAt: good.Report.CompletedAt, Expires: good.Expires},
		{ID: "../request_private", Domain: "private.example.com", CompletedAt: now, Expires: time.Now().Add(time.Hour)},
	}
	s.db.Update(ctx, directoryKey, func(json.RawMessage) (any, error) { return publicDirectory{Sites: sites}, nil })
	counted := &directoryStore{Store: s.db}
	s.db = counted
	recent, err := s.recentPublicReports(ctx)
	if err != nil || len(recent) != 1 || recent[0].ID != good.Report.ID {
		t.Fatal("unpublished/expired/mismatched report leaked", err, recent)
	}
	for _, key := range counted.reads {
		if key != directoryKey && !publicReportID.MatchString(strings.TrimPrefix(key, "report_")) {
			t.Fatal("nonpublic read", key)
		}
	}
}

func TestRecentDirectoryKeepsNewestPerDomain(t *testing.T) {
	s, _, _ := setup(t)
	ctx := context.Background()
	now := time.Now().Add(-time.Minute)
	newer := savePublicFixture(t, s, 0, now)
	older := savePublicFixture(t, s, 1, now.Add(-time.Hour))
	older.Report.Domain = newer.Report.Domain
	s.db.Update(ctx, "report_"+older.Report.ID, func(json.RawMessage) (any, error) { return older, nil })
	for _, snap := range []Snapshot{newer, older, newer} {
		if err := s.indexPublicReport(ctx, snap); err != nil {
			t.Fatal(err)
		}
	}
	recent, err := s.recentPublicReports(ctx)
	if err != nil || len(recent) != 1 || recent[0].ID != newer.Report.ID {
		t.Fatal("older/replayed publication replaced newest", err, recent)
	}
}

func TestLandingDirectoryAndDedicatedReport(t *testing.T) {
	s, _, _ := setup(t)
	s.cfg.SMS = false
	empty := perform(s, "GET", "/", "")
	if empty.Code != 200 || !strings.Contains(empty.Body.String(), "No recent public reports listed yet") {
		t.Fatal("missing empty state")
	}
	key := completed(t, s)
	home := perform(s, "GET", "/", "")
	if home.Code != 200 || !strings.Contains(home.Body.String(), `href="/example.com"`) || strings.Contains(home.Body.String(), `class="scores"`) {
		t.Fatal("landing must list domain reports without inline score report")
	}
	for _, path := range []string{"/example.com", "/reports/" + key, "/reports/" + key + "/pdf"} {
		if w := perform(s, "GET", path, ""); w.Code != 200 {
			t.Fatal("report unavailable without SMS", path, w.Code)
		}
	}
	counted := &directoryStore{Store: s.db, failDirectoryRead: true}
	s.db = counted
	unavailable := perform(s, "GET", "/", "")
	if unavailable.Code != 200 || !strings.Contains(unavailable.Body.String(), "Recent reports are temporarily unavailable") || strings.Contains(unavailable.Body.String(), "fixture unavailable") {
		t.Fatal("directory failure not isolated from landing")
	}
}

func TestDirectoryFailureDoesNotBlockReportPublication(t *testing.T) {
	s, scan, _ := setup(t)
	s.db = &directoryStore{Store: s.db, failDirectoryWrite: true}
	key := completed(t, s)
	var queue Queue
	if err := s.db.Get(context.Background(), "queue", &queue); err != nil {
		t.Fatal(err)
	}
	if queue.Jobs[key].State != "completed" || scan.calls != 1 {
		t.Fatal("directory write blocked scan completion", queue.Jobs[key].State, scan.calls)
	}
	for _, path := range []string{"/example.com", "/reports/" + key, "/reports/" + key + "/pdf"} {
		if w := perform(s, "GET", path, ""); w.Code != 200 {
			t.Fatal("directory failure hid report", path, w.Code)
		}
	}
}
