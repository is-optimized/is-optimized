package reportmodel

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestRepeatedDefinitionsRoundTripAndFitRecordLimit(t *testing.T) {
	report := Report{ID: "sample", Domain: "example.com"}
	for page := range 32 {
		url := fmt.Sprintf("https://example.com/page-%d", page)
		report.Pages = append(report.Pages, Page{URL: url, StatusCode: 200})
		for check := range 45 {
			report.Findings = append(report.Findings, Finding{ID: fmt.Sprintf("check-%d", check), Pillar: "seo", Title: "A descriptive title", Status: "partial", URL: url, Evidence: fmt.Sprintf("Page %d: observed some relevant signals", page), Remediation: strings.Repeat("Review and verify the relevant source. ", 5), Applicability: "Applies to observed page HTML.", Weight: 1, Tier: "recommended", Stage: "read", Foundational: true, Standards: []StandardReference{{ID: "HTML", URL: "https://html.spec.whatwg.org/", Coverage: "partial"}}})
		}
	}
	original, _ := json.Marshal(report)
	compact := CompactPageReferences(report)
	encoded, _ := json.Marshal(compact)
	if len(encoded) >= 800000 || len(encoded) >= len(original)/2 || len(compact.FindingDefinitions) != 45 {
		t.Fatalf("original=%d compact=%d definitions=%d", len(original), len(encoded), len(compact.FindingDefinitions))
	}
	var stored Report
	if err := json.Unmarshal(encoded, &stored); err != nil {
		t.Fatal(err)
	}
	expanded := ExpandPageReferences(stored)
	if !reflect.DeepEqual(expanded, report) {
		t.Fatal("expanded report changed evidence or metadata")
	}
	repeated, _ := json.Marshal(CompactPageReferences(compact))
	if string(repeated) != string(encoded) {
		t.Fatal("storage compaction is not idempotent")
	}
	after, _ := json.Marshal(report)
	if string(original) != string(after) {
		t.Fatal("mutated caller report")
	}
}
