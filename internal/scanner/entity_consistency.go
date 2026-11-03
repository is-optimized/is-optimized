package scanner

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"golang.org/x/net/html"
)

// Compare only explicit shared identities. Separate business locations, products,
// authors, and legal/trading names must not be merged by a guessed brand match.
func entityConsistencyChecks(pages []pageResult) []reportmodel.Finding {
	f := reportmodel.Finding{ID: "geo.contact_consistency", Pillar: "geo", Title: "Sampled business identity consistency", Status: "not_applicable", Weight: 1, Tier: "recommended", Stage: "read", Applicability: "Compares name, telephone and PostalAddress fields only across Organization/LocalBusiness declarations with the same explicit @id or URL on sampled pages. Normalized equality does not verify identity or factual truth.", Remediation: "Review differing declarations for the same entity and keep verified name, address and contact details consistent. Preserve legitimate separate branches and legal/trading names; do not invent business facts.", Standards: []reportmodel.StandardReference{{ID: "Schema.org Organization", URL: "https://schema.org/Organization", Coverage: "partial"}}}
	type entityRecord struct{ fields map[string]string }
	groups := map[string][]entityRecord{}
	usable := 0
	for _, page := range pages {
		if page.doc == nil || page.page.Error != "" {
			continue
		}
		usable++
		if f.URL == "" {
			f.URL = page.page.URL
		}
		walk(page.doc, func(n *html.Node) {
			if n.Type != html.ElementNode || n.Data != "script" || !strings.EqualFold(attr(n, "type"), "application/ld+json") {
				return
			}
			var document any
			if json.Unmarshal([]byte(rawScriptText(n)), &document) != nil {
				return
			}
			var entities []map[string]any
			walkJSONLD(document, &entities, map[string]bool{})
			for _, entity := range entities {
				kinds := schemaTypes(entity)
				if !kinds["organization"] && !kinds["localbusiness"] {
					continue
				}
				identity := schemaString(entity["@id"])
				if identity == "" {
					identity = schemaString(entity["url"])
				}
				if identity == "" {
					continue
				}
				base := pageURL(page.page.URL)
				if base == nil {
					continue
				}
				u, err := base.Parse(identity)
				if err != nil || validateURL(u) != nil {
					continue
				}
				fields := map[string]string{"name": normalizeEntityField(schemaString(entity["name"])), "telephone": normalizeEntityField(schemaString(entity["telephone"]))}
				if address, ok := entity["address"].(map[string]any); ok {
					var parts []string
					for _, key := range []string{"streetAddress", "addressLocality", "addressRegion", "postalCode", "addressCountry"} {
						parts = append(parts, schemaString(address[key]))
					}
					fields["address"] = normalizeEntityField(strings.Join(parts, " "))
				}
				groups[u.String()] = append(groups[u.String()], entityRecord{fields: fields})
			}
		})
	}
	comparisons, conflicts := 0, []string{}
	for identity, records := range groups {
		for _, field := range []string{"name", "telephone", "address"} {
			values := map[string]bool{}
			observed := 0
			for _, record := range records {
				if value := record.fields[field]; value != "" {
					observed++
					values[value] = true
				}
			}
			if observed < 2 {
				continue
			}
			comparisons++
			if len(values) > 1 {
				label := []rune(identity)
				if len(label) > 120 {
					label = label[:120]
				}
				conflicts = append(conflicts, field+" for "+string(label))
			}
		}
	}
	sort.Strings(conflicts)
	if len(conflicts) > 3 {
		conflicts = conflicts[:3]
	}
	switch {
	case usable == 0:
		f.Status = "unknown"
		f.Evidence = "Sampled page HTML unavailable; business identity declarations were not compared."
	case comparisons == 0:
		f.Evidence = "No repeated explicit business identity with comparable name, telephone or address fields was observed across sampled declarations; separate entities were not merged."
	case len(conflicts) > 0:
		f.Status = "partial"
		f.Evidence = fmt.Sprintf("%d comparable shared-identity fields; differing normalized declarations require review: %s. Formatting and legal/trading-name equivalence were not verified.", comparisons, strings.Join(conflicts, "; "))
	default:
		f.Status = "pass"
		f.Evidence = fmt.Sprintf("%d repeated shared-identity fields match after case, whitespace and punctuation normalization. This does not verify business ownership or real-world contact details.", comparisons)
	}
	return []reportmodel.Finding{f}
}

func normalizeEntityField(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, value)
}
