package render

import (
	"fmt"
	"sort"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
)

// Presentation summaries count immutable findings; they never calculate a score.
type findingCounts struct {
	Pass, Partial, Fail, Unknown, NotApplicable, Information, Other, Total int
}

func (c *findingCounts) add(status string) {
	c.Total++
	switch status {
	case "pass":
		c.Pass++
	case "partial":
		c.Partial++
	case "fail":
		c.Fail++
	case "unknown":
		c.Unknown++
	case "not_applicable":
		c.NotApplicable++
	case "information":
		c.Information++
	default:
		c.Other++
	}
}

type countSegment struct {
	Status, Label string
	Count         int
}

func (c findingCounts) segments() []countSegment {
	all := []countSegment{{"pass", "Passed", c.Pass}, {"partial", "Partial", c.Partial}, {"fail", "Needs attention", c.Fail}, {"unknown", "Unknown", c.Unknown}, {"not_applicable", "Not applicable", c.NotApplicable}, {"information", "Informational", c.Information}, {"other", "Unclassified", c.Other}}
	var visible []countSegment
	for _, s := range all {
		if s.Count > 0 {
			visible = append(visible, s)
		}
	}
	return visible
}

type pillarSummary struct {
	ID, Name, Short, Question string
	Score                     *float64
	Coverage                  float64
	Missing                   bool
	Findings                  []reportmodel.Finding
	Counts                    findingCounts
	Segments                  []countSegment
}
type reportSummary struct {
	Pillars             []pillarSummary
	Counts              findingCounts
	Fixes               []reportmodel.Finding
	Narrative           string
	Selected, Responded int
}

func summarize(r reportmodel.Report) reportSummary {
	v := reportSummary{Selected: len(r.Pages)}
	definitions := []pillarSummary{
		{ID: "seo", Name: "Search engine readiness", Short: "SEO", Question: "Can search engines read the essentials?", Missing: true},
		{ID: "geo", Name: "Generative search readiness", Short: "GEO", Question: "Is your information clear and attributable?", Missing: true},
		{ID: "agent", Name: "AI-agent readiness", Short: "AEO", Question: "Can software understand the interface?", Missing: true},
	}
	positions := map[string]int{}
	for i, p := range definitions {
		positions[p.ID] = i
	}
	for _, p := range r.Pillars {
		i, ok := positions[p.ID]
		if !ok {
			i = len(definitions)
			positions[p.ID] = i
			definitions = append(definitions, pillarSummary{ID: p.ID, Name: p.Name, Short: p.ID, Question: p.Name})
		}
		definitions[i].Score = p.Score
		definitions[i].Coverage = p.Coverage
		definitions[i].Missing = false
	}
	for _, f := range r.Findings {
		v.Counts.add(f.Status)
		if f.Status == "fail" || f.Status == "partial" {
			v.Fixes = append(v.Fixes, f)
		}
		i, ok := positions[f.Pillar]
		if !ok {
			i = len(definitions)
			positions[f.Pillar] = i
			definitions = append(definitions, pillarSummary{ID: f.Pillar, Name: f.Pillar, Short: f.Pillar, Question: f.Pillar, Missing: true})
		}
		definitions[i].Findings = append(definitions[i].Findings, f)
		definitions[i].Counts.add(f.Status)
	}
	// Status is the priority, not a claim of business impact. Within each status,
	// foundational checks are first; ties retain the saved evidence order.
	sort.SliceStable(v.Fixes, func(i, j int) bool {
		a, b := v.Fixes[i], v.Fixes[j]
		if a.Status != b.Status {
			return a.Status == "fail"
		}
		return a.Foundational && !b.Foundational
	})
	for i := range definitions {
		definitions[i].Segments = definitions[i].Counts.segments()
	}
	v.Pillars = definitions
	for _, p := range r.Pages {
		if p.StatusCode >= 200 && p.StatusCode < 300 && p.Error == "" {
			v.Responded++
		}
	}
	if v.Counts.Total == 0 {
		v.Narrative = "No individual checks were recorded. The available evidence does not establish website readiness."
	} else {
		v.Narrative = fmt.Sprintf("%d checks need attention; %d checks were not observed. Use the evidence below to decide what to fix next.", v.Counts.Fail+v.Counts.Partial, v.Counts.Unknown)
	}
	for _, p := range v.Pillars {
		if p.Missing || p.Score == nil {
			v.Narrative += " Some pillar scores are unavailable; inspect coverage and missing results."
			break
		}
	}
	return v
}
