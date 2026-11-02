package scanner

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"golang.org/x/net/html"
)

func siteAgentChecks(ss *session, home *url.URL, homepage response, homepagePage pageResult) []reportmodel.Finding {
	if homepage.code == 0 {
		return []reportmodel.Finding{
			{ID: "agent.markdown", Pillar: "agent", Title: "Markdown content negotiation", Status: "unknown", Weight: 3, Tier: "essential", URL: origin(home) + "/", Evidence: "Homepage response unavailable; site-level representation was not probed."},
			{ID: "agent.markdown_404", Pillar: "agent", Title: "Markdown-aware not-found response", Status: "unknown", Weight: 3, Tier: "essential", URL: origin(home), Evidence: "Homepage response unavailable; not-found behavior was not probed."},
			{ID: "agent.robots", Pillar: "agent", Title: "AI crawler access policy", Status: "unknown", Weight: 3, Tier: "essential", URL: origin(home) + "/robots.txt", Evidence: "Homepage response unavailable; retrieval behavior was not probed."},
			trustFinding("geo.trust.about", "About page", "/about", "unknown", "Homepage response unavailable; trust-page discovery was not probed."),
			trustFinding("geo.trust.contact", "Contact page", "/contact", "unknown", "Homepage response unavailable; trust-page discovery was not probed."),
			trustFinding("geo.trust.privacy", "Privacy page", "/privacy", "unknown", "Homepage response unavailable; trust-page discovery was not probed."),
			trustFinding("geo.trust.terms", "Terms page", "/terms", "unknown", "Homepage response unavailable; trust-page discovery was not probed."),
			{ID: "seo.social_metadata", Pillar: "seo", Title: "Social metadata completeness", Status: "unknown", Weight: 2, Tier: "recommended", URL: origin(home) + "/", Evidence: "Homepage response unavailable; social metadata was not evaluated."},
			{ID: "geo.organization", Pillar: "geo", Title: "Organization schema completeness", Status: "unknown", Weight: 2, Tier: "recommended", URL: origin(home) + "/", Evidence: "Homepage response unavailable; structured identity was not evaluated."},
			{ID: "geo.entity_links", Pillar: "geo", Title: "Structured entity links", Status: "unknown", Weight: 1, Tier: "recommended", URL: origin(home) + "/", Evidence: "Homepage response unavailable; sameAs links were not evaluated."},
			{ID: "geo.schema_breadth", Pillar: "geo", Title: "Structured data type breadth", Status: "not_applicable", Weight: 1, Tier: "bonus", URL: origin(home) + "/", Evidence: "Homepage response unavailable; structured-data types were not evaluated."},
			{ID: "agent.instructions.quality", Pillar: "agent", Title: "llms.txt quality", Status: "unknown", Weight: 1, Tier: "recommended", URL: origin(home) + "/llms.txt", Evidence: "Homepage response unavailable; agent instructions were not probed."},
			{ID: "agent.instructions.when_to_use", Pillar: "agent", Title: "Agent when-to-use guidance", Status: "unknown", Weight: 1, Tier: "recommended", URL: origin(home) + "/llms.txt", Evidence: "Homepage response unavailable; agent instructions were not probed."},
			{ID: "agent.instructions.full", Pillar: "agent", Title: "Full agent reference file", Status: "unknown", URL: origin(home) + "/llms-full.txt", Evidence: "Homepage response unavailable; agent reference was not probed."},
		}
	}
	results := make(chan reportmodel.Finding, 14)
	go func() { results <- markdownNegotiationCheck(ss, home, homepage) }()
	go func() { results <- markdownNotFoundCheck(ss, home) }()
	go func() { results <- aiCrawlerAccessCheck(ss, home, homepage) }()
	for _, trust := range trustPageDefinitions() {
		trust := trust
		go func() { results <- trustPageCheck(ss, home, homepagePage, trust) }()
	}
	go func() { results <- socialMetadataCheck(ss, home, homepagePage) }()
	for _, schemaFinding := range schemaChecks(home, homepagePage) {
		schemaFinding := schemaFinding
		go func() { results <- schemaFinding }()
	}
	go func() {
		for _, f := range agentInstructionChecks(ss, home) {
			results <- f
		}
	}()
	out := make([]reportmodel.Finding, 0, 14)
	for i := 0; i < cap(results); i++ {
		out = append(out, <-results)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

var markdownLinkPattern = regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)

type llmsAssessment struct {
	hasHeading, hasSummary, withinBudget, balancedFences bool
	hasWhenToUse, hasLinks                               bool
	links                                                []string
}

func assessLLMSText(body string) llmsAssessment {
	assessment := llmsAssessment{}
	content := strings.TrimSpace(body)
	lines := strings.Split(content, "\n")
	if len(lines) > 0 {
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				assessment.hasHeading = strings.HasPrefix(strings.TrimSpace(line), "# ")
				break
			}
		}
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		assessment.hasSummary = assessment.hasSummary || strings.HasPrefix(line, ">")
		lower := strings.ToLower(line)
		if strings.Contains(lower, "when to use this") || strings.Contains(lower, "when to use") || strings.Contains(lower, "use this when") {
			assessment.hasWhenToUse = true
		}
	}
	assessment.withinBudget = len(body) <= 30000
	assessment.balancedFences = strings.Count(content, "```")%2 == 0
	for _, match := range markdownLinkPattern.FindAllStringSubmatch(body, 20) {
		raw := strings.TrimSpace(match[1])
		u, err := url.Parse(raw)
		if err != nil || raw == "" || strings.HasPrefix(raw, "#") || (u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		assessment.links = append(assessment.links, raw)
	}
	assessment.hasLinks = len(assessment.links) > 0
	return assessment
}

func agentInstructionChecks(ss *session, home *url.URL) []reportmodel.Finding {
	quality := reportmodel.Finding{ID: "agent.instructions.quality", Pillar: "agent", Title: "llms.txt quality", Status: "unknown", Weight: 1, Tier: "recommended", Stage: "read", URL: origin(home) + "/llms.txt", Applicability: "Assesses public agent-readable guidance when the site publishes llms.txt.", Remediation: "Publish a concise Markdown llms.txt with an H1, a short summary, a when-to-use section, and working links to authoritative site resources."}
	when := reportmodel.Finding{ID: "agent.instructions.when_to_use", Pillar: "agent", Title: "Agent when-to-use guidance", Status: "unknown", Weight: 1, Tier: "recommended", Stage: "read", URL: quality.URL, Applicability: "Relevant when the site publishes instructions intended for agent readers.", Remediation: "Add a specific when-to-use section that names best-fit tasks and links to the resources needed to complete them."}
	full := reportmodel.Finding{ID: "agent.instructions.full", Pillar: "agent", Title: "Full agent reference file", Status: "information", Stage: "read", URL: origin(home) + "/llms-full.txt", Applicability: "Optional longer reference file; presence alone does not establish quality.", Remediation: "If useful, publish a maintained full reference at /llms-full.txt."}
	type fetchResult struct {
		path string
		resp response
		err  error
	}
	files := make(chan fetchResult, 2)
	for _, path := range []string{"/llms.txt", "/llms-full.txt"} {
		path := path
		go func() {
			r, err := ss.probe(origin(home)+path, origin(home))
			files <- fetchResult{path: path, resp: r, err: err}
		}()
	}
	var llms response
	llmsUnavailable := false
	for range 2 {
		result := <-files
		if result.path == "/llms-full.txt" {
			if result.err != nil {
				full.Evidence = "Full reference probe not observed: " + result.err.Error()
			} else if result.resp.code == http.StatusOK && !looksLikeHTML(result.resp.body) {
				full.Evidence = fmt.Sprintf("HTTP %d; %d body bytes observed", result.resp.code, len(result.resp.body))
			} else {
				full.Evidence = fmt.Sprintf("HTTP %d; optional full reference not found as text", result.resp.code)
			}
			continue
		}
		if result.err != nil {
			quality.Evidence = "llms.txt not observed: " + result.err.Error()
			when.Evidence = quality.Evidence
			llmsUnavailable = true
			continue
		}
		llms = result.resp
	}
	if llmsUnavailable {
		quality.Status, when.Status = "unknown", "unknown"
		return []reportmodel.Finding{quality, when, full}
	}
	if llms.code != http.StatusOK || looksLikeHTML(llms.body) {
		quality.Status, when.Status = "fail", "fail"
		if quality.Evidence == "" {
			quality.Evidence = fmt.Sprintf("/llms.txt returned HTTP %d or HTML instead of a text/Markdown guide", llms.code)
		}
		when.Evidence = "No usable /llms.txt guidance was observed."
		return []reportmodel.Finding{quality, when, full}
	}
	assessment := assessLLMSText(string(llms.body))
	resolved := 0
	links := assessment.links
	if len(links) > 5 {
		links = links[:5]
	}
	linkResults := make(chan bool, len(links))
	for _, link := range links {
		raw := strings.TrimSpace(link)
		go func(raw string) {
			u, err := url.Parse(raw)
			if err != nil || u.User != nil || u.RawQuery != "" {
				linkResults <- false
				return
			}
			u.Fragment = ""
			u.RawFragment = ""
			u = home.ResolveReference(u)
			if validateURL(u) != nil {
				linkResults <- false
				return
			}
			var r response
			if origin(u) == origin(home) {
				r, err = ss.probe(u.String(), origin(home))
			} else {
				r, err = ss.probeExternal(u.String())
			}
			linkResults <- err == nil && r.code >= 200 && r.code < 400
		}(raw)
	}
	for range len(links) {
		if <-linkResults {
			resolved++
		}
	}
	baseOK := assessment.hasHeading && assessment.hasSummary && assessment.withinBudget && assessment.balancedFences && assessment.hasLinks && resolved*5 >= len(links)*4
	quality.Evidence = fmt.Sprintf("H1=%t, quoted summary=%t, %d characters (30,000 max), balanced fences=%t; %d/%d sampled Markdown links resolved", assessment.hasHeading, assessment.hasSummary, len(llms.body), assessment.balancedFences, resolved, len(links))
	if baseOK {
		quality.Status = "pass"
	} else if assessment.hasHeading || assessment.hasSummary || assessment.hasLinks {
		quality.Status = "partial"
	} else {
		quality.Status = "fail"
	}
	when.Status = "fail"
	when.Evidence = "No specific when-to-use section observed."
	if assessment.hasWhenToUse {
		when.Status = "pass"
		when.Evidence = "A when-to-use section was observed in llms.txt; task fit was not independently judged."
	}
	return []reportmodel.Finding{quality, when, full}
}

func schemaChecks(home *url.URL, homepage pageResult) []reportmodel.Finding {
	baseURL := origin(home) + "/"
	org := reportmodel.Finding{ID: "geo.organization", Pillar: "geo", Title: "Organization schema completeness", Status: "unknown", Weight: 2, Tier: "recommended", Stage: "read", URL: baseURL, Applicability: "Checks declared public Schema.org entity fields without validating their truth or business legitimacy.", Remediation: "Add accurate name, url, logo, description, two authority sameAs links, and for Organization entities, contactPoint and PostalAddress fields."}
	links := reportmodel.Finding{ID: "geo.entity_links", Pillar: "geo", Title: "Structured entity links", Status: "unknown", Weight: 1, Tier: "recommended", Stage: "find", URL: baseURL, Applicability: "Relevant to JSON-LD organization, local business, software, or person entities.", Remediation: "Add at least two accurate sameAs links to authoritative profiles for the declared entity."}
	breadth := reportmodel.Finding{ID: "geo.schema_breadth", Pillar: "geo", Title: "Structured data type breadth", Status: "not_applicable", Weight: 1, Tier: "bonus", Stage: "read", URL: baseURL, Applicability: "Bonus only when parseable JSON-LD entity types are present.", Remediation: "Add other accurate and applicable Schema.org types, such as WebSite or BreadcrumbList, where they help describe the site."}
	if homepage.doc == nil {
		org.Evidence, links.Evidence, breadth.Evidence = "Homepage HTML unavailable; schema was not observed.", "Homepage HTML unavailable; schema was not observed.", "Homepage HTML unavailable; schema was not observed."
		return []reportmodel.Finding{org, links, breadth}
	}
	var entityMaps []map[string]any
	types := map[string]bool{}
	walk(homepage.doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "script" || !strings.EqualFold(strings.TrimSpace(attr(n, "type")), "application/ld+json") {
			return
		}
		var value any
		if json.Unmarshal([]byte(text(n)), &value) == nil {
			walkJSONLD(value, &entityMaps, types)
		}
	})
	var recognized []map[string]any
	for _, entity := range entityMaps {
		for kind := range schemaTypes(entity) {
			switch kind {
			case "organization", "localbusiness", "softwareapplication", "person":
				recognized = append(recognized, entity)
			}
		}
	}
	if len(entityMaps) == 0 {
		org.Status, links.Status = "fail", "fail"
		org.Evidence, links.Evidence = "No parseable JSON-LD entity nodes were observed.", "No JSON-LD sameAs links were observed."
		breadth.Evidence = "No JSON-LD types observed; bonus is not applicable."
		return []reportmodel.Finding{org, links, breadth}
	}
	if len(recognized) == 0 {
		org.Status, links.Status = "fail", "fail"
		org.Evidence = "JSON-LD was present but no Organization, LocalBusiness, SoftwareApplication, or Person entity was observed."
		links.Evidence = "No recognized entity's sameAs links were observed."
	} else {
		complete, required := 0, 0
		maxSameAs := 0
		for _, entity := range recognized {
			kinds := schemaTypes(entity)
			orgEntity := kinds["organization"] || kinds["localbusiness"]
			checks := []bool{schemaValuePresent(entity["name"]), absolutePublicURL(schemaString(entity["url"])), schemaValuePresent(entity["logo"]), schemaValuePresent(entity["description"])}
			count := schemaSameAsCount(entity["sameAs"])
			if count > maxSameAs {
				maxSameAs = count
			}
			checks = append(checks, count >= 2)
			if orgEntity {
				checks = append(checks, schemaContactPoint(entity["contactPoint"]), schemaPostalAddress(entity["address"]))
			}
			for _, present := range checks {
				required++
				if present {
					complete++
				}
			}
		}
		org.Evidence = fmt.Sprintf("Observed %d of %d required fields across %d recognized entity node(s); fields: name, absolute url, logo, description, at least two sameAs links, and for Organization/LocalBusiness contactPoint plus PostalAddress", complete, required, len(recognized))
		switch {
		case complete == required:
			org.Status = "pass"
		case complete > 0:
			org.Status = "partial"
		default:
			org.Status = "fail"
		}
		links.Evidence = fmt.Sprintf("Maximum of %d sameAs link(s) observed on a recognized entity; two are recommended", maxSameAs)
		switch {
		case maxSameAs >= 2:
			links.Status = "pass"
		case maxSameAs == 1:
			links.Status = "partial"
		default:
			links.Status = "fail"
		}
	}
	if len(types) >= 3 {
		breadth.Status = "pass"
	} else if len(types) > 0 {
		breadth.Status = "partial"
	}
	breadth.Evidence = fmt.Sprintf("Observed %d distinct JSON-LD @type value(s); three or more earns a capped bonus signal", len(types))
	return []reportmodel.Finding{org, links, breadth}
}

func walkJSONLD(value any, entities *[]map[string]any, types map[string]bool) {
	visited := 0
	walkJSONLDNode(value, entities, types, &visited, 0)
}

func walkJSONLDNode(value any, entities *[]map[string]any, types map[string]bool, visited *int, depth int) {
	if *visited >= 2048 || depth > 64 {
		return
	}
	*visited++
	switch v := value.(type) {
	case map[string]any:
		if _, exists := v["@type"]; exists && len(*entities) < 128 {
			*entities = append(*entities, v)
			for kind := range schemaTypes(v) {
				types[kind] = true
			}
		}
		for _, child := range v {
			walkJSONLDNode(child, entities, types, visited, depth+1)
		}
	case []any:
		for _, child := range v {
			walkJSONLDNode(child, entities, types, visited, depth+1)
		}
	}
}

func schemaTypes(entity map[string]any) map[string]bool {
	out := map[string]bool{}
	add := func(raw string) {
		parts := strings.Split(strings.TrimSpace(raw), "/")
		if len(parts) > 0 && parts[len(parts)-1] != "" {
			out[strings.ToLower(parts[len(parts)-1])] = true
		}
	}
	switch value := entity["@type"].(type) {
	case string:
		add(value)
	case []any:
		for _, item := range value {
			if str, ok := item.(string); ok {
				add(str)
			}
		}
	}
	return out
}

func schemaString(value any) string {
	if str, ok := value.(string); ok {
		return str
	}
	if object, ok := value.(map[string]any); ok {
		if id, ok := object["@id"].(string); ok {
			return id
		}
	}
	return ""
}

func schemaValuePresent(value any) bool {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case map[string]any:
		return len(v) > 0
	case []any:
		return len(v) > 0
	default:
		return false
	}
}

func schemaSameAsCount(value any) int {
	switch v := value.(type) {
	case string:
		if absolutePublicURL(v) {
			return 1
		}
	case []any:
		count := 0
		for _, item := range v {
			if str, ok := item.(string); ok && absolutePublicURL(str) {
				count++
			}
		}
		return count
	}
	return 0
}

func schemaContactPoint(value any) bool {
	items := []any{value}
	if list, ok := value.([]any); ok {
		items = list
	}
	for _, item := range items {
		if object, ok := item.(map[string]any); ok && schemaValuePresent(object["contactType"]) && (schemaValuePresent(object["email"]) || schemaValuePresent(object["telephone"])) {
			return true
		}
	}
	return false
}

func schemaPostalAddress(value any) bool {
	items := []any{value}
	if list, ok := value.([]any); ok {
		items = list
	}
	for _, item := range items {
		if object, ok := item.(map[string]any); ok {
			kinds := schemaTypes(object)
			if kinds["postaladdress"] && (schemaValuePresent(object["addressCountry"]) || schemaValuePresent(object["addressLocality"])) {
				return true
			}
		}
	}
	return false
}

func socialMetadataCheck(ss *session, home *url.URL, homepage pageResult) reportmodel.Finding {
	f := reportmodel.Finding{
		ID: "seo.social_metadata", Pillar: "seo", Title: "Social metadata completeness", Status: "unknown",
		Weight: 2, Tier: "recommended", Stage: "read", URL: origin(home) + "/",
		Applicability: "Assesses homepage metadata signals used for page identity and link previews.",
		Remediation:   "Add html lang, Open Graph title/description/image/type/url, a Twitter card, and one valid canonical link.",
	}
	if homepage.doc == nil {
		f.Evidence = "Homepage HTML unavailable; metadata was not observed."
		return f
	}
	var lang, ogTitle, ogDescription, ogImage, ogType, ogURL, twitterCard, canonical string
	walk(homepage.doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "html" {
			lang = strings.TrimSpace(attr(n, "lang"))
		}
		if n.Data == "meta" {
			switch strings.ToLower(attr(n, "property")) {
			case "og:title":
				ogTitle = attr(n, "content")
			case "og:description":
				ogDescription = attr(n, "content")
			case "og:image":
				ogImage = strings.TrimSpace(attr(n, "content"))
			case "og:type":
				ogType = attr(n, "content")
			case "og:url":
				ogURL = strings.TrimSpace(attr(n, "content"))
			}
			if strings.EqualFold(attr(n, "name"), "twitter:card") {
				twitterCard = attr(n, "content")
			}
		}
		if n.Data == "link" && hasToken(attr(n, "rel"), "canonical") {
			canonical = strings.TrimSpace(attr(n, "href"))
		}
	})
	checks := 0
	observed := 0
	for _, ok := range []bool{
		lang != "", strings.TrimSpace(ogTitle) != "", strings.TrimSpace(ogDescription) != "",
		false, strings.TrimSpace(ogType) != "", absolutePublicURL(ogURL), strings.TrimSpace(twitterCard) != "", absolutePublicURL(canonical),
	} {
		checks++
		if ok {
			observed++
		}
	}
	imageState := "missing"
	if ogImage != "" {
		imageURL, err := url.Parse(ogImage)
		if err == nil && imageURL.IsAbs() && validateURL(imageURL) == nil {
			var image response
			if origin(imageURL) == origin(home) {
				image, err = ss.probe(imageURL.String(), origin(home))
			} else {
				image, err = ss.probeExternal(imageURL.String())
			}
			mediaType, _, _ := mime.ParseMediaType(image.contentType)
			if err == nil && image.code == http.StatusOK && strings.HasPrefix(strings.ToLower(mediaType), "image/") {
				imageState = "resolved to an HTTP 200 image/* response"
				observed++
			} else if err != nil {
				imageState = "resolution was not observed"
			} else {
				imageState = fmt.Sprintf("resolved to HTTP %d with Content-Type %s", image.code, mediaType)
			}
		} else {
			imageState = "present but not an absolute public URL"
		}
	} else {
		imageState = "missing"
	}
	f.Evidence = fmt.Sprintf("Observed %d of %d signals (html lang=%t, og:title=%t, og:description=%t, og:image=%s, og:type=%t, og:url=%t, twitter:card=%t, canonical=%t)",
		observed, checks, lang != "", strings.TrimSpace(ogTitle) != "", strings.TrimSpace(ogDescription) != "", imageState,
		strings.TrimSpace(ogType) != "", absolutePublicURL(ogURL), strings.TrimSpace(twitterCard) != "", absolutePublicURL(canonical))
	switch {
	case observed == checks:
		f.Status = "pass"
	case observed > 0:
		f.Status = "partial"
	default:
		f.Status = "fail"
	}
	return f
}

func absolutePublicURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && u.IsAbs() && validateURL(u) == nil
}

type trustPageDefinition struct {
	ID, Title, Route, LinkTerm string
}

func trustPageDefinitions() []trustPageDefinition {
	return []trustPageDefinition{
		{ID: "geo.trust.about", Title: "About page", Route: "/about", LinkTerm: "about"},
		{ID: "geo.trust.contact", Title: "Contact page", Route: "/contact", LinkTerm: "contact"},
		{ID: "geo.trust.privacy", Title: "Privacy page", Route: "/privacy", LinkTerm: "privacy"},
		{ID: "geo.trust.terms", Title: "Terms page", Route: "/terms", LinkTerm: "terms"},
	}
}

func trustFinding(id, title, route, status, evidence string) reportmodel.Finding {
	return reportmodel.Finding{
		ID: id, Pillar: "geo", Title: title, Status: status, Weight: 1, Tier: "recommended", Stage: "read",
		URL: route, Evidence: evidence,
		Applicability: "Assesses whether the site publishes a public first-party trust page; specific legal or business claims are not verified.",
		Remediation:   "Publish a useful first-party page for this topic and link it from the homepage or footer.",
	}
}

func trustPageCheck(ss *session, home *url.URL, homepage pageResult, def trustPageDefinition) reportmodel.Finding {
	f := trustFinding(def.ID, def.Title, origin(home)+def.Route, "unknown", "Trust-page response was not observed.")
	if homepage.doc == nil {
		f.Evidence = "Homepage HTML unavailable; trust-page links were not inspected."
		return f
	}
	probeURL := origin(home) + def.Route
	var linked string
	walk(homepage.doc, func(n *html.Node) {
		if linked != "" || n.Type != html.ElementNode || n.Data != "a" {
			return
		}
		u, ok := readinessLink(attr(n, "href"), home)
		if ok && origin(u) == origin(home) && strings.Contains(strings.ToLower(u.Path), def.LinkTerm) {
			linked = u.String()
		}
	})
	if linked != "" {
		probeURL = linked
	}
	r, err := ss.probe(probeURL, origin(home))
	if err != nil {
		f.Evidence = "Trust-page fetch was not observed: " + err.Error()
		return f
	}
	f.URL = r.url.String()
	page := analyze(r, nil, probeURL, 0)
	visibleBytes := 0
	if page.doc != nil {
		visibleBytes = len(initialText(page.doc))
	}
	f.Evidence = fmt.Sprintf("%s returned HTTP %d with %d visible-text bytes%s", def.Title, r.code, visibleBytes, map[bool]string{true: "; linked from homepage", false: "; standard path probed"}[linked != ""])
	if r.code != http.StatusOK || page.doc == nil {
		f.Status = "fail"
	} else if visibleBytes >= 500 {
		f.Status = "pass"
	} else if visibleBytes > 0 {
		f.Status = "partial"
	} else {
		f.Status = "fail"
	}
	return f
}

func aiCrawlerAccessCheck(ss *session, home *url.URL, homepage response) reportmodel.Finding {
	f := reportmodel.Finding{
		ID: "agent.robots", Pillar: "agent", Title: "AI crawler access policy", Status: "unknown",
		Weight: 3, Tier: "essential", Stage: "find", URL: origin(home) + "/robots.txt",
		Applicability: "Reports the published robots policy and read-only retrieval behavior; crawler policy choices are controlled by the site owner.",
		Remediation:   "Allow retrieval agents you intend to serve in robots.txt. Training-bot restrictions are a valid policy choice and are not penalized.",
	}
	policy := ss.getPolicy(home)
	if policy.err != nil {
		f.Evidence = "robots.txt policy was unavailable or malformed; agent rules and Content-Signal directives could not be evaluated."
		return f
	}
	retrieval := []string{"ChatGPT-User", "OAI-SearchBot", "Claude-User", "PerplexityBot"}
	type result struct {
		ua        string
		allowed   bool
		specified bool
		resp      response
		err       error
	}
	results := make(chan result, len(retrieval))
	for _, ua := range retrieval {
		go func(ua string) {
			allowed, specified := policy.rules.decision(ua, "/")
			if !allowed {
				results <- result{ua: ua, allowed: false, specified: specified, err: fmt.Errorf("disallowed by robots policy")}
				return
			}
			headers := make(http.Header)
			headers.Set("User-Agent", ua)
			r, err := ss.probeHeaders(origin(home)+"/", origin(home), headers)
			results <- result{ua: ua, allowed: allowed, specified: specified, resp: r, err: err}
		}(ua)
	}
	var observed []result
	blocked := false
	unobserved := false
	for range retrieval {
		observed = append(observed, <-results)
	}
	sort.Slice(observed, func(i, j int) bool { return observed[i].ua < observed[j].ua })
	var robotMatrix, liveMatrix []string
	for _, r := range observed {
		if r.specified {
			state := "allowed"
			if !r.allowed {
				state = "blocked"
				blocked = true
			}
			robotMatrix = append(robotMatrix, fmt.Sprintf("%s=%s", r.ua, state))
		} else {
			robotMatrix = append(robotMatrix, fmt.Sprintf("%s=unspecified", r.ua))
		}
		if r.err != nil {
			unobserved = true
			liveMatrix = append(liveMatrix, r.ua+"=unobserved")
			continue
		}
		ratio := 0.0
		if len(homepage.body) > 0 {
			ratio = float64(len(r.resp.body)) / float64(len(homepage.body))
		}
		liveMatrix = append(liveMatrix, fmt.Sprintf("%s=%d/%d%%", r.ua, r.resp.code, int(ratio*100)))
		if r.resp.code == http.StatusForbidden || r.resp.code == http.StatusUnauthorized || challengeBody(r.resp.body) {
			blocked = true
		}
		if r.resp.code != http.StatusOK || ratio < .8 {
			unobserved = true
		}
	}
	var lines []string
	if len(policy.rules.contentSignals) > 0 {
		lines = append(lines, "Content-Signal="+strings.Join(policy.rules.contentSignals, ";"))
	} else {
		lines = append(lines, "Content-Signal=not observed")
	}
	if !policy.present {
		lines = append(lines, "robots.txt has no rules: everything is allowed by default")
	}
	lines = append(lines, "robots retrieval "+strings.Join(robotMatrix, ","))
	lines = append(lines, "live GET "+strings.Join(liveMatrix, ","))
	training := []string{"GPTBot", "ClaudeBot", "anthropic-ai", "Google-Extended", "Applebot-Extended", "CCBot", "Bytespider", "meta-externalagent"}
	var trainingMatrix []string
	for _, ua := range training {
		allowed, specified := policy.rules.decision(ua, "/")
		state := "unspecified (allowed by default)"
		if specified {
			state = "allowed"
			if !allowed {
				state = "blocked"
			}
		}
		trainingMatrix = append(trainingMatrix, ua+"="+state)
	}
	lines = append(lines, "training (informational) "+strings.Join(trainingMatrix, ","))
	f.Evidence = excerptLimit(strings.Join(lines, "; "), 600)
	if blocked {
		f.Status = "fail"
	} else if unobserved {
		f.Status = "unknown"
	} else {
		f.Status = "pass"
	}
	return f
}

func challengeBody(body []byte) bool {
	text := strings.ToLower(string(body))
	return strings.Contains(text, "captcha") || strings.Contains(text, "challenge-platform") || strings.Contains(text, "access denied")
}

func markdownNegotiationCheck(ss *session, home *url.URL, homepage response) reportmodel.Finding {
	f := reportmodel.Finding{
		ID: "agent.markdown", Pillar: "agent", Title: "Markdown content negotiation", Status: "unknown",
		Weight: 3, Tier: "essential", Stage: "read", URL: origin(home) + "/",
		Applicability: "Applies to a site's public page representation; Markdown is optional but helps text-first clients.",
		Remediation:   "Serve Markdown when Accept: text/markdown is requested, include Vary: Accept, or publish a working Markdown alternate such as /index.md.",
	}
	r, err := ss.probeAccept(f.URL, origin(home), "text/markdown")
	if err != nil {
		f.Evidence = "Markdown representation was not observed: " + err.Error()
		return f
	}
	mediaType, _, _ := mime.ParseMediaType(r.contentType)
	isMarkdown := strings.EqualFold(mediaType, "text/markdown") && len(strings.TrimSpace(string(r.body))) > 0 && !looksLikeHTML(r.body)
	varyAccept := hasToken(r.header.Get("Vary"), "Accept")
	f.Evidence = fmt.Sprintf("GET / with Accept: text/markdown returned HTTP %d, Content-Type %s, Vary %q, %d body bytes", r.code, mediaType, r.header.Get("Vary"), len(r.body))
	homeType, _, _ := mime.ParseMediaType(homepage.contentType)
	htmlAvailable := homepage.code == http.StatusOK && strings.EqualFold(homeType, "text/html")
	if isMarkdown && varyAccept && htmlAvailable {
		f.Status = "pass"
		return f
	}
	if isMarkdown || varyAccept || !htmlAvailable {
		f.Status = "partial"
		return f
	}
	alt, altErr := ss.probe(origin(home)+"/index.md", origin(home))
	if altErr == nil && alt.code == http.StatusOK && markdownResponse(alt) {
		f.Status = "partial"
		f.Evidence += "; /index.md fallback returns Markdown"
		return f
	}
	f.Status = "fail"
	if altErr != nil {
		f.Evidence += "; /index.md fallback was not observed: " + altErr.Error()
	} else {
		f.Evidence += fmt.Sprintf("; /index.md fallback returned HTTP %d and Content-Type %s", alt.code, alt.contentType)
	}
	return f
}

func markdownNotFoundCheck(ss *session, home *url.URL) reportmodel.Finding {
	f := reportmodel.Finding{
		ID: "agent.markdown_404", Pillar: "agent", Title: "Markdown-aware not-found response", Status: "unknown",
		Weight: 3, Tier: "essential", Stage: "read", URL: origin(home),
		Applicability: "Applies to nonexistent public paths requested by text-first clients.",
		Remediation:   "Keep a 404 or 410 status and return a short text/markdown explanation with a link to documentation, a sitemap, or llms.txt when Markdown is requested.",
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		f.Evidence = "Could not create a unique not-found probe path."
		return f
	}
	probeURL := origin(home) + "/__is-optimized-404-" + hex.EncodeToString(nonce[:])
	html, htmlErr := ss.probeAccept(probeURL, origin(home), "text/html")
	markdown, markdownErr := ss.probeAccept(probeURL, origin(home), "text/markdown")
	if htmlErr != nil || markdownErr != nil {
		f.Evidence = fmt.Sprintf("Not-found probes were incomplete (HTML: %v; Markdown: %v)", htmlErr, markdownErr)
		return f
	}
	mediaType, _, _ := mime.ParseMediaType(markdown.contentType)
	isMarkdown := strings.EqualFold(mediaType, "text/markdown") && len(strings.TrimSpace(string(markdown.body))) >= 20 && !looksLikeHTML(markdown.body) && containsMarkdownLink(string(markdown.body))
	f.Evidence = fmt.Sprintf("Unique path returned HTML HTTP %d and Markdown HTTP %d with Content-Type %s and %d Markdown bytes", html.code, markdown.code, mediaType, len(markdown.body))
	if (html.code == http.StatusNotFound || html.code == http.StatusGone) &&
		(markdown.code == http.StatusNotFound || markdown.code == http.StatusGone) && isMarkdown {
		f.Status = "pass"
		return f
	}
	if (html.code == http.StatusNotFound || html.code == http.StatusGone) &&
		(markdown.code == http.StatusNotFound || markdown.code == http.StatusGone) {
		f.Status = "partial"
		return f
	}
	f.Status = "fail"
	return f
}

func markdownResponse(r response) bool {
	t, _, _ := mime.ParseMediaType(r.contentType)
	return strings.EqualFold(t, "text/markdown") && len(strings.TrimSpace(string(r.body))) > 0 && !looksLikeHTML(r.body)
}

func looksLikeHTML(body []byte) bool {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	return strings.HasPrefix(text, "<!doctype html") || strings.HasPrefix(text, "<html") || strings.Contains(text, "<body")
}

func containsMarkdownLink(body string) bool {
	open := strings.Index(body, "](")
	if open < 1 {
		return false
	}
	end := strings.Index(body[open+2:], ")")
	return end > 0
}
