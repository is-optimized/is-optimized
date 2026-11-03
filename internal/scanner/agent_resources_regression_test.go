package scanner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestAgentResourcesDoNotFailUnavailableGuides(t *testing.T) {
	for _, status := range []int{429, 500, 503} {
		s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/robots.txt" {
				return reply(r, 404, "", "text/plain"), nil
			}
			return reply(r, status, "unavailable", "text/plain"), nil
		})
		home, _ := url.Parse("https://example.com/")
		ss := &session{s: s, ctx: context.Background(), policies: map[string]policy{}}
		findings := agentResourceChecks(ss, home, readinessPage(t, `<main><a href="/docs">API docs</a></main>`))
		for _, f := range findings {
			if f.Status != "unknown" {
				t.Fatalf("HTTP%d: %s=%s", status, f.ID, f.Status)
			}
		}
	}
}

func TestSkillProbeCapCountsFailedAttempts(t *testing.T) {
	var entries []skillEntry
	for _, name := range []string{"one", "two", "three"} {
		entries = append(entries, skillEntry{Name: "skill-" + name, Type: "skill-md", Description: "Read the public guide.", URL: "/skills/" + name + ".md", Digest: "sha256:" + strings.Repeat("0", 64)})
	}
	body, _ := json.Marshal(skillsIndex{Schema: agentSkillsSchemaV020, Skills: entries})
	var fetched []string
	s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/.well-known/agent-skills/index.json" {
			return reply(r, 200, string(body), "application/json"), nil
		}
		if strings.HasPrefix(r.URL.Path, "/skills/") {
			fetched = append(fetched, r.URL.Path)
		}
		return reply(r, 404, "missing", "text/plain"), nil
	})
	home, _ := url.Parse("https://example.com/")
	ss := &session{s: s, ctx: context.Background(), policies: map[string]policy{}}
	findings := agentResourceChecks(ss, home, readinessPage(t, `<main>Public website</main>`))
	if len(fetched) != 2 || fetched[0] != "/skills/one.md" || fetched[1] != "/skills/two.md" || findingByID(findings, "agent.resource.skills_index").Status != "fail" {
		t.Fatalf("fetches=%v findings=%v", fetched, findings)
	}
}

func TestSkillIndexRequiresArray(t *testing.T) {
	for _, tc := range []struct{ tail, want string }{{"", "fail"}, {`,"skills":null`, "fail"}, {`,"skills":[]`, "partial"}} {
		s := fixtureScanner(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/.well-known/agent-skills/index.json" {
				return reply(r, 200, `{"$schema":"`+agentSkillsSchemaV020+`"`+tc.tail+`}`, "application/json"), nil
			}
			return reply(r, 404, "missing", "text/plain"), nil
		})
		home, _ := url.Parse("https://example.com/")
		ss := &session{s: s, ctx: context.Background(), policies: map[string]policy{}}
		got := findingByID(agentResourceChecks(ss, home, readinessPage(t, `<main>Public website</main>`)), "agent.resource.skills_index")
		if got.Status != tc.want {
			t.Fatalf("%s: %s want%s", tc.tail, got.Status, tc.want)
		}
	}
}
