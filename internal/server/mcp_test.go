package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func callMCP(t *testing.T, s *Server, body string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("MCP-Protocol-Version", mcpVersion)
	r.RemoteAddr = "192.0.2.10:1234"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var result map[string]any
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatalf("non-JSON MCP response: %d %s", w.Code, w.Body.String())
		}
	}
	return w.Code, result
}

func TestMCPPublicReportFlow(t *testing.T) {
	s, scans, sender := setup(t)
	completed(t, s)
	status, init := callMCP(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}`)
	if status != 200 || init["error"] != nil || init["result"].(map[string]any)["protocolVersion"] != mcpVersion {
		t.Fatalf("initialize: %+v", init)
	}
	if status, _ := callMCP(t, s, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); status != 202 {
		t.Fatal(status)
	}
	_, tools := callMCP(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if len(tools["result"].(map[string]any)["tools"].([]any)) != 3 {
		t.Fatalf("tools=%+v", tools)
	}
	_, report := callMCP(t, s, `{"jsonrpc":"2.0","id":"report","method":"tools/call","params":{"name":"get_report","arguments":{"domain":"example.com"}}}`)
	result := report["result"].(map[string]any)
	if result["isError"] != false || result["structuredContent"].(map[string]any)["domain"] != "example.com" || scans.calls != 1 || sender.calls != 0 {
		t.Fatalf("report=%+v scans=%d texts=%d", report, scans.calls, sender.calls)
	}
	_, resource := callMCP(t, s, `{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"is-optimized://guide"}}`)
	if resource["error"] != nil {
		t.Fatal(resource)
	}
}

func TestMCPErrorsAndOrigin(t *testing.T) {
	s, _, _ := setup(t)
	for _, tc := range []struct {
		body string
		code float64
	}{
		{`{`, -32700},
		{`{"jsonrpc":"1.0","id":1,"method":"ping"}`, -32600},
		{`{"jsonrpc":"2.0","id":{},"method":"ping"}`, -32600},
		{`{"jsonrpc":"2.0","id":1,"method":"bogus"}`, -32601},
		{`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, -32602},
		{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"send_sms","arguments":{}}}`, -32602},
		{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_report","arguments":{"domain":"example.com","extra":"x"}}}`, -32602},
	} {
		_, got := callMCP(t, s, tc.body)
		if got["error"] == nil || got["error"].(map[string]any)["code"] != tc.code {
			t.Fatalf("%s: %+v", tc.body, got)
		}
	}
	r := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	if got := perform(s, "GET", "/mcp", ""); got.Code != 405 {
		t.Fatal(got.Code)
	}
}
