package reportmodel

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompactPageReferencesBoundedStorageAndRoundTrip(t *testing.T) {
	report := Report{Pages: make([]Page, 32)}
	for page := range report.Pages {
		report.Pages[page] = Page{URL: "https://example.com/" + strings.Repeat("x", 1750) + string(rune('a'+page))}
		for check := 0; check < 25; check++ {
			report.Findings = append(report.Findings, Finding{
				ID: "check", Pillar: "seo", Title: "A check", Status: "partial", URL: report.Pages[page].URL,
				Evidence: strings.Repeat("e", 300), Remediation: strings.Repeat("r", 160),
			})
		}
	}
	full, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := json.Marshal(CompactPageReferences(report))
	if err != nil {
		t.Fatal(err)
	}
	if len(full) < 850000 || len(compact) >= 850000 {
		t.Fatalf("unexpected serialized sizes: full=%d compact=%d", len(full), len(compact))
	}
	got, err := json.Marshal(ExpandPageReferences(CompactPageReferences(report)))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(full) {
		t.Fatal("compacted finding references did not round trip")
	}
}
