package scanner

import (
	"net/url"
	"testing"
)

func TestPricingApplicabilityAndObservedEvidence(t *testing.T) {
	home, _ := url.Parse("https://example.com/")
	for _, tc := range []struct{ html, want string }{
		{`<main>Free website audits</main><script type="application/ld+json">{"@context":"https://schema.org","@type":"Offer","price":"0","priceCurrency":"USD"}</script>`, "pass"},
		{`<meta name="site:type" content="app"><main><a href="/pricing">Pricing</a></main>`, "partial"},
		{`<meta name="site:type" content="store"><main>Buy products</main>`, "fail"},
		{`<main>About a community organization</main>`, "not_applicable"},
	} {
		got := pricingChecks(home, []pageResult{readinessPage(t, tc.html)})[0]
		if got.Status != tc.want {
			t.Fatalf("%s: %+v", tc.want, got)
		}
	}
	if got := pricingChecks(home, nil)[0]; got.Status != "unknown" {
		t.Fatal(got)
	}
}
