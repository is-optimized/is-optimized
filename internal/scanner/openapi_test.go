package scanner

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

const validOpenAPIJSON = `{"openapi":"3.1.0","info":{"title":"Example API","version":"v1"},"paths":{"/health":{"get":{"operationId":"getHealth","summary":"Read health","responses":{"200":{"description":"OK","content":{"application/json":{"schema":{"type":"object","properties":{"ok":{"type":"boolean"}}}}}},"400":{"description":"Bad request","content":{"application/problem+json":{"schema":{"$ref":"#/components/schemas/Problem"}}}}}}}},"components":{"schemas":{"Problem":{"type":"object"}},"securitySchemes":{"oauth":{"type":"oauth2","flows":{"clientCredentials":{"tokenUrl":"/oauth/token","scopes":{"read":"Read data"}}}}}}}`

func TestDecodeAndAssessOpenAPIJSONAndYAML(t *testing.T) {
	for _, tc := range []struct {
		name, body, format string
	}{
		{name: "json", body: validOpenAPIJSON, format: "json"},
		{name: "yaml", body: "openapi: 3.1.0\ninfo:\n  title: Example API\n  version: v1\npaths:\n  /health:\n    get:\n      operationId: getHealth\n      summary: Read health\n      responses:\n        '200':\n          description: OK\n          content:\n            application/json:\n              schema:\n                type: object\n", format: "yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, format, err := decodeOpenAPI([]byte(tc.body))
			if err != nil || format != tc.format {
				t.Fatalf("decode format=%q err=%v", format, err)
			}
			stats := assessOpenAPI(doc)
			if !stats.version3 || !stats.hasOperations || !stats.versioned {
				t.Fatalf("missing OpenAPI metadata: %+v", stats)
			}
			if tc.name == "json" && (stats.typedSuccess != 1 || stats.typedError != 1 || !stats.scopedSchemes) {
				t.Fatalf("missing schema/error/scope evidence: %+v", stats)
			}
		})
	}
}

func TestDecodeOpenAPIRejectsYAMLAliases(t *testing.T) {
	if _, _, err := decodeOpenAPI([]byte("openapi: 3.1.0\ninfo: &shared {title: Example, version: v1}\ncopy: *shared\n")); !errors.Is(err, errOpenAPIYAMLComplexity) {
		t.Fatal("YAML aliases must be rejected to keep untrusted contract parsing bounded")
	}
}

func TestOpenAPIFindingVerdicts(t *testing.T) {
	parsed, _, err := decodeOpenAPI([]byte(validOpenAPIJSON))
	if err != nil {
		t.Fatal(err)
	}
	findings := openAPIFindings("https://example.com/openapi.json", parsed, true, nil)
	want := map[string]string{"agent.openapi.spec": "pass", "agent.openapi.operations": "pass", "agent.openapi.errors": "pass", "agent.openapi.scopes": "pass", "agent.openapi.versioning": "pass"}
	for _, f := range findings {
		if f.Status != want[f.ID] || f.Stage != "act" || f.Applicability == "" || len(f.Standards) == 0 {
			t.Fatalf("unexpected passing contract finding: %+v", f)
		}
	}

	partial, _, err := decodeOpenAPI([]byte(`{"openapi":"3.0.0","info":{"title":"Partial","version":"v1"},"paths":{"/health":{"get":{"operationId":"health","responses":{"200":{"description":"OK","content":{"application/json":{"schema":{"type":"object"}}}}}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range openAPIFindings("https://example.com/openapi.json", partial, true, nil) {
		if (f.ID == "agent.openapi.operations" || f.ID == "agent.openapi.errors") && f.Status != "partial" {
			t.Fatalf("expected proportional partial result: %+v", f)
		}
	}

	note := "A public contract response was observed but was not valid OpenAPI JSON or YAML."
	for _, f := range openAPIFindings("https://example.com/openapi.json", nil, true, &note) {
		if f.ID == "agent.openapi.spec" {
			if f.Status != "fail" || !strings.Contains(f.Evidence, "not valid OpenAPI") {
				t.Fatalf("invalid contract did not fail with evidence: %+v", f)
			}
		} else if f.Status != "unknown" {
			t.Fatalf("unobserved contract details were scored: %+v", f)
		}
	}
	for _, f := range openAPIFindings("https://example.com/openapi.json", nil, false, nil) {
		if f.Status != "not_applicable" {
			t.Fatalf("undiscovered API was scored: %+v", f)
		}
	}
}

func TestScannerParsesLinkedOpenAPIContract(t *testing.T) {
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/robots.txt":
			return reply(r, http.StatusNotFound, "", "text/plain"), nil
		case "/":
			return reply(r, http.StatusOK, `<title>Example</title><main><h1>Example</h1><a href="/docs/api">API docs</a></main>`, "text/html"), nil
		case "/openapi.json":
			return reply(r, http.StatusOK, validOpenAPIJSON, "application/json"), nil
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
		if strings.HasPrefix(finding.ID, "agent.openapi.") {
			got[finding.ID] = finding.Status
		}
	}
	for id, want := range map[string]string{
		"agent.openapi.spec": "pass", "agent.openapi.operations": "pass",
		"agent.openapi.errors": "pass", "agent.openapi.scopes": "pass", "agent.openapi.versioning": "pass",
	} {
		if got[id] != want {
			t.Fatalf("%s=%q, want %q (all: %v)", id, got[id], want, got)
		}
	}
}
