package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
)

func TestSummaryPreservesZeroMissingAndUnscoredEvidence(t *testing.T) {
	zero := 0.0
	r := reportmodel.Report{Pillars: []reportmodel.Pillar{{ID: "seo", Score: &zero, Coverage: 1}, {ID: "geo", Coverage: .4}}}
	for _, status := range []string{"pass", "partial", "fail", "unknown", "not_applicable", "information", "future_status"} {
		r.Findings = append(r.Findings, reportmodel.Finding{ID: status, Pillar: "seo", Status: status})
	}
	r.Findings = append(r.Findings, reportmodel.Finding{ID: "missing-pillar-evidence", Pillar: "agent", Status: "unknown"})
	before, _ := json.Marshal(r)
	v := summarize(r)
	if len(v.Pillars) != 3 || v.Pillars[0].Score == nil || *v.Pillars[0].Score != 0 || v.Pillars[0].Missing {
		t.Fatal("zero was replaced by missing evidence")
	}
	if v.Pillars[1].Missing || v.Pillars[1].Score != nil || v.Pillars[1].Coverage != .4 {
		t.Fatal("withheld score lost its recorded coverage")
	}
	if !v.Pillars[2].Missing || v.Pillars[2].Score != nil || len(v.Pillars[2].Findings) != 1 {
		t.Fatal("missing pillar lost its recorded findings")
	}
	if v.Counts.Total != 8 || v.Counts.Unknown != 2 || v.Counts.Other != 1 || v.Counts.NotApplicable != 1 || v.Counts.Information != 1 {
		t.Fatal("unobserved or excluded evidence disappeared", v.Counts)
	}
	for _, p := range v.Pillars {
		count := 0
		for _, segment := range p.Segments {
			count += segment.Count
		}
		if count != p.Counts.Total {
			t.Fatal("bar does not represent all recorded statuses")
		}
	}
	after, _ := json.Marshal(r)
	if !bytes.Equal(before, after) {
		t.Fatal("summary mutated saved evidence")
	}
	var out bytes.Buffer
	if err := HTML(&out, r); err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{`>0 <small>/ 100</small>`, `Insufficient evidence`, `Pillar result not recorded`, `40%`, `unclassified`} {
		if !strings.Contains(out.String(), wanted) {
			t.Fatal("missing distinction", wanted)
		}
	}
}

func TestPresentationPriorityUsesOnlyRecordedStatusAndFoundation(t *testing.T) {
	r := reportmodel.Report{Findings: []reportmodel.Finding{
		{ID: "partial-normal", Status: "partial"}, {ID: "fail-normal", Status: "fail"},
		{ID: "partial-foundation", Status: "partial", Foundational: true}, {ID: "fail-foundation", Status: "fail", Foundational: true},
		{ID: "unknown", Status: "unknown", Foundational: true}, {ID: "pass", Status: "pass"},
	}}
	v := summarize(r)
	want := []string{"fail-foundation", "fail-normal", "partial-foundation", "partial-normal"}
	if len(v.Fixes) != len(want) {
		t.Fatal("unknown/pass promoted into fixes")
	}
	for i, id := range want {
		if v.Fixes[i].ID != id {
			t.Fatal("priority mismatch", i, v.Fixes[i].ID)
		}
	}
	if r.Findings[0].ID != "partial-normal" {
		t.Fatal("saved ordering changed")
	}
}

func TestEmptyReportDoesNotImplyReadinessAndLinksRemainEscaped(t *testing.T) {
	r := reportmodel.Report{}
	v := summarize(r)
	if len(v.Pillars) != 3 || !strings.Contains(v.Narrative, "does not establish") {
		t.Fatal("empty report claims readiness")
	}
	for _, p := range v.Pillars {
		if !p.Missing || p.Score != nil || len(p.Segments) != 0 {
			t.Fatal("empty report manufactured evidence")
		}
	}
	r.Findings = []reportmodel.Finding{{Pillar: "seo", Status: "fail", Title: "Untrusted evidence", URL: "javascript:alert(1)", Evidence: "<img src=x onerror=alert(1)>", Remediation: "Inspect the source"}}
	var out bytes.Buffer
	if err := HTML(&out, r); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), `href="javascript:`) || strings.Contains(out.String(), `<img src=x`) || !strings.Contains(out.String(), "&lt;img") {
		t.Fatal("new affected-page links/evidence bypassed escaping")
	}
}
