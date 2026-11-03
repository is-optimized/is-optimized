package reportmodel

import (
	"testing"
	"time"
)

func TestHistoryPreservesSamplesAndUnknownEvidence(t *testing.T) {
	before := Report{ID: "old", Domain: "example.com", CompletedAt: time.Now().Add(-time.Hour), Findings: []Finding{{ID: "title", URL: "https://example.com/", Status: "fail", Evidence: "missing"}, {ID: "gone", Status: "fail"}}}
	after := Report{ID: "new", Domain: "example.com", CompletedAt: time.Now(), Findings: []Finding{{ID: "title", URL: "https://example.com/", Status: "unknown", Evidence: "blocked"}}}
	got := WithHistory(after, before)
	if got.PreviousReportID != "old" || len(got.Changes) != 2 || got.Changes[0].ID != "gone" || got.Changes[0].Status != "" || got.Changes[1].Status != "unknown" {
		t.Fatalf("%+v", got)
	}
	if after.PreviousReportID != "" || before.Findings[0].Status != "fail" {
		t.Fatal("mutated input")
	}
	before.Domain = "other.com"
	if WithHistory(after, before).PreviousReportID != "" {
		t.Fatal("cross-domain history")
	}
}
