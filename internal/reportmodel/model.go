// Package reportmodel defines the immutable public evidence shared by HTML and PDF.
package reportmodel

import "time"

type Report struct {
	ID                 string    `json:"id"`
	Domain             string    `json:"domain"`
	StartedAt          time.Time `json:"started_at"`
	CompletedAt        time.Time `json:"completed_at"`
	MethodologyVersion string    `json:"methodology_version"`
	SelectionPolicy    string    `json:"selection_policy"`
	Pages              []Page    `json:"pages"`
	Findings           []Finding `json:"findings"`
	Pillars            []Pillar  `json:"pillars"`
}
type Page struct {
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}
type Finding struct {
	ID           string  `json:"id"`
	Pillar       string  `json:"pillar"`
	Title        string  `json:"title"`
	Status       string  `json:"status"` // pass, partial, fail, unknown, not_applicable, information
	URL          string  `json:"url"`
	Evidence     string  `json:"evidence"`
	Remediation  string  `json:"remediation"`
	Weight       float64 `json:"weight"`
	Foundational bool    `json:"foundational"`
}
type Pillar struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Score    *float64 `json:"score"` // nil means evidence is insufficient
	Coverage float64  `json:"coverage"`
}
