package scanner

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"golang.org/x/net/html"
)

const (
	oauthMetadataRFC     = "https://www.rfc-editor.org/rfc/rfc8414.html"
	protectedResourceRFC = "https://www.rfc-editor.org/rfc/rfc9728.html"
	oidcDiscoverySpec    = "https://openid.net/specs/openid-connect-discovery-1_0.html"
)

func oauthChecks(ss *session, home *url.URL, page pageResult) []reportmodel.Finding {
	applicable := page.doc != nil && hasAuthSurface(page.doc, home)
	if !applicable {
		findings := oauthFindings(origin(home)+"/.well-known/oauth-authorization-server", nil, false, nil)
		return append(findings, oauthResourceFinding(nil, home, false, nil))
	}
	var lastErr error
	var evidence string
	for _, path := range []string{"/.well-known/oauth-authorization-server", "/.well-known/openid-configuration"} {
		candidate := origin(home) + path
		r, err := ss.probe(candidate, origin(home))
		if err != nil {
			lastErr = err
			evidence = "OAuth/OIDC metadata was not observed: " + err.Error()
			continue
		}
		if r.code == http.StatusNotFound || r.code == http.StatusGone {
			evidence = fmt.Sprintf("%s returned HTTP %d", path, r.code)
			continue
		}
		if r.code != http.StatusOK {
			evidence = fmt.Sprintf("%s returned HTTP %d", path, r.code)
			if r.code >= http.StatusInternalServerError {
				lastErr = fmt.Errorf("server error response")
				evidence = fmt.Sprintf("OAuth/OIDC metadata was not observed: %s returned HTTP %d", path, r.code)
			}
			continue
		}
		var metadata map[string]any
		if err := json.Unmarshal(r.body, &metadata); err != nil || metadata == nil {
			invalid := "OAuth/OIDC metadata response was not a JSON object."
			findings := oauthFindings(candidate, nil, true, &invalid)
			return append(findings, oauthResourceFinding(ss, home, true, nil))
		}
		findings := oauthFindings(candidate, metadata, true, nil)
		return append(findings, oauthResourceFinding(ss, home, true, nil))
	}
	if lastErr != nil {
		findings := oauthFindings(origin(home)+"/.well-known/oauth-authorization-server", nil, true, &evidence)
		return append(findings, oauthResourceFinding(ss, home, true, &evidence))
	}
	if evidence == "" {
		evidence = "Authentication links were observed, but no public OAuth/OIDC metadata document was found."
	}
	findings := oauthFindings(origin(home)+"/.well-known/oauth-authorization-server", nil, true, &evidence)
	return append(findings, oauthResourceFinding(ss, home, true, nil))
}

func hasAuthSurface(doc *html.Node, home *url.URL) bool {
	found := false
	walk(doc, func(n *html.Node) {
		if found || n.Type != html.ElementNode || (n.Data != "a" && n.Data != "link") {
			return
		}
		u, ok := readinessLink(attr(n, "href"), home)
		if !ok {
			return
		}
		path := strings.ToLower(u.Path)
		label := strings.ToLower(text(n))
		found = pathSegment(path, "oauth", "oauth2", "openid", "authentication", "authorization", "api") || strings.Contains(path, "oauth") || (pathSegment(path, "docs", "documentation", "developers") && strings.Contains(label, "auth"))
	})
	return found
}

func oauthFindings(raw string, metadata map[string]any, applicable bool, note *string) []reportmodel.Finding {
	base := func(id, title, tier string, weight float64, spec, remediation string) reportmodel.Finding {
		return reportmodel.Finding{
			ID: id, Pillar: "agent", Title: title, Status: "unknown", Weight: weight, Tier: tier, Stage: "act", URL: raw,
			Applicability: "Applies when the homepage links to an authentication surface; the audit reads public discovery metadata only and never registers clients or exchanges tokens.",
			Remediation:   remediation, Standards: []reportmodel.StandardReference{{ID: "OAuth/OIDC discovery", URL: spec, Coverage: "partial"}},
		}
	}
	findings := []reportmodel.Finding{
		base("agent.oauth.discovery", "OAuth/OIDC metadata discovery", "essential", 2, oauthMetadataRFC, "Publish standards-compliant OAuth authorization-server or OpenID Connect discovery metadata with an issuer and authorization endpoint."),
		base("agent.oauth.pkce", "OAuth S256 PKCE support", "essential", 2, oauthMetadataRFC, `Advertise "code_challenge_methods_supported": ["S256"] in authorization-server metadata.`),
		base("agent.oauth.scopes", "OAuth scope discovery", "recommended", 1, oidcDiscoverySpec, `Advertise supported scopes in a "scopes_supported" array so clients can request least-privilege access.`),
	}
	if !applicable {
		for i := range findings {
			findings[i].Status = "not_applicable"
			findings[i].Evidence = "No public authentication surface was linked from the homepage."
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
				findings[i].Evidence = "OAuth capabilities were not evaluated because usable metadata was unavailable."
			}
		}
		return findings
	}
	issuer := stringValue(metadata["issuer"])
	authorizationEndpoint := stringValue(metadata["authorization_endpoint"])
	tokenEndpoint := stringValue(metadata["token_endpoint"])
	findings[0].Evidence = fmt.Sprintf("issuer=%t, authorization_endpoint=%t, token_endpoint=%t", absolutePublicURL(issuer), absolutePublicURL(authorizationEndpoint), absolutePublicURL(tokenEndpoint))
	if absolutePublicURL(issuer) && absolutePublicURL(authorizationEndpoint) {
		findings[0].Status = "pass"
	} else {
		findings[0].Status = "fail"
	}
	findings[1].Evidence = fmt.Sprintf("code_challenge_methods_supported includes S256=%t", stringArrayIncludes(metadata["code_challenge_methods_supported"], "S256"))
	if stringArrayIncludes(metadata["code_challenge_methods_supported"], "S256") {
		findings[1].Status = "pass"
	} else {
		findings[1].Status = "fail"
	}
	scopes := metadata["scopes_supported"]
	findings[2].Evidence = fmt.Sprintf("%d supported scope(s) declared", len(stringArray(scopes)))
	if len(stringArray(scopes)) > 0 {
		findings[2].Status = "pass"
	} else {
		findings[2].Status = "fail"
	}
	return findings
}

func oauthResourceFinding(ss *session, home *url.URL, applicable bool, note *string) reportmodel.Finding {
	f := reportmodel.Finding{
		ID: "agent.oauth.protected_resource", Pillar: "agent", Title: "Protected-resource metadata", Status: "unknown", Weight: 1, Tier: "recommended", Stage: "act",
		URL: "", Applicability: "Applies when OAuth/API surfaces are advertised; reads RFC 9728 metadata only and never authenticates or exchanges tokens.",
		Remediation: "Publish protected-resource metadata with a resource identifier and supported authorization servers.",
		Standards:   []reportmodel.StandardReference{{ID: "OAuth Protected Resource Metadata", URL: protectedResourceRFC, Coverage: "partial"}},
	}
	if !applicable {
		f.Status = "not_applicable"
		f.Evidence = "No public authentication surface was linked from the homepage."
		return f
	}
	if note != nil {
		f.Evidence = "Protected-resource metadata was not observed: " + *note
		return f
	}
	raw := origin(home) + "/.well-known/oauth-protected-resource"
	f.URL = raw
	r, err := ss.probe(raw, origin(home))
	if err != nil {
		f.Evidence = "Protected-resource metadata was not observed: " + err.Error()
		return f
	}
	if r.code == http.StatusNotFound || r.code == http.StatusGone {
		f.Status = "fail"
		f.Evidence = fmt.Sprintf("RFC 9728 metadata returned HTTP %d", r.code)
		return f
	}
	if r.code != http.StatusOK {
		f.Evidence = fmt.Sprintf("Protected-resource metadata returned HTTP %d", r.code)
		return f
	}
	var metadata map[string]any
	if json.Unmarshal(r.body, &metadata) != nil || metadata == nil {
		f.Status = "fail"
		f.Evidence = "Protected-resource response was not a JSON metadata object."
		return f
	}
	resource := absolutePublicURL(stringValue(metadata["resource"]))
	servers := stringArray(metadata["authorization_servers"])
	validServers := 0
	for _, server := range servers {
		if absolutePublicURL(server) {
			validServers++
		}
	}
	f.Evidence = fmt.Sprintf("resource identifier valid=%t; %d of %d authorization server URL(s) valid", resource, validServers, len(servers))
	if resource && validServers > 0 {
		f.Status = "pass"
	} else if resource || validServers > 0 {
		f.Status = "partial"
	} else {
		f.Status = "fail"
	}
	return f
}

func stringArray(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text := stringValue(value); text != "" {
			result = append(result, text)
		}
	}
	return result
}

func stringArrayIncludes(value any, want string) bool {
	for _, got := range stringArray(value) {
		if got == want {
			return true
		}
	}
	return false
}
