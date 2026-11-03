package scanner

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
)

func TestRequestReservationsNeverExceedLimit(t *testing.T) {
	var counter, accepted atomic.Int32
	var group sync.WaitGroup
	for range 100 {
		group.Add(1)
		go func() {
			defer group.Done()
			if reserveRequest(&counter, 16) {
				accepted.Add(1)
			}
		}()
	}
	group.Wait()
	if counter.Load() != 16 || accepted.Load() != 16 {
		t.Fatalf("counter=%d accepted=%d", counter.Load(), accepted.Load())
	}
}

func TestBudgetLimitedScanPreservesEvidenceButWithholdsScores(t *testing.T) {
	value := 90.0
	report := reportmodel.Report{Pillars: []reportmodel.Pillar{{ID: "seo", Score: &value}}, Findings: []reportmodel.Finding{{ID: "title", Status: "pass"}}, AgentReadiness: &reportmodel.AgentReadinessSummary{ScoreLabel: "Ready"}}
	ss := &session{ctx: context.Background()}
	ss.discoveryBudgetExhaustions.Add(1)
	applyScanLimitations(&report, ss)
	if report.Pillars[0].Score != nil || report.AgentReadiness.ScoreLabel != "" || len(report.Limitations) != 1 || report.Findings[0].Status != "pass" {
		t.Fatalf("%+v", report)
	}
}
