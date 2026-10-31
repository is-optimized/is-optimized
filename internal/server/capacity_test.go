package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
)

func TestProjectedQueueWaitRequiresObservedSamples(t *testing.T) {
	q := Queue{Jobs: map[string]Job{"one": {State: "completed", Started: time.Unix(1, 0), Updated: time.Unix(2, 0)}}}
	if _, ok := projectedQueueWait(q); ok {
		t.Fatal("one scan must not be treated as a stable capacity estimate")
	}
}

func TestProjectedQueueWaitHonorsFiveMinuteLimit(t *testing.T) {
	q := Queue{Jobs: make(map[string]Job)}
	for i := 0; i < minQueueServiceSamples; i++ {
		started := time.Unix(int64(i*120), 0)
		q.Jobs[string(rune('a'+i))] = Job{State: "completed", Started: started, Updated: started.Add(60 * time.Second)}
	}
	for i := 0; i < 24; i++ {
		q.Jobs[string(rune('A'+i))] = Job{State: "queued"}
	}
	wait, ok := projectedQueueWait(q)
	if !ok || wait <= maxQueueWait {
		t.Fatalf("expected measured projected wait over five minutes, got %v, known=%v", wait, ok)
	}
}

func TestQueuedJobProgressReportsPositionAndMeasuredWait(t *testing.T) {
	q := Queue{Jobs: make(map[string]Job)}
	base := time.Unix(100, 0)
	for i := 0; i < minQueueServiceSamples; i++ {
		id := "done-" + strconv.Itoa(i)
		q.Jobs[id] = Job{ID: id, State: "completed", Started: base, Updated: base.Add(time.Duration(i+10) * time.Second)}
	}
	for i := 0; i < scanWorkers; i++ {
		id := "running-" + strconv.Itoa(i)
		q.Jobs[id] = Job{ID: id, State: "running"}
	}
	q.Jobs["queued-1"] = Job{ID: "queued-1", State: "queued", Created: base.Add(time.Second)}
	q.Jobs["queued-2"] = Job{ID: "queued-2", State: "queued", Created: base.Add(2 * time.Second)}
	if got := queuePosition(q, "queued-2"); got != 2 {
		t.Fatalf("queue position=%d; want 2", got)
	}
	wait, known := queuedJobWait(q, "queued-2")
	if !known || wait != 17*time.Second {
		t.Fatalf("queued wait=%v, known=%v; want 17s", wait, known)
	}
}

func TestQueueClaimUsesDisplayedFIFOOrderAndRecoversExpiredLeases(t *testing.T) {
	now := time.Unix(300, 0)
	q := Queue{Jobs: map[string]Job{
		"z-tie":   {ID: "z-tie", State: "queued", Created: now},
		"a-tie":   {ID: "a-tie", State: "queued", Created: now},
		"later":   {ID: "later", State: "queued", Created: now.Add(time.Second)},
		"expired": {ID: "expired", State: "running", Created: now.Add(-time.Second), LeaseUntil: now.Add(-time.Second), Attempts: 3},
	}}
	if got := queuePosition(q, "a-tie"); got != 1 {
		t.Fatalf("stable FIFO position=%d; want 1", got)
	}
	claimed := claimOldest(&q, now)
	if claimed.ID != "a-tie" || claimed.Attempts != 1 {
		t.Fatalf("claimed %q with %d attempts; want a-tie on first attempt", claimed.ID, claimed.Attempts)
	}
	if q.Jobs["expired"].State != "failed" {
		t.Fatal("expired job at retry limit was not marked failed")
	}
}

func TestScanStatusReturnsQueueProgress(t *testing.T) {
	s, _, _ := setup(t)
	q := Queue{Jobs: make(map[string]Job)}
	base := time.Unix(200, 0)
	for i := 0; i < minQueueServiceSamples; i++ {
		id := "done-" + strconv.Itoa(i)
		q.Jobs[id] = Job{ID: id, State: "completed", Started: base, Updated: base.Add(time.Duration(i+10) * time.Second)}
	}
	for i := 0; i < scanWorkers; i++ {
		id := "running-" + strconv.Itoa(i)
		q.Jobs[id] = Job{ID: id, State: "running"}
	}
	q.Jobs["queued-1"] = Job{ID: "queued-1", State: "queued", Created: base.Add(time.Second)}
	q.Jobs["queued-2"] = Job{ID: "queued-2", State: "queued", Created: base.Add(2 * time.Second)}
	if err := s.db.Update(context.Background(), "queue", func(json.RawMessage) (any, error) { return q, nil }); err != nil {
		t.Fatal(err)
	}
	w := perform(s, http.MethodGet, "/api/v1/scans/queued-2", "")
	var status struct {
		State         string `json:"state"`
		Position      int    `json:"position"`
		EstimatedWait int    `json:"estimated_wait_seconds"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &status) != nil || status.State != "queued" || status.Position != 2 || status.EstimatedWait != 17 {
		t.Fatalf("unexpected queued status response: %d %s", w.Code, w.Body.String())
	}
}

func TestReportStateMarksIncompleteSamplesPartial(t *testing.T) {
	if got := reportState(reportmodel.Report{Pages: []reportmodel.Page{{StatusCode: 200}}}); got != "completed" {
		t.Fatalf("complete report state=%q", got)
	}
	if got := reportState(reportmodel.Report{Pages: []reportmodel.Page{{StatusCode: 200}, {StatusCode: 404}}}); got != "partial" {
		t.Fatalf("partial report state=%q", got)
	}
}

func TestRateLimitReturnsWindowBasedRetryHint(t *testing.T) {
	now := time.Now()
	s := &Server{rates: map[string][]time.Time{"192.0.2.1": {now.Add(-9 * time.Minute)}}}
	req := httptest.NewRequest("POST", "/api/v1/scans", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	for i := 0; i < 4; i++ {
		if ok, _ := s.allow(req); !ok {
			t.Fatal("request was limited before reaching the configured threshold")
		}
	}
	if ok, retry := s.allow(req); ok || retry < 59 || retry > 60 {
		t.Fatalf("expected retry after approximately 60 seconds, got allowed=%v retry=%d", ok, retry)
	}
}
