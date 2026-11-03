package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
)

func TestLatestReportJSONUsesPublishedSnapshot(t *testing.T) {
	s, scan, sender := setup(t)
	completed(t, s)
	w := perform(s, "GET", "/api/v1/reports/EXAMPLE.COM", "")
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var report reportmodel.Report
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Domain != "example.com" || len(report.Findings) == 0 || scan.calls != 1 || sender.calls != 0 {
		t.Fatalf("unexpected report or side effects: %+v scans=%d texts=%d", report, scan.calls, sender.calls)
	}
	if w.Header().Get("X-Robots-Tag") != "noindex" {
		t.Fatal("snapshot indexing policy missing")
	}
	for _, domain := range []string{"unknown.example.com", "localhost"} {
		if got := perform(s, "GET", "/api/v1/reports/"+domain, ""); got.Code != 404 {
			t.Fatalf("%s: status=%d", domain, got.Code)
		}
	}
}
