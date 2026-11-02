package scanner

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestOAuthMetadataFindingsAreApplicabilityAware(t *testing.T) {
	metadata := map[string]any{
		"issuer":                           "https://issuer.example.com",
		"authorization_endpoint":           "https://issuer.example.com/authorize",
		"token_endpoint":                   "https://issuer.example.com/token",
		"code_challenge_methods_supported": []any{"S256"},
		"scopes_supported":                 []any{"read", "write"},
	}
	want := map[string]string{"agent.oauth.discovery": "pass", "agent.oauth.pkce": "pass", "agent.oauth.scopes": "pass"}
	for _, finding := range oauthFindings("https://example.com/.well-known/openid-configuration", metadata, true, nil) {
		if finding.Status != want[finding.ID] || finding.Stage != "act" || finding.Applicability == "" || len(finding.Standards) == 0 {
			t.Fatalf("unexpected OAuth finding: %+v", finding)
		}
	}
	for _, finding := range oauthFindings("https://example.com/.well-known/openid-configuration", nil, false, nil) {
		if finding.Status != "not_applicable" {
			t.Fatalf("OAuth was scored without an auth surface: %+v", finding)
		}
	}
	note := "OAuth/OIDC metadata was not observed: timeout"
	for _, finding := range oauthFindings("https://example.com/.well-known/openid-configuration", nil, true, &note) {
		if finding.Status != "unknown" {
			t.Fatalf("unobserved OAuth metadata was scored: %+v", finding)
		}
	}
}

func TestScannerReadsOAuthDiscoveryWithoutAuthenticating(t *testing.T) {
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			t.Errorf("scanner attempted non-GET request %s %s", r.Method, r.URL.Path)
		}
		switch r.URL.Path {
		case "/robots.txt":
			return reply(r, http.StatusNotFound, "", "text/plain"), nil
		case "/":
			return reply(r, http.StatusOK, `<title>Example</title><main><h1>Example</h1><a href="/oauth/login">Sign in</a></main>`, "text/html"), nil
		case "/.well-known/oauth-authorization-server":
			return reply(r, http.StatusOK, `{"issuer":"https://example.com","authorization_endpoint":"https://example.com/authorize","token_endpoint":"https://example.com/token","code_challenge_methods_supported":["S256"],"scopes_supported":["read"]}`, "application/json"), nil
		case "/.well-known/oauth-protected-resource":
			return reply(r, http.StatusOK, `{"resource":"https://example.com/api","authorization_servers":["https://example.com"]}`, "application/json"), nil
		default:
			return reply(r, http.StatusNotFound, "Not found", "text/plain"), nil
		}
	})
	report, err := s.Scan(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, finding := range report.Findings {
		if strings.HasPrefix(finding.ID, "agent.oauth.") {
			got[finding.ID] = finding.Status
		}
	}
	for id, want := range map[string]string{
		"agent.oauth.discovery": "pass", "agent.oauth.pkce": "pass",
		"agent.oauth.scopes": "pass", "agent.oauth.protected_resource": "pass",
	} {
		if got[id] != want {
			t.Fatalf("%s=%q, want %q (all: %v)", id, got[id], want, got)
		}
	}
}

func TestScannerTreatsOAuthServerErrorsAsUnobserved(t *testing.T) {
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/robots.txt":
			return reply(r, http.StatusNotFound, "", "text/plain"), nil
		case "/":
			return reply(r, http.StatusOK, `<title>Example</title><main><h1>Example</h1><a href="/oauth/login">Sign in</a></main>`, "text/html"), nil
		case "/.well-known/oauth-authorization-server", "/.well-known/openid-configuration", "/.well-known/oauth-protected-resource":
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
		if strings.HasPrefix(finding.ID, "agent.oauth.") && finding.Status != "unknown" {
			t.Errorf("server error must not be scored as a site defect: %+v", finding)
		}
	}
}
