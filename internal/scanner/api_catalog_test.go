package scanner

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestScannerReadsRFC9727APICatalog(t *testing.T) {
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/robots.txt":
			return reply(r, http.StatusNotFound, "", "text/plain"), nil
		case "/":
			return reply(r, http.StatusOK, `<title>Example</title><main><h1>Example</h1><a href="/docs/api">API docs</a></main>`, "text/html"), nil
		case "/.well-known/api-catalog":
			if !strings.Contains(r.Header.Get("Accept"), "application/linkset+json") {
				t.Errorf("catalog request missing Linkset Accept header: %q", r.Header.Get("Accept"))
			}
			body := `{"linkset":[{"anchor":"https://example.com/.well-known/api-catalog","item":[{"href":"https://api.example.com/v1"}]}]}`
			return reply(r, http.StatusOK, body, `application/linkset+json; profile="https://www.rfc-editor.org/info/rfc9727"`), nil
		default:
			return reply(r, http.StatusNotFound, "Not found", "text/plain"), nil
		}
	})
	report, err := s.Scan(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range report.Findings {
		if finding.ID == "agent.api_catalog" {
			if finding.Status != "pass" || !strings.Contains(finding.Evidence, "1 API endpoint") {
				t.Fatalf("unexpected catalog finding: %+v", finding)
			}
			return
		}
	}
	t.Fatal("API catalog finding missing")
}

func TestAPICatalogApplicabilityAndServerError(t *testing.T) {
	home, err := url.Parse("https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got := apiCatalogChecks(nil, home, pageResult{}); got[0].Status != "not_applicable" {
		t.Fatalf("API catalog without a developer surface should be N/A: %+v", got[0])
	}
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/robots.txt":
			return reply(r, http.StatusNotFound, "", "text/plain"), nil
		case "/":
			return reply(r, http.StatusOK, `<title>Example</title><main><h1>Example</h1><a href="/api">API</a></main>`, "text/html"), nil
		case "/.well-known/api-catalog":
			return reply(r, http.StatusServiceUnavailable, "temporarily unavailable", "text/plain"), nil
		default:
			return reply(r, http.StatusNotFound, "Not found", "text/plain"), nil
		}
	})
	report, err := s.Scan(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range report.Findings {
		if finding.ID == "agent.api_catalog" {
			if finding.Status != "unknown" {
				t.Fatalf("server error must not be scored as a site defect: %+v", finding)
			}
			return
		}
	}
	t.Fatal("API catalog finding missing")
}
