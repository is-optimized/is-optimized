package render

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
)

func designFixture() reportmodel.Report {
	seo, geo := 75.0, 50.0
	return reportmodel.Report{ID: "design-fixture-20261027", Domain: "example.com", CompletedAt: time.Date(2026, 10, 27, 19, 0, 0, 0, time.UTC), MethodologyVersion: "fixture-only", SelectionPolicy: "homepage-direct-links-v1", Pages: []reportmodel.Page{{URL: "https://example.com", StatusCode: 200}, {URL: "https://example.com/about", StatusCode: 200}, {URL: "https://example.com/contact", Error: "Timed out; not observed"}}, Pillars: []reportmodel.Pillar{{ID: "seo", Name: "Search engine readiness", Score: &seo, Coverage: 1}, {ID: "geo", Name: "Generative search readiness", Score: &geo, Coverage: .85}, {ID: "agent", Name: "AI-agent readiness", Coverage: .6}}, Findings: []reportmodel.Finding{
		{ID: "pass", Pillar: "seo", Status: "pass", Title: "A descriptive page title", URL: "https://example.com", Evidence: "Fixture title is readable in the initial HTML.", Remediation: "Keep the title specific to this page."},
		{ID: "fix", Pillar: "seo", Status: "fail", Title: "Give each page a useful description", URL: "https://example.com/about", Evidence: "Fixture page has no meta description. Quoted content: <script>alert(1)</script>", Remediation: "Write a concise, accurate description of this page. Confirm the description is present in the initial HTML."},
		{ID: "partial", Pillar: "geo", Status: "partial", Title: "Make the business identity explicit", URL: "https://example.com", Evidence: "Fixture structured data identifies a business but lacks a verified website reference.", Remediation: "Review the existing structured data against the visible business information. Add only facts you can verify."},
		{ID: "unknown", Pillar: "agent", Status: "unknown", Title: "Contact-page controls could not be observed", URL: "https://example.com/contact", Evidence: "The fixture request timed out. No score penalty was invented.", Remediation: "Check public access and run a new scan after the page is available."},
	}}
}

func TestRenderImmutableEvidence(t *testing.T) {
	r := designFixture()
	before, _ := json.Marshal(r)
	var html bytes.Buffer
	if err := HTML(&html, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html.String(), "&lt;script&gt;") || strings.Contains(html.String(), "<script>alert") {
		t.Fatal("unescaped evidence")
	}
	if strings.Index(html.String(), "Give each page") > strings.Index(html.String(), "A descriptive page title") {
		t.Fatal("fixes not prioritized")
	}
	for _, expected := range []string{r.ID, "fixture-only", "Insufficient evidence", "https://sire.run/privacy/", "https://sire.run/terms/", "privacy@sire.run"} {
		if !strings.Contains(html.String(), expected) {
			t.Fatal("missing report content", expected)
		}
	}
	pdf, err := PDF(r)
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatal("PDF generation", err)
	}
	after, _ := json.Marshal(r)
	if !bytes.Equal(before, after) {
		t.Fatal("render changed immutable evidence")
	}
	// Explicit opt-in artifact export for local design review; no real customer data.
	if dir := os.Getenv("DESIGN_ARTIFACT_DIR"); dir != "" {
		if err = os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for name, data := range map[string][]byte{"fixture-report.html": html.Bytes(), "fixture-report.pdf": pdf, "fixture-evidence.json": before} {
			if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestLongReportPagination(t *testing.T) {
	r := designFixture()
	for i := 0; i < 25; i++ {
		r.Findings = append(r.Findings, reportmodel.Finding{ID: "long", Title: "A long finding for pagination", Status: "unknown", URL: "https://example.com/" + strings.Repeat("long-path-", 20), Evidence: strings.Repeat("Bounded evidence with café and naïve text. ", 20), Remediation: strings.Repeat("Verify the source before editing. ", 15)})
	}
	if _, err := PDF(r); err != nil {
		t.Fatal(err)
	}
}
