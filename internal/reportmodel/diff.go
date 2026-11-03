package reportmodel

import (
	"encoding/json"
	"sort"
)

// FindingChange compares two immutable snapshots, not unobserved live behavior.
type FindingChange struct {
	ID               string `json:"id"`
	URL              string `json:"url,omitempty"`
	PreviousStatus   string `json:"previous_status,omitempty"`
	Status           string `json:"status,omitempty"`
	PreviousEvidence string `json:"previous_evidence,omitempty"`
	Evidence         string `json:"evidence,omitempty"`
}

// WithHistory attaches a bounded status/evidence diff. Missing findings mean
// absent from the later sample, never that the earlier issue was fixed.
func WithHistory(current, previous Report) Report {
	if current.Domain == "" || previous.Domain != current.Domain || previous.ID == "" || previous.ID == current.ID || !previous.CompletedAt.Before(current.CompletedAt) {
		return current
	}
	current.PreviousReportID = previous.ID
	current.Changes, current.ChangesTruncated = nil, false
	old := map[string]Finding{}
	for _, f := range ExpandPageReferences(previous).Findings {
		old[f.ID+"\x00"+f.URL] = f
	}
	var changes []FindingChange
	for _, f := range ExpandPageReferences(current).Findings {
		key := f.ID + "\x00" + f.URL
		prior, exists := old[key]
		delete(old, key)
		if exists && prior.Status == f.Status && prior.Evidence == f.Evidence {
			continue
		}
		changes = append(changes, FindingChange{ID: f.ID, URL: f.URL, PreviousStatus: prior.Status, Status: f.Status, PreviousEvidence: diffSnippet(prior.Evidence), Evidence: diffSnippet(f.Evidence)})
	}
	for _, f := range old {
		changes = append(changes, FindingChange{ID: f.ID, URL: f.URL, PreviousStatus: f.Status, PreviousEvidence: diffSnippet(f.Evidence)})
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].ID != changes[j].ID {
			return changes[i].ID < changes[j].ID
		}
		return changes[i].URL < changes[j].URL
	})
	// Bound serialized history as well as row count; URLs and escaped evidence
	// can otherwise crowd out the report's existing durable record budget.
	bytes := 2
	for _, change := range changes {
		encoded, _ := json.Marshal(change)
		if len(current.Changes) == 100 || bytes+len(encoded)+1 > 48000 {
			current.ChangesTruncated = true
			break
		}
		current.Changes = append(current.Changes, change)
		bytes += len(encoded) + 1
	}
	return current
}

func diffSnippet(text string) string {
	runes := []rune(text)
	if len(runes) > 256 {
		return string(runes[:256]) + "…"
	}
	return text
}
