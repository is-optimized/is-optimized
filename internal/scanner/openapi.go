package scanner

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"golang.org/x/net/html"
	"gopkg.in/yaml.v3"
)

const openAPISpecURL = "https://spec.openapis.org/oas/latest.html"
const maxOpenAPIProbes = 3

var openAPIVersionPattern = regexp.MustCompile(`^3\.\d+\.\d+$`)

var errOpenAPIYAMLComplexity = errors.New("OpenAPI YAML aliases or complexity limit; format was not observed")

var openAPIMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

type openAPIStats struct {
	version3, validRoot, hasOperations, hasSecurity bool
	hasOAuth                                        bool
	operations, operationIDs, descriptions          int
	typedSuccess, typedError                        int
	scopedSchemes, versioned                        bool
}

func openAPIChecks(ss *session, home *url.URL, homepage response, page pageResult) []reportmodel.Finding {
	clue, candidates := openAPIHints(home, homepage, page)
	if !clue {
		return openAPIFindings(home.String(), nil, false, nil)
	}
	var chosen *url.URL
	var decoded map[string]any
	var lastErr error
	var lastEvidence string
	type probeResult struct {
		candidate *url.URL
		response  response
		err       error
	}
	results := make([]probeResult, len(candidates))
	var wg sync.WaitGroup
	for i, candidate := range candidates {
		wg.Add(1)
		go func(i int, candidate *url.URL) {
			defer wg.Done()
			var r response
			var err error
			if origin(candidate) == origin(home) {
				r, err = ss.probe(candidate.String(), origin(home))
			} else {
				r, err = ss.probeExternal(candidate.String())
			}
			results[i] = probeResult{candidate: candidate, response: r, err: err}
		}(i, candidate)
	}
	wg.Wait()
	for _, result := range results {
		candidate, r, err := result.candidate, result.response, result.err
		if err != nil {
			if lastErr == nil {
				lastErr = err
				lastEvidence = "OpenAPI document probe was not observed: " + err.Error()
			}
			continue
		}
		if r.code == http.StatusNotFound || r.code == http.StatusGone {
			if lastEvidence == "" {
				lastEvidence = fmt.Sprintf("%s returned HTTP %d", candidate.Path, r.code)
			}
			continue
		}
		if r.code != http.StatusOK {
			if lastEvidence == "" {
				lastEvidence = fmt.Sprintf("%s returned HTTP %d", candidate.Path, r.code)
			}
			if r.code >= http.StatusInternalServerError {
				if lastErr == nil {
					lastErr = fmt.Errorf("server error response")
				}
				lastEvidence = fmt.Sprintf("OpenAPI document was not observed: %s returned HTTP %d", candidate.Path, r.code)
			}
			continue
		}
		decoded, _, err = decodeOpenAPI(r.body)
		if err != nil {
			if chosen == nil {
				chosen = candidate
				if errors.Is(err, errOpenAPIYAMLComplexity) {
					lastEvidence = "OpenAPI document parsing was not observed: " + err.Error()
				} else {
					lastEvidence = "A public contract response was observed but was not valid OpenAPI JSON or YAML."
				}
			}
			continue
		}
		chosen = candidate
		lastEvidence = ""
		break
	}
	if chosen == nil {
		if lastErr != nil {
			return openAPIFindings(origin(home)+"/openapi.json", nil, true, &lastEvidence)
		}
		if lastEvidence == "" {
			lastEvidence = "API/developer documentation links were observed, but no public OpenAPI document was found at the linked or conventional paths."
		}
		return openAPIFindings(origin(home)+"/openapi.json", nil, true, &lastEvidence)
	}
	if lastEvidence != "" {
		return openAPIFindings(chosen.String(), decoded, true, &lastEvidence)
	}
	return openAPIFindings(chosen.String(), decoded, true, nil)
}

func openAPIHints(home *url.URL, homepage response, page pageResult) (bool, []*url.URL) {
	clue := false
	candidates := make([]*url.URL, 0, 8)
	seen := map[string]bool{}
	add := func(raw string) {
		u, ok := readinessLink(raw, home)
		if !ok || seen[u.String()] {
			return
		}
		seen[u.String()] = true
		candidates = append(candidates, u)
	}
	if page.doc != nil {
		walk(page.doc, func(n *html.Node) {
			if n.Type != html.ElementNode || (n.Data != "a" && n.Data != "link") {
				return
			}
			raw := strings.TrimSpace(attr(n, "href"))
			u, ok := readinessLink(raw, home)
			if !ok {
				return
			}
			path := strings.ToLower(u.Path)
			if strings.Contains(path, "openapi") || strings.Contains(path, "swagger") || (n.Data == "link" && hasToken(attr(n, "rel"), "service-desc")) {
				clue = true
				add(raw)
				return
			}
			if pathSegment(path, "api", "developer", "developers", "docs", "documentation") {
				clue = true
			}
		})
	}
	if homepage.header != nil {
		for _, value := range homepage.header.Values("Link") {
			lower := strings.ToLower(value)
			if !strings.Contains(lower, "service-desc") {
				continue
			}
			clue = true
			start, end := strings.Index(value, "<"), strings.Index(value, ">")
			if start >= 0 && end > start {
				add(value[start+1 : end])
			}
		}
	}
	if !clue {
		return false, nil
	}
	for _, path := range []string{"/openapi.json", "/openapi.yaml", "/swagger.json", "/api/openapi.json"} {
		add(origin(home) + path)
	}
	if len(candidates) > maxOpenAPIProbes {
		candidates = candidates[:maxOpenAPIProbes]
	}
	return true, candidates
}

func decodeOpenAPI(body []byte) (map[string]any, string, error) {
	var jsonDoc map[string]any
	if json.Unmarshal(body, &jsonDoc) == nil && jsonDoc != nil {
		return jsonDoc, "json", nil
	}
	var yamlDoc map[string]any
	var node yaml.Node
	if err := yaml.Unmarshal(body, &node); err != nil {
		return nil, "", fmt.Errorf("invalid or over-complex OpenAPI YAML document")
	}
	if err := validateYAMLNode(&node, 0, new(int)); err != nil {
		return nil, "", errOpenAPIYAMLComplexity
	}
	if node.Decode(&yamlDoc) != nil || yamlDoc == nil {
		return nil, "", fmt.Errorf("invalid OpenAPI YAML document")
	}
	if yamlDoc != nil {
		return yamlDoc, "yaml", nil
	}
	return nil, "", fmt.Errorf("invalid OpenAPI document")
}

func validateYAMLNode(node *yaml.Node, depth int, count *int) error {
	if node == nil {
		return nil
	}
	*count++
	if depth > 64 || *count > 100000 || node.Kind == yaml.AliasNode {
		return fmt.Errorf("YAML aliases or complexity limit exceeded")
	}
	for _, child := range node.Content {
		if err := validateYAMLNode(child, depth+1, count); err != nil {
			return err
		}
	}
	return nil
}

func assessOpenAPI(document map[string]any) openAPIStats {
	stats := openAPIStats{}
	version := stringValue(document["openapi"])
	stats.version3 = openAPIVersionPattern.MatchString(version)
	info := mapValue(document["info"])
	stats.versioned = stringValue(info["version"]) != ""
	_, hasComponents := document["components"]
	_, hasPaths := document["paths"]
	_, hasWebhooks := document["webhooks"]
	stats.validRoot = stats.version3 && stringValue(info["title"]) != "" && stats.versioned &&
		((hasComponents && isOpenAPIObject(document["components"])) || (hasPaths && isOpenAPIObject(document["paths"])) || (hasWebhooks && isOpenAPIObject(document["webhooks"])))
	paths := mapValue(document["paths"])
	for _, pathItemValue := range paths {
		pathItem := mapValue(pathItemValue)
		for method, operationValue := range pathItem {
			if !openAPIMethods[strings.ToLower(method)] {
				continue
			}
			operation := mapValue(operationValue)
			stats.operations++
			if stringValue(operation["operationId"]) != "" {
				stats.operationIDs++
			}
			if stringValue(operation["summary"]) != "" || stringValue(operation["description"]) != "" {
				stats.descriptions++
			}
			responses := mapValue(operation["responses"])
			for code, responseValue := range responses {
				resp := mapValue(responseValue)
				content := mapValue(resp["content"])
				typed := false
				for _, mediaValue := range content {
					if hasSchema(mapValue(mediaValue)["schema"]) {
						typed = true
						break
					}
				}
				if strings.HasPrefix(code, "2") && typed {
					stats.typedSuccess++
				}
				if (strings.HasPrefix(code, "4") || strings.HasPrefix(code, "5")) && typed {
					stats.typedError++
				}
			}
		}
	}
	components := mapValue(document["components"])
	schemes := mapValue(components["securitySchemes"])
	stats.hasSecurity = len(schemes) > 0
	for _, schemeValue := range schemes {
		scheme := mapValue(schemeValue)
		if stringValue(scheme["type"]) != "oauth2" {
			continue
		}
		stats.hasOAuth = true
		flows := mapValue(scheme["flows"])
		for _, flowValue := range flows {
			if len(mapValue(mapValue(flowValue)["scopes"])) > 0 {
				stats.scopedSchemes = true
			}
		}
	}
	stats.hasOperations = stats.operations > 0
	return stats
}

func isOpenAPIObject(value any) bool {
	_, ok := value.(map[string]any)
	return ok
}

func openAPIFindings(raw string, document map[string]any, applicable bool, note *string) []reportmodel.Finding {
	base := func(id, title, tier string, weight float64, remediation string) reportmodel.Finding {
		return reportmodel.Finding{ID: id, Pillar: "agent", Title: title, Status: "unknown", Weight: weight, Tier: tier, Stage: "act", URL: raw, Applicability: "Applies when the homepage links to a public API or developer surface; the scanner reads public contracts only and never calls mutations.", Remediation: remediation, Standards: []reportmodel.StandardReference{{ID: "OpenAPI 3", URL: openAPISpecURL, Coverage: "partial"}}}
	}
	snippet := `{"openapi":"3.1.0","info":{"title":"Public API","version":"v1"},"paths":{"/health":{"get":{"operationId":"getHealth","responses":{"200":{"description":"OK","content":{"application/json":{"schema":{"type":"object"}}}}}}}}}`
	findings := []reportmodel.Finding{
		base("agent.openapi.spec", "OpenAPI 3 contract", "essential", 3, "Publish a valid OpenAPI 3 JSON or YAML contract, for example: "+snippet),
		base("agent.openapi.operations", "OpenAPI operation quality", "recommended", 2, "Give each operation a stable operationId and a concise summary or description."),
		base("agent.openapi.errors", "Typed OpenAPI responses", "recommended", 1, "Describe JSON response bodies with schemas for successful and error responses."),
		base("agent.openapi.scopes", "Scoped API permissions", "essential", 1, "Describe named OAuth scopes in OpenAPI security schemes when API access is authenticated."),
		base("agent.openapi.versioning", "API version metadata", "recommended", 1, "Declare the API version in the OpenAPI info.version field and document supported compatibility policy."),
	}
	if !applicable {
		for i := range findings {
			findings[i].Status = "not_applicable"
			findings[i].Evidence = "No public API or developer-surface link was observed on the homepage."
		}
		return findings
	}
	if note != nil {
		if strings.Contains(*note, "not observed") {
			for i := range findings {
				findings[i].Status = "unknown"
				findings[i].Evidence = *note
			}
		} else {
			findings[0].Status = "fail"
			findings[0].Evidence = *note
			for i := 1; i < len(findings); i++ {
				findings[i].Status = "unknown"
				findings[i].Evidence = "Operation details were not evaluated because no valid OpenAPI contract was available."
			}
		}
		return findings
	}
	stats := assessOpenAPI(document)
	findings[0].Evidence = fmt.Sprintf("OpenAPI version %q observed; required info title and version present=%t", stringValue(document["openapi"]), stats.validRoot)
	if !stats.validRoot {
		findings[0].Status = "fail"
	} else {
		findings[0].Status = "pass"
	}
	findings[1].Evidence = fmt.Sprintf("%d/%d operations have operationId; %d/%d have summary or description", stats.operationIDs, stats.operations, stats.descriptions, stats.operations)
	if stats.operations == 0 || stats.operationIDs == 0 && stats.descriptions == 0 {
		findings[1].Status = "fail"
	} else if stats.operationIDs < stats.operations || stats.descriptions < stats.operations {
		findings[1].Status = "partial"
	} else {
		findings[1].Status = "pass"
	}
	findings[2].Evidence = fmt.Sprintf("%d typed 2xx responses and %d typed error/default responses observed", stats.typedSuccess, stats.typedError)
	if stats.typedSuccess == 0 && stats.typedError == 0 {
		findings[2].Status = "fail"
	} else if stats.typedSuccess == 0 || stats.typedError == 0 {
		findings[2].Status = "partial"
	} else {
		findings[2].Status = "pass"
	}
	findings[3].Evidence = fmt.Sprintf("Authentication security schemes present=%t; named OAuth scopes present=%t", stats.hasSecurity, stats.scopedSchemes)
	if !stats.hasOAuth {
		findings[3].Status = "not_applicable"
	} else if stats.scopedSchemes {
		findings[3].Status = "pass"
	} else {
		findings[3].Status = "fail"
	}
	findings[4].Evidence = fmt.Sprintf("OpenAPI info.version present=%t", stats.versioned)
	if stats.versioned {
		findings[4].Status = "pass"
	} else {
		findings[4].Status = "fail"
	}
	return findings
}

func mapValue(value any) map[string]any {
	if v, ok := value.(map[string]any); ok {
		return v
	}
	return nil
}

func stringValue(value any) string {
	if v, ok := value.(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func hasSchema(value any) bool {
	schema := mapValue(value)
	for _, key := range []string{"$ref", "type", "properties", "items", "oneOf", "anyOf", "allOf", "enum"} {
		if _, ok := schema[key]; ok {
			return true
		}
	}
	return false
}
