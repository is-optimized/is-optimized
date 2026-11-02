// Package reportmodel defines the immutable public evidence shared by HTML and PDF.
package reportmodel

import "time"

type Report struct {
	ID                   string                 `json:"id"`
	Domain               string                 `json:"domain"`
	StartedAt            time.Time              `json:"started_at"`
	CompletedAt          time.Time              `json:"completed_at"`
	MethodologyVersion   string                 `json:"methodology_version"`
	SelectionPolicy      string                 `json:"selection_policy"`
	BehavioralEvaluation string                 `json:"behavioral_evaluation,omitempty"`
	Pages                []Page                 `json:"pages"`
	Findings             []Finding              `json:"findings"`
	Pillars              []Pillar               `json:"pillars"`
	AgentReadiness       *AgentReadinessSummary `json:"agent_readiness,omitempty"`
}
type Page struct {
	RequestedURL string `json:"requested_url,omitempty"`
	URL          string `json:"url"`
	StatusCode   int    `json:"status_code"`
	Error        string `json:"error,omitempty"`
	DurationMS   int64  `json:"duration_ms"`
}
type Finding struct {
	ID            string              `json:"id"`
	Pillar        string              `json:"pillar"`
	Title         string              `json:"title"`
	Status        string              `json:"status"` // pass, partial, fail, unknown, not_applicable, information
	URL           string              `json:"url,omitempty"`
	PageIndex     *int                `json:"page_index,omitempty"`
	Evidence      string              `json:"evidence"`
	Remediation   string              `json:"remediation"`
	Weight        float64             `json:"weight"`
	Tier          string              `json:"readiness_tier,omitempty"`
	Foundational  bool                `json:"foundational"`
	Stage         string              `json:"stage,omitempty"`
	EvidenceLevel string              `json:"evidence_level,omitempty"`
	Applicability string              `json:"applicability,omitempty"`
	Standards     []StandardReference `json:"standards,omitempty"`
}

type ReadinessTierSummary struct {
	Earned    float64 `json:"earned"`
	Available float64 `json:"available"`
	Passing   int     `json:"passing"`
	Total     int     `json:"total"`
}

type AgentReadinessSummary struct {
	ScoreLabel      string               `json:"score_label,omitempty"`
	Coverage        float64              `json:"coverage"`
	Essential       ReadinessTierSummary `json:"essential"`
	Recommended     ReadinessTierSummary `json:"recommended"`
	BonusPoints     float64              `json:"bonus_points"`
	PositiveSignals int                  `json:"positive_signals"`
}

// StandardReference describes the relationship of an observation to a practice.
// Partial and reference mappings are not conformance or certification claims.
type StandardReference struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Coverage string `json:"coverage"`
}
type Pillar struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Score    *float64 `json:"score"` // nil means evidence is insufficient
	Coverage float64  `json:"coverage"`
}
