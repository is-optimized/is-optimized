package reportmodel

// CompactPageReferences replaces finding URLs that point to a page already
// listed in the report with a small page index. This keeps durable report
// records bounded without changing the rendered evidence.
func CompactPageReferences(r Report) Report {
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
	return r
}

// ExpandPageReferences restores full URLs after loading a compact report.
func ExpandPageReferences(r Report) Report {
	r.Findings = append([]Finding(nil), r.Findings...)
	for i := range r.Findings {
		index := r.Findings[i].PageIndex
		if index == nil {
			continue
		}
		if *index >= 0 && *index < len(r.Pages) {
			r.Findings[i].URL = r.Pages[*index].URL
		}
		r.Findings[i].PageIndex = nil
	}
	return r
}
