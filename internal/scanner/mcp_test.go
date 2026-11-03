package scanner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
)

func mcpFixtureSession(f roundTrip) *session {
	return &session{s: fixtureScanner(f), ctx: context.Background(), policies: map[string]policy{}}
}

func mcpStatuses(findings []reportmodel.Finding) map[string]string {
	out := map[string]string{}
	for _, f := range findings {
		out[f.ID] = f.Status
	}
	return out
}

func TestMCPAuditNegotiationAndReadOnlyAllowlist(t *testing.T) {
	home, _ := url.Parse("https://example.com/")
	var calls []string
	ss := mcpFixtureSession(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/robots.txt" {
			return reply(r, 404, "", "text/plain"), nil
		}
		if r.Method != "POST" || r.URL.Path != "/mcp" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Accept") != "application/json, text/event-stream" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatal("missing Streamable HTTP headers")
		}
		var request struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Fatal("invalid request")
		}
		calls = append(calls, request.Method)
		if request.Method != "initialize" && (r.Header.Get("Mcp-Session-Id") != "opaque-fixture-session" || r.Header.Get("MCP-Protocol-Version") != mcpProtocolVersion) {
			t.Fatal("negotiated session/version missing")
		}
		if request.Method == "notifications/initialized" {
			return reply(r, 202, "", ""), nil
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": mcpProtocolVersion, "serverInfo": map[string]string{"name": "fixture", "version": "1.0"}, "instructions": "Read public documentation", "capabilities": map[string]any{"tools": map[string]any{}, "resources": map[string]any{}, "prompts": map[string]any{}}}
		case "tools/list":
			if string(request.Params) == "false" {
				body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "error": map[string]any{"code": -32602, "message": "Invalid params"}})
				return reply(r, 400, string(body), "application/json"), nil
			}
			result = map[string]any{"tools": []any{map[string]any{"name": "read_docs", "description": "Retrieve public first-party documentation pages.", "inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"readOnlyHint": true}}}}
		case "resources/list":
			result = map[string]any{"resources": []any{map[string]any{"uri": "docs://overview", "name": "Overview", "mimeType": "text/markdown"}}}
		case "prompts/list":
			result = map[string]any{"prompts": []any{map[string]any{"name": "help"}}}
		default:
			t.Fatalf("mutating or unexpected method %s", request.Method)
		}
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": result})
		resp := reply(r, 200, string(body), "application/json")
		if request.Method == "initialize" {
			resp.Header.Set("Mcp-Session-Id", "opaque-fixture-session")
		}
		return resp, nil
	})
	page := readinessPage(t, `<a href="/mcp">MCP</a>`)
	findings := mcpChecks(ss, home, response{code: 200}, page)
	for _, f := range findings {
		want := "pass"
		if f.ID == "agent.mcp.apps" {
			want = "not_applicable"
		}
		if f.Status != want {
			t.Errorf("%s=%s evidence=%s", f.ID, f.Status, f.Evidence)
		}
		if f.Applicability == "" || f.Remediation == "" || len(f.Standards) == 0 {
			t.Errorf("incomplete check contract: %+v", f)
		}
	}
	want := []string{"initialize", "notifications/initialized", "tools/list", "resources/list", "prompts/list", "tools/list"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls=%v", calls)
	}
	if ss.requests.Load() != 7 || ss.discovery.Load() != 7 {
		t.Fatalf("requests=%d discovery=%d", ss.requests.Load(), ss.discovery.Load())
	}
}

func TestMCPDiscoveryApplicabilityAndBlockedTargets(t *testing.T) {
	home, _ := url.Parse("https://example.com/")
	for _, tc := range []struct {
		name, html, card, want string
		code                   int
	}{
		{"absent", `<main>Business</main>`, "", "not_applicable", 404},
		{"unavailable card", `<main>Business</main>`, "", "unknown", 503},
		{"private advertised", `<a href="https://127.0.0.1/mcp">MCP</a>`, "", "not_applicable", 404},
		{"card endpoint", `<main>Business</main>`, `{"remotes":[{"type":"streamable-http","url":"https://example.com/mcp"}]}`, "unknown", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts := 0
			ss := mcpFixtureSession(func(r *http.Request) (*http.Response, error) {
				if r.Method == "POST" {
					posts++
					return reply(r, 401, "", "text/plain"), nil
				}
				if r.URL.Path == "/robots.txt" {
					return reply(r, 404, "", "text/plain"), nil
				}
				return reply(r, tc.code, tc.card, "application/json"), nil
			})
			out := mcpChecks(ss, home, response{code: 200}, readinessPage(t, tc.html))
			if out[0].Status != tc.want {
				t.Fatalf("status=%s evidence=%s", out[0].Status, out[0].Evidence)
			}
			if tc.name != "card endpoint" && posts != 0 {
				t.Fatal("unexpected MCP POST")
			}
		})
	}
	ss := mcpFixtureSession(func(r *http.Request) (*http.Response, error) {
		t.Fatal("homepage missing must not probe")
		return nil, nil
	})
	if out := mcpChecks(ss, home, response{}, pageResult{}); out[0].Status != "unknown" {
		t.Fatal(out)
	}
}

func TestMCPTransportGuards(t *testing.T) {
	public, _ := url.Parse("https://example.com/mcp")
	for _, tc := range []struct {
		name, endpoint, method string
		budget                 int32
		robots                 string
		redirect               bool
	}{
		{"mutation rejected", public.String(), "tools/call", 0, "", false},
		{"private rejected", "https://127.0.0.1/mcp", "initialize", 0, "", false},
		{"HTTP rejected", "http://example.com/mcp", "initialize", 0, "", false},
		{"query rejected", "https://example.com/mcp?token=secret", "initialize", 0, "", false},
		{"budget", public.String(), "initialize", maxDiscoveryCount, "", false},
		{"robots", public.String(), "initialize", 0, "User-agent: *\nDisallow: /mcp", false},
		{"redirect", public.String(), "initialize", 0, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts := 0
			ss := mcpFixtureSession(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/robots.txt" {
					if tc.robots != "" {
						return reply(r, 200, tc.robots, "text/plain"), nil
					}
					return reply(r, 404, "", "text/plain"), nil
				}
				posts++
				res := reply(r, 302, "", "text/plain")
				res.Header.Set("Location", "https://other.example.com/mcp")
				return res, nil
			})
			ss.discovery.Store(tc.budget)
			u, _ := url.Parse(tc.endpoint)
			if _, _, err := mcpPost(ss, u, mcpMessage{JSONRPC: "2.0", ID: mcpID(1), Method: tc.method}, "", ""); err == nil {
				t.Fatal("guard should reject")
			}
			if posts > 1 || (!tc.redirect && posts > 0) {
				t.Fatalf("unsafe request count %d", posts)
			}
		})
	}
}

func TestMCPJSONRPCValidation(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"jsonrpc":"2.0","id":1,"result":{}}`, true},
		{`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid request"}}`, true},
		{`{"jsonrpc":"2.0","id":2,"result":{}}`, false},
		{`{"jsonrpc":"2.0","id":1,"result":{},"error":{"code":1,"message":"bad"}}`, false},
		{`{"jsonrpc":"2.0","id":1,"error":{"message":"missing code"}}`, false},
		{`{"jsonrpc":"2.0","id":1,"error":{"code":null,"message":"bad"}}`, false},
		{`{"jsonrpc":"2.0","id":1,"result":null}`, false},
		{`[{"jsonrpc":"2.0","id":1,"result":{}}]`, false},
	} {
		if _, ok := decodeMCPResponse([]byte(tc.body), 1); ok != tc.valid {
			t.Errorf("valid=%t body=%s", ok, tc.body)
		}
	}
}

type mcpOpenSSE struct {
	io.Reader
	done chan struct{}
}

func (r *mcpOpenSSE) Read(p []byte) (int, error) {
	n, e := r.Reader.Read(p)
	if e == io.EOF {
		<-r.done
	}
	return n, e
}
func (r *mcpOpenSSE) Close() error { close(r.done); return nil }

func TestMCPSSEStopsOnMatchingResponse(t *testing.T) {
	u, _ := url.Parse("https://example.com/mcp")
	ss := mcpFixtureSession(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/robots.txt" {
			return reply(r, 404, "", "text/plain"), nil
		}
		res := reply(r, 200, "", "text/event-stream")
		res.Body = &mcpOpenSSE{Reader: strings.NewReader("event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\"}\n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n"), done: make(chan struct{})}
		return res, nil
	})
	_, env, err := mcpPost(ss, u, mcpMessage{JSONRPC: "2.0", ID: mcpID(1), Method: "initialize"}, "", "")
	if err != nil || env == nil {
		t.Fatalf("SSE response missing err=%v", err)
	}
}

func TestMCPToolAndResourceVerdicts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tools []map[string]any
		want  string
		more  bool
	}{
		{"empty", nil, "fail", false},
		{"invalid", []map[string]any{{"name": "bad tool"}}, "fail", false},
		{"partial", []map[string]any{{"name": "good", "description": "A sufficiently long public description.", "inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"readOnlyHint": false}}, {"name": "bad"}}, "partial", false},
		{"paginated", []map[string]any{{"name": "good", "description": "A sufficiently long public description.", "inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"readOnlyHint": false}}}, "partial", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := make([]reportmodel.Finding, 9)
			assessMCPTools(out, tc.tools, tc.more)
			if out[1].Status != tc.want || out[2].Status != tc.want || out[3].Status != tc.want {
				t.Fatal(mcpStatuses(out))
			}
		})
	}
	out := make([]reportmodel.Finding, 9)
	assessMCPResources(out, []map[string]any{{"uri": "ui://app/view", "name": "App", "mimeType": "text/html;profile=mcp-app"}}, false)
	if out[4].Status != "pass" || out[5].Status != "pass" || out[8].Status != "unknown" {
		t.Fatal(out)
	}
}
