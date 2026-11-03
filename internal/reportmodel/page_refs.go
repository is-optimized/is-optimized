package reportmodel

import "encoding/json"

// CompactPageReferences replaces finding URLs that point to a page already
// listed in the report with a small page index. This keeps durable report
// records bounded without changing the rendered evidence.
func CompactPageReferences(r Report) Report {
	r = ExpandPageReferences(r)
	pages := make(map[string]int, len(r.Pages))
	for i, page := range r.Pages {
		pages[page.URL] = i
	}
	r.Findings = append([]Finding(nil), r.Findings...)
	for i := range r.Findings {
		if index, ok := pages[r.Findings[i].URL]; ok {
			r.Findings[i].URL = ""
			r.Findings[i].PageIndex = &index
		}
	}
	// Per-page check definitions repeat across the sample. Store them once,
	// keeping verdicts and evidence per finding. Expand restores the public API.
	counts := map[string]int{}
	keys := make([]string, len(r.Findings))
	definitions := make([]FindingDefinition, len(r.Findings))
	for i, finding := range r.Findings {
		definition := FindingDefinition{ID: finding.ID, Stage: finding.Stage, EvidenceLevel: finding.EvidenceLevel, Applicability: finding.Applicability, Standards: finding.Standards}
		encoded, _ := json.Marshal(definition)
		keys[i], definitions[i] = string(encoded), definition
		counts[keys[i]]++
	}
	indexes := map[string]int{}
	for i, finding := range r.Findings {
		if counts[keys[i]] < 2 {
			continue
		}
		index, found := indexes[keys[i]]
		if !found {
			index = len(r.FindingDefinitions)
			indexes[keys[i]] = index
			r.FindingDefinitions = append(r.FindingDefinitions, definitions[i])
		}
		finding.DefinitionIndex = &index
		finding.Stage, finding.EvidenceLevel, finding.Applicability = "", "", ""
		finding.Standards = nil
		r.Findings[i] = finding
	}
	return r
}

// ExpandPageReferences restores full URLs after loading a compact report.
func ExpandPageReferences(r Report) Report {
	r.Findings = append([]Finding(nil), r.Findings...)
	for i := range r.Findings {
		if definition := r.Findings[i].DefinitionIndex; definition != nil && *definition >= 0 && *definition < len(r.FindingDefinitions) {
			metadata := r.FindingDefinitions[*definition]
			if metadata.ID == r.Findings[i].ID {
				r.Findings[i].Stage, r.Findings[i].EvidenceLevel, r.Findings[i].Applicability = metadata.Stage, metadata.EvidenceLevel, metadata.Applicability
				r.Findings[i].Standards = metadata.Standards
			}
			r.Findings[i].DefinitionIndex = nil
		}
		index := r.Findings[i].PageIndex
		if index == nil {
			continue
		}
		if *index >= 0 && *index < len(r.Pages) {
			r.Findings[i].URL = r.Pages[*index].URL
		}
		r.Findings[i].PageIndex = nil
	}
	r.FindingDefinitions = nil
	return r
}
