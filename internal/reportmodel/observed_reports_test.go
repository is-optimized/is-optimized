package reportmodel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Opt-in replay of public live reports; no network or provider effects.
func TestObservedParityReportsFitDurableRecord(t *testing.T) {
	dir := os.Getenv("IS_OPTIMIZED_OBSERVED_REPORTS")
	if dir == "" {
		t.Skip("set IS_OPTIMIZED_OBSERVED_REPORTS to replay live artifacts")
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.report.json"))
	if err != nil || len(paths) != 8 {
		t.Fatalf("expected eight reports: %d err=%v", len(paths), err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var report Report
			if err = json.Unmarshal(data, &report); err != nil {
				t.Fatal(err)
			}
			compact := CompactPageReferences(report)
			record, err := json.Marshal(map[string]any{"Report": compact, "Expires": "2026-12-03T00:00:00Z", "Artifact": "public-fixture"})
			if err != nil {
				t.Fatal(err)
			}
			if len(record) > 850000 {
				t.Fatalf("durable record exceeds store ceiling: %d bytes", len(record))
			}
			encoded, _ := json.Marshal(compact)
			var stored Report
			if err = json.Unmarshal(encoded, &stored); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(ExpandPageReferences(stored), report) {
				t.Fatal("evidence, metadata or scores changed during storage")
			}
			t.Logf("public JSON=%d bytes; durable snapshot=%d bytes", len(data), len(record))
		})
	}
}
