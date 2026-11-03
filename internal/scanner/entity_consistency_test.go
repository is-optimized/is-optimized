package scanner

import "testing"

func TestSharedEntityComparisonsPreserveDistinctBranches(t *testing.T) {
	for _, tc := range []struct{ name, secondID, secondName, phone, want string }{
		{"formatting", "https://example.com/#org", "Example Inc.", "+1 555 555 0100", "pass"},
		{"different declared facts", "https://example.com/#org", "Other Company", "+15555550101", "partial"},
		{"separate branch", "https://example.com/#branch", "Other Company", "+15555550101", "not_applicable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := readinessPage(t, `<script type="application/ld+json">{"@context":"https://schema.org","@type":"Organization","@id":"https://example.com/#org","name":"Example Inc","telephone":"+15555550100"}</script>`)
			second := readinessPage(t, `<script type="application/ld+json">{"@context":"https://schema.org","@type":"Organization","@id":"`+tc.secondID+`","name":"`+tc.secondName+`","telephone":"`+tc.phone+`"}</script>`)
			got := entityConsistencyChecks([]pageResult{first, second})[0]
			if got.Status != tc.want {
				t.Fatalf("%+v", got)
			}
		})
	}
	if got := entityConsistencyChecks([]pageResult{{}})[0]; got.Status != "unknown" {
		t.Fatal(got)
	}
}
