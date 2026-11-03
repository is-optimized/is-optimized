package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
)

// TestParityLive is explicitly opt-in. It makes sequential public, unauthenticated
// scans using the production Scanner and its existing time/byte/request limits.
// The output directory should be on external artifact storage, outside the repo.
// Run from the integrated candidate, for example:
// IS_OPTIMIZED_PARITY_LIVE_DIR=/Volumes/BuildOffload/artifacts/parity go test ./internal/scanner -run '^TestParityLive$' -timeout 12m -count=1 -v
// Optional IS_AGENTIC_PARITY_BASELINE_DIR reuses existing <domain>.json reports
// without invoking npm, refreshing upstream reports, or sending provider writes.
func TestParityLive(t *testing.T) {
	root := os.Getenv("IS_OPTIMIZED_PARITY_LIVE_DIR")
	if root == "" {
		t.Skip("set IS_OPTIMIZED_PARITY_LIVE_DIR to opt into eight public-site scans")
	}
	if !filepath.IsAbs(root) {
		t.Fatal("IS_OPTIMIZED_PARITY_LIVE_DIR must be an absolute external artifact path")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	// A fresh run directory preserves previous receipts and avoids overwriting
	// uncertain or interrupted earlier scans.
	runDir, err := os.MkdirTemp(root, "parity-live-"+time.Now().UTC().Format("20060102T150405Z")+"-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("parity artifacts: %s", runDir)
	baselineDir := os.Getenv("IS_AGENTIC_PARITY_BASELINE_DIR")
	if baselineDir != "" && !filepath.IsAbs(baselineDir) {
		t.Fatal("IS_AGENTIC_PARITY_BASELINE_DIR must be absolute when supplied")
	}
	scanner := New()
	var results []parityLiveResult
	for _, domain := range []string{"prosody.chat", "is-agentic.com", "vercel.com", "github.com", "galileo.ai", "gymshark.com", "parallel.ai", "is-optimized.com"} {
		t.Run(domain, func(t *testing.T) {
			started := time.Now()
			report, scanErr := scanner.Scan(context.Background(), domain)
			result := parityLiveResult{Domain: domain, StartedAt: started.UTC(), ElapsedMS: time.Since(started).Milliseconds(), Pages: len(report.Pages), MethodologyVersion: report.MethodologyVersion, Pillars: report.Pillars, AgentReadiness: report.AgentReadiness, FindingStatusCounts: map[string]int{}}
			if scanErr != nil {
				result.Error = scanErr.Error()
			}
			for _, finding := range report.Findings {
				result.FindingStatusCounts[finding.Status]++
			}
			if baselineDir != "" {
				baselinePath := filepath.Join(baselineDir, domain+".json")
				// Existing baselines remain read-only. Copy valid JSON unchanged and
				// fingerprint it so later comparisons identify the exact receipt.
				baseline, readErr := parityReadBaseline(baselinePath)
				if readErr != nil {
					result.BaselineNote = "Existing baseline not observed: " + readErr.Error()
				} else {
					sum := sha256.Sum256(baseline)
					result.BaselineSHA256 = hex.EncodeToString(sum[:])
					parityWriteArtifact(t, filepath.Join(runDir, domain+".is-agentic-baseline.json"), baseline)
				}
			} else {
				result.BaselineNote = "Existing is-agentic baseline directory not supplied; no upstream comparison inferred."
			}
			parityWriteJSON(t, filepath.Join(runDir, domain+".report.json"), report)
			parityWriteFindingsCSV(t, filepath.Join(runDir, domain+".findings.csv"), report.Findings)
			parityWriteJSON(t, filepath.Join(runDir, domain+".scores.json"), result)
			results = append(results, result)
			// Record progress after each domain so an interrupted later scan
			// does not erase successful earlier artifacts.
			parityWriteJSON(t, filepath.Join(runDir, "summary.json"), results)
			t.Logf("%s: %d pages, %d findings, %d ms; error=%q", domain, result.Pages, len(report.Findings), result.ElapsedMS, result.Error)
			if scanErr != nil {
				t.Errorf("public scan did not complete: %v", scanErr)
			}
		})
	}
}

type parityLiveResult struct {
	Domain              string                             `json:"domain"`
	StartedAt           time.Time                          `json:"started_at"`
	ElapsedMS           int64                              `json:"elapsed_ms"`
	Pages               int                                `json:"pages"`
	MethodologyVersion  string                             `json:"methodology_version"`
	Pillars             []reportmodel.Pillar               `json:"pillars"`
	AgentReadiness      *reportmodel.AgentReadinessSummary `json:"agent_readiness"`
	FindingStatusCounts map[string]int                     `json:"finding_status_counts"`
	Error               string                             `json:"error,omitempty"`
	BaselineSHA256      string                             `json:"is_agentic_baseline_sha256,omitempty"`
	BaselineNote        string                             `json:"baseline_note,omitempty"`
}

func parityWriteJSON(t *testing.T, path string, value any) {
	t.Helper()
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	parityWriteArtifact(t, path, append(body, '\n'))
}

func parityWriteArtifact(t *testing.T, path string, body []byte) {
	t.Helper()
	f, err := os.CreateTemp(filepath.Dir(path), ".parity-artifact-")
	if err != nil {
		t.Fatal(err)
	}
	temporary := f.Name()
	defer os.Remove(temporary)
	if _, err := f.Write(body); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, path); err != nil {
		t.Fatal(err)
	}
}

func parityWriteFindingsCSV(t *testing.T, path string, findings []reportmodel.Finding) {
	t.Helper()
	var body strings.Builder
	w := csv.NewWriter(&body)
	if err := w.Write([]string{"id", "status", "url", "evidence"}); err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		row := []string{f.ID, f.Status, f.URL, excerptLimit(f.Evidence, 300)}
		for i := range row {
			// CSV quoting does not prevent spreadsheet formula execution.
			// Keep scanned text inert if artifacts are opened in a spreadsheet.
			trimmed := strings.TrimLeft(row[i], " \t\r\n")
			if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
				row[i] = "'" + row[i]
			}
		}
		if err := w.Write(row); err != nil {
			t.Fatal(err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		t.Fatal(err)
	}
	parityWriteArtifact(t, path, []byte(body.String()))
}

func parityReadBaseline(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	const maxBaselineBytes = 8 << 20
	body, err := io.ReadAll(io.LimitReader(f, maxBaselineBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBaselineBytes || !json.Valid(body) {
		return nil, fmt.Errorf("baseline exceeds 8 MiB or is not valid JSON")
	}
	return body, nil
}
