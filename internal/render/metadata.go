package render

import (
	"strings"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
)

func stageLabel(stage string) string {
	switch stage {
	case "find":
		return "Find"
	case "read":
		return "Read"
	case "act":
		return "Act"
	default:
		return "Not recorded"
	}
}

func evidenceLabel(f reportmodel.Finding) string {
	// An unobserved finding cannot establish a higher level of evidence, even
	// when a malformed or future saved report includes conflicting metadata.
	if f.Status == "unknown" {
		return "Not recorded"
	}
	switch f.EvidenceLevel {
	case "observed_html":
		return "Observed HTML"
	case "discovered_interface":
		return "Discovered interface (not validated)"
	case "validated_contract":
		return "Validated contract"
	case "completed_task":
		return "Completed task"
	default:
		return "Not recorded"
	}
}

func applicabilityLabel(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "Not recorded"
	}
	return boundedMetadata(reason, 600)
}

func taskEvaluationLabel(state string) string {
	if state == "not_run" {
		return "Task evaluation not run"
	}
	return "Task evaluation not recorded"
}

type standardReference struct {
	ID, URL, Coverage string
}

func standardReferences(refs []reportmodel.StandardReference) []standardReference {
	var out []standardReference
	seen := map[string]bool{}
	for _, ref := range refs {
		if len(out) == 8 {
			break
		}
		// References are descriptive mappings, not arbitrary navigation URLs
		// imported from the scanned site. Only known identifiers link to the
		// fixed public guide; the stored URL is never promoted to a trusted link.
		if !knownStandard(ref.ID) || seen[ref.ID] {
			continue
		}
		coverage := "Coverage not recorded"
		switch ref.Coverage {
		case "partial":
			coverage = "Partial mapping"
		case "reference":
			coverage = "Reference only"
		}
		out = append(out, standardReference{ID: ref.ID, URL: "https://www.agentready.org/", Coverage: coverage})
		seen[ref.ID] = true
	}
	return out
}

func knownStandard(id string) bool {
	switch id {
	case "AR-FIND-01", "AR-FIND-02", "AR-FIND-03", "AR-FIND-04",
		"AR-READ-01", "AR-READ-02", "AR-READ-03", "AR-READ-04", "AR-READ-05", "AR-READ-06", "AR-READ-07", "AR-READ-08", "AR-READ-09", "AR-READ-11",
		"AR-ACT-01", "AR-ACT-02", "AR-ACT-03", "AR-ACT-06", "AR-ACT-09", "AR-ACT-10":
		return true
	}
	return false
}

func boundedMetadata(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}
