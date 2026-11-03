package scanner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"golang.org/x/net/html"
)

const mcpProtocolVersion = "2025-11-25"
const mcpSpecURL = "https://modelcontextprotocol.io/specification/2025-11-25"

// mcpMessage is deliberately limited to the read-only protocol surface. No
// tools/call, resource reads, credentials, client sampling or elicitation exist.
type mcpMessage struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int   `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type mcpEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func mcpID(n int) *int { return &n }

// mcpPost only accepts the audit's explicit allowlist. Redirects are not
// followed: session identifiers and POST bodies never move to a different URL.
// The normal secure transport vets DNS on every dial; test transports remain
// injectable through Scanner as with the existing GET probes.
func mcpPost(ss *session, endpoint *url.URL, msg mcpMessage, sessionID, version string) (response, *mcpEnvelope, error) {
	var out response
	switch msg.Method {
	case "initialize", "notifications/initialized", "tools/list", "resources/list", "prompts/list":
	default:
		return out, nil, errors.New("MCP audit method is not allowed")
	}
	if endpoint == nil || validateURL(endpoint) != nil || endpoint.RawQuery != "" {
		return out, nil, errors.New("MCP endpoint is not a public HTTP URL without query credentials")
	}
	if endpoint.Scheme != "https" {
		return out, nil, errors.New("MCP audit requires HTTPS")
	}
	p := ss.getPolicy(endpoint)
	robotPath := endpoint.EscapedPath()
	if robotPath == "" {
		robotPath = "/"
	}
	if p.err != nil || !p.rules.allowed(robotPath) {
		return out, nil, errors.New("MCP probe blocked by unavailable or disallowing robots policy")
	}
	if ss.discovery.Add(1) > maxDiscoveryCount {
		ss.discoveryBudgetExhaustions.Add(1)
		return out, nil, errors.New("discovery budget exhausted")
	}
	if ss.requests.Add(1) > maxRequestCount {
		ss.requestBudgetExhaustions.Add(1)
		return out, nil, errors.New("scan request budget exhausted")
	}
	if ss.bytes.Load() >= maxScanBytes {
		ss.scanByteBudgetExhaustions.Add(1)
		return out, nil, errScanByteBudget
	}
	body, err := json.Marshal(msg)
	if err != nil || len(body) > 16<<10 {
		return out, nil, errors.New("MCP audit request exceeds limits")
	}
	ctx, cancel := context.WithTimeout(ss.ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return out, nil, err
	}
	req.Header.Set("User-Agent", userAgent+"/1.0 (+https://is-optimized.com)")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	if version != "" {
		req.Header.Set("MCP-Protocol-Version", version)
	}
	select {
	case ss.s.slots <- struct{}{}:
	case <-ctx.Done():
		return out, nil, ctx.Err()
	}
	defer func() { <-ss.s.slots }()
	r, err := ss.s.transport.RoundTrip(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			ss.timeouts.Add(1)
		}
		return out, nil, errors.New("MCP transport not observed")
	}
	defer r.Body.Close()
	out = response{url: endpoint, code: r.StatusCode, contentType: r.Header.Get("Content-Type"), header: r.Header.Clone()}
	if r.StatusCode >= 300 && r.StatusCode < 400 {
		return out, nil, errors.New("MCP POST redirect was not followed")
	}
	if msg.ID == nil {
		if r.StatusCode != http.StatusAccepted {
			return out, nil, errors.New("MCP initialized notification was not accepted with HTTP 202")
		}
		return out, nil, nil
	}
	if r.StatusCode != http.StatusOK && r.StatusCode != http.StatusBadRequest {
		return out, nil, fmt.Errorf("MCP response not observed: HTTP %d", r.StatusCode)
	}
	media, _, _ := mime.ParseMediaType(out.contentType)
	if media == "text/event-stream" {
		// Stop as soon as the matching response arrives. Waiting for EOF would
		// incorrectly time out a server that keeps the SSE connection open.
		reader := &mcpBudgetReader{ss: ss, source: r.Body, remaining: maxBody}
		scan := bufio.NewScanner(reader)
		scan.Buffer(make([]byte, 4096), maxBody)
		var data strings.Builder
		for scan.Scan() {
			line := strings.TrimSuffix(scan.Text(), "\r")
			if line == "" {
				if envelope, ok := decodeMCPResponse([]byte(data.String()), *msg.ID); ok {
					return out, envelope, nil
				}
				data.Reset()
			} else if strings.HasPrefix(line, "data:") {
				v := strings.TrimPrefix(line, "data:")
				v = strings.TrimPrefix(v, " ")
				if data.Len()+len(v) > maxBody {
					return out, nil, errors.New("MCP event exceeded size limit")
				}
				data.WriteString(v)
				data.WriteByte('\n')
			}
		}
		if scan.Err() != nil {
			return out, nil, errors.New("MCP SSE response was not fully observed")
		}
		return out, nil, errors.New("MCP SSE stream had no matching response")
	}
	if media != "application/json" {
		return out, nil, errors.New("MCP response media type is not JSON or SSE")
	}
	out.body, err = readBudgeted(r.Body, &ss.bytes, maxBody+1, maxScanBytes)
	if err != nil || len(out.body) > maxBody {
		return out, nil, errors.New("MCP response exceeded size or scan budget, or was incomplete")
	}
	envelope, ok := decodeMCPResponse(out.body, *msg.ID)
	if !ok {
		return out, nil, errors.New("MCP response was not a matching JSON-RPC response")
	}
	return out, envelope, nil
}

type mcpBudgetReader struct {
	ss        *session
	source    io.Reader
	remaining int64
}

func (r *mcpBudgetReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, errors.New("MCP stream exceeds response limit")
	}
	want := min(int64(len(p)), r.remaining)
	reserved := reserveScanBytes(&r.ss.bytes, want, maxScanBytes)
	if reserved == 0 {
		r.ss.scanByteBudgetExhaustions.Add(1)
		return 0, errScanByteBudget
	}
	n, err := r.source.Read(p[:int(reserved)])
	r.ss.bytes.Add(int64(n) - reserved)
	r.remaining -= int64(n)
	return n, err
}

func decodeMCPResponse(data []byte, id int) (*mcpEnvelope, bool) {
	var raw map[string]json.RawMessage
	var out mcpEnvelope
	if json.Unmarshal(data, &raw) != nil || json.Unmarshal(data, &out) != nil || out.JSONRPC != "2.0" {
		return nil, false
	}
	var got int
	if json.Unmarshal(out.ID, &got) != nil || got != id {
		return nil, false
	}
	_, result := raw["result"]
	_, rpcError := raw["error"]
	if result == rpcError || (result && string(out.Result) == "null") {
		return nil, false
	}
	if rpcError {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw["error"], &fields) != nil || out.Error == nil || strings.TrimSpace(out.Error.Message) == "" {
			return nil, false
		}
		if _, ok := fields["code"]; !ok {
			return nil, false
		}
		if string(fields["code"]) == "null" {
			return nil, false
		}
	}
	return &out, true
}

func mcpFinding(id, title, tier string, weight float64, endpoint string) reportmodel.Finding {
	return reportmodel.Finding{ID: "agent.mcp." + id, Pillar: "agent", Title: title, Status: "unknown", Weight: weight, Tier: tier, Stage: "act", URL: endpoint,
		Applicability: "Applies to advertised public MCP interfaces; only initialization and read-only list methods are probed. Authentication and tool execution are not attempted.",
		Remediation:   "Publish a Streamable HTTP MCP endpoint with negotiated capabilities and JSON-RPC errors. Example: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2025-11-25\",\"capabilities\":{},\"serverInfo\":{\"name\":\"example\",\"version\":\"1.0.0\"}}}",
		Standards:     []reportmodel.StandardReference{{ID: "MCP 2025-11-25", URL: mcpSpecURL, Coverage: "partial"}}}
}

func mcpChecks(ss *session, home *url.URL, homepage response, page pageResult) []reportmodel.Finding {
	endpoint := mcpEndpoint(home, page)
	base := origin(home) + "/mcp"
	out := []reportmodel.Finding{
		mcpFinding("server", "MCP server identity and transport", "recommended", 2, base),
		mcpFinding("tools", "MCP tool naming and descriptions", "recommended", 1, base),
		mcpFinding("schemas", "MCP tool input schemas", "recommended", 1, base),
		mcpFinding("annotations", "MCP tool behavior annotations", "recommended", 1, base),
		mcpFinding("resources", "MCP resource listing", "essential", 2, base),
		mcpFinding("resource_quality", "MCP resource metadata quality", "recommended", 1, base),
		mcpFinding("prompts", "MCP prompt listing", "bonus", 1, base),
		mcpFinding("errors", "MCP JSON-RPC error shape", "recommended", 1, base),
		mcpFinding("apps", "MCP Apps resource metadata", "bonus", 1, base),
	}
	if homepage.code == 0 {
		setMCPStatus(out, "unknown", "Homepage unavailable; MCP discovery was not observed.")
		return out
	}
	if endpoint == nil {
		// A conventional public server-card is a discovery hint, not proof of
		// server support or of a standardized card schema.
		card, err := ss.probe(origin(home)+"/.well-known/mcp/server-card.json", origin(home))
		if err != nil {
			setMCPStatus(out, "unknown", "MCP server-card discovery was not observed: "+err.Error())
			return out
		}
		if card.code != http.StatusOK && card.code != http.StatusNotFound && card.code != http.StatusGone {
			setMCPStatus(out, "unknown", fmt.Sprintf("MCP server-card discovery was not observed: HTTP %d.", card.code))
			return out
		}
		if card.code == 200 {
			endpoint = mcpCardEndpoint(card.body, home)
		}
		if endpoint == nil {
			setMCPStatus(out, "not_applicable", "No advertised MCP endpoint or usable public server-card endpoint was observed.")
			return out
		}
	}
	for i := range out {
		out[i].URL = endpoint.String()
	}
	setMCPStatus(out, "unknown", "MCP initialization or negotiated list response was not observed.")
	r, init, err := mcpPost(ss, endpoint, mcpMessage{JSONRPC: "2.0", ID: mcpID(1), Method: "initialize", Params: map[string]any{"protocolVersion": mcpProtocolVersion, "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "is-optimized-audit", "version": "1.0.0"}}}, "", "")
	if err != nil {
		setMCPStatus(out, "unknown", "MCP initialization not observed: "+err.Error())
		return out
	}
	if init.Error != nil {
		out[0].Status = "fail"
		out[0].Evidence = fmt.Sprintf("Initialize returned JSON-RPC error code %d; no list calls were made.", init.Error.Code)
		return out
	}
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
		Instructions string                     `json:"instructions"`
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	if json.Unmarshal(init.Result, &result) != nil || result.Capabilities == nil {
		out[0].Status = "fail"
		out[0].Evidence = "Initialize result did not contain a capabilities object."
		return out
	}
	if strings.TrimSpace(result.ServerInfo.Name) == "" || strings.TrimSpace(result.ServerInfo.Version) == "" {
		out[0].Status = "fail"
		out[0].Evidence = "Initialize result was missing required serverInfo.name or serverInfo.version; list calls were not attempted."
		return out
	}
	version := result.ProtocolVersion
	if version != "2025-11-25" && version != "2025-06-18" && version != "2025-03-26" {
		out[0].Evidence = "Server negotiated an unsupported protocol version; list calls were not attempted."
		return out
	}
	sid := r.header.Get("Mcp-Session-Id")
	if len(sid) > 1024 || strings.IndexFunc(sid, func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
		out[0].Status = "fail"
		out[0].Evidence = "Server supplied an invalid session identifier; it was not reused."
		return out
	}
	_, _, err = mcpPost(ss, endpoint, mcpMessage{JSONRPC: "2.0", Method: "notifications/initialized"}, sid, version)
	if err != nil {
		out[0].Evidence = "Initialization notification was not accepted; list calls were not attempted."
		return out
	}
	out[0].Status = "pass"
	if strings.TrimSpace(result.ServerInfo.Name) == "" || strings.TrimSpace(result.ServerInfo.Version) == "" || strings.TrimSpace(result.Instructions) == "" {
		out[0].Status = "partial"
	}
	out[0].Evidence = fmt.Sprintf("Streamable HTTP initialization negotiated %s; name=%t, version=%t, instructions=%t. Identity is self-declared; no tool was executed.", version, result.ServerInfo.Name != "", result.ServerInfo.Version != "", result.Instructions != "")
	for _, group := range []struct {
		cap, method, key string
		indices          []int
		id               int
	}{{"tools", "tools/list", "tools", []int{1, 2, 3}, 2}, {"resources", "resources/list", "resources", []int{4, 5, 8}, 3}, {"prompts", "prompts/list", "prompts", []int{6}, 4}} {
		if _, ok := result.Capabilities[group.cap]; !ok {
			for _, i := range group.indices {
				out[i].Status = "not_applicable"
				out[i].Evidence = "Server did not advertise the optional " + group.cap + " capability."
			}
			continue
		}
		_, envelope, e := mcpPost(ss, endpoint, mcpMessage{JSONRPC: "2.0", ID: mcpID(group.id), Method: group.method, Params: map[string]any{}}, sid, version)
		if e != nil || envelope.Error != nil {
			for _, i := range group.indices {
				out[i].Evidence = "Advertised " + group.method + " response not observed or returned an RPC error."
			}
			continue
		}
		var listing map[string]json.RawMessage
		var entries []map[string]any
		if json.Unmarshal(envelope.Result, &listing) != nil || json.Unmarshal(listing[group.key], &entries) != nil || len(entries) > 1000 {
			for _, i := range group.indices {
				out[i].Status = "fail"
				out[i].Evidence = "List result was missing the expected array or exceeded the 1,000-entry audit limit."
			}
			continue
		}
		partial := len(listing["nextCursor"]) > 0 && string(listing["nextCursor"]) != "null" && string(listing["nextCursor"]) != "\"\""
		switch group.cap {
		case "tools":
			assessMCPTools(out, entries, partial)
		case "resources":
			assessMCPResources(out, entries, partial)
		case "prompts":
			out[6].Status = "pass"
			if len(entries) == 0 || partial {
				out[6].Status = "partial"
			}
			out[6].Evidence = fmt.Sprintf("%d prompt descriptors listed; additional pages=%t. Prompt bodies were not requested.", len(entries), partial)
		}
	}
	// One deliberately malformed, negotiated read-only list request checks the
	// error envelope. No unknown method or callable tool name is ever sent.
	errorMethod := ""
	for _, capability := range []string{"tools", "resources", "prompts"} {
		if _, ok := result.Capabilities[capability]; ok {
			errorMethod = capability + "/list"
			break
		}
	}
	if errorMethod == "" {
		out[7].Status = "not_applicable"
		out[7].Evidence = "No read-only list capability was negotiated; malformed-list testing was not attempted."
		return out
	}
	_, bad, e := mcpPost(ss, endpoint, mcpMessage{JSONRPC: "2.0", ID: mcpID(99), Method: errorMethod, Params: false}, sid, version)
	if e != nil {
		out[7].Evidence = "Malformed read-only list response was not observed: " + e.Error()
	}
	if e == nil {
		out[7].Status = "fail"
		out[7].Evidence = "Malformed read-only list params did not return a JSON-RPC error."
		if bad.Error != nil {
			out[7].Status = "pass"
			out[7].Evidence = fmt.Sprintf("Malformed read-only list request returned a matching JSON-RPC error with numeric code %d and a message.", bad.Error.Code)
		}
	}
	return out
}

func setMCPStatus(out []reportmodel.Finding, status, evidence string) {
	for i := range out {
		out[i].Status = status
		out[i].Evidence = evidence
	}
}

func mcpEndpoint(home *url.URL, page pageResult) *url.URL {
	var found *url.URL
	if page.doc == nil {
		return nil
	}
	walk(page.doc, func(n *html.Node) {
		if found != nil || n.Type != html.ElementNode || (n.Data != "a" && n.Data != "link") {
			return
		}
		u, ok := readinessLink(attr(n, "href"), home)
		if !ok || u.Scheme != "https" || u.RawQuery != "" {
			return
		}
		path := strings.TrimSuffix(strings.ToLower(u.Path), "/")
		if path == "/mcp" || strings.HasSuffix(path, "/mcp") || hasToken(attr(n, "rel"), "mcp-server") {
			found = u
		}
	})
	return found
}

func mcpCardEndpoint(body []byte, home *url.URL) *url.URL {
	var card map[string]any
	if len(body) > 128<<10 || json.Unmarshal(body, &card) != nil {
		return nil
	}
	candidates := []string{}
	for _, key := range []string{"url", "endpoint"} {
		if s, ok := card[key].(string); ok {
			candidates = append(candidates, s)
		}
	}
	if transport, ok := card["transport"].(map[string]any); ok {
		if s, ok := transport["url"].(string); ok {
			candidates = append(candidates, s)
		}
	}
	if remotes, ok := card["remotes"].([]any); ok {
		for _, item := range remotes {
			if remote, ok := item.(map[string]any); ok && remote["type"] == "streamable-http" {
				if s, ok := remote["url"].(string); ok {
					candidates = append(candidates, s)
				}
			}
		}
	}
	for _, raw := range candidates {
		if u, ok := readinessLink(raw, home); ok && u.Scheme == "https" && u.RawQuery == "" {
			return u
		}
	}
	return nil
}

var mcpToolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

func assessMCPTools(out []reportmodel.Finding, tools []map[string]any, more bool) {
	quality, schemas, annotations := 0, 0, 0
	seen := map[string]bool{}
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		description, _ := tool["description"].(string)
		if mcpToolNamePattern.MatchString(name) && !seen[name] && len(strings.TrimSpace(description)) >= 30 {
			quality++
		}
		seen[name] = true
		if schema, ok := tool["inputSchema"].(map[string]any); ok && schema["type"] == "object" {
			schemas++
		}
		if hints, ok := tool["annotations"].(map[string]any); ok {
			if _, ok := hints["readOnlyHint"].(bool); ok {
				annotations++
			}
		}
	}
	for _, check := range []struct {
		i, n        int
		description string
	}{{1, quality, "unique valid names and descriptions >=30 characters"}, {2, schemas, "object inputSchema declarations (not full JSON Schema validation)"}, {3, annotations, "explicit boolean readOnlyHint annotations (advisory, not execution proof)"}} {
		out[check.i].Status = "fail"
		if check.n > 0 {
			out[check.i].Status = "partial"
		}
		if len(tools) > 0 && check.n == len(tools) && !more {
			out[check.i].Status = "pass"
		}
		out[check.i].Evidence = fmt.Sprintf("%d/%d sampled tools have %s; additional pages=%t; focused-surface observation: %d tools (not a universal threshold).", check.n, len(tools), check.description, more, len(tools))
	}
}

func assessMCPResources(out []reportmodel.Finding, resources []map[string]any, more bool) {
	valid, apps := 0, 0
	for _, resource := range resources {
		uri, _ := resource["uri"].(string)
		name, _ := resource["name"].(string)
		kind, _ := resource["mimeType"].(string)
		u, err := url.Parse(uri)
		media, _, mediaErr := mime.ParseMediaType(kind)
		if err == nil && u.Scheme != "" && strings.TrimSpace(name) != "" && mediaErr == nil && media != "" {
			valid++
		}
		if strings.HasPrefix(uri, "ui://") && (media == "text/html" || media == "text/html+mcp") {
			apps++
		}
	}
	out[4].Status = "fail"
	if len(resources) > 0 {
		out[4].Status = "pass"
	}
	if more && len(resources) > 0 {
		out[4].Status = "partial"
	}
	out[4].Evidence = fmt.Sprintf("%d public resource descriptors listed; additional pages=%t. Resource content was not read.", len(resources), more)
	out[5].Status = "fail"
	if valid > 0 {
		out[5].Status = "partial"
	}
	if valid == len(resources) && valid > 0 && !more {
		out[5].Status = "pass"
	}
	out[5].Evidence = fmt.Sprintf("%d/%d descriptors have an absolute URI, name, and parseable MIME type. Non-empty content was not verified because resources/read is outside the audit allowlist.", valid, len(resources))
	out[8].Status = "not_applicable"
	if apps > 0 {
		out[8].Status = "unknown"
	}
	out[8].Evidence = fmt.Sprintf("%d UI resource descriptors observed. View HTML, CSP, and color-scheme require resource reads and were not verified.", apps)
}
