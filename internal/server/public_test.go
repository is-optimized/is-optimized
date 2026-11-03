package server

import (
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicDiscoveryDocuments(t *testing.T) {
	s, scans, texts := setup(t)
	for _, path := range []string{"/robots.txt", "/sitemap.xml", "/llms.txt", "/llms-full.txt", "/AGENTS.md", "/auth.md", "/openapi.json", "/.well-known/api-catalog", "/.well-known/ai-catalog.json", "/.well-known/mcp/server-card.json", "/.well-known/agent-skills/index.json", "/.well-known/agent-skills/is-optimized-audit/SKILL.md", "/about", "/contact", "/privacy", "/terms", "/docs", "/methodology"} {
		w := perform(s, "GET", path, "")
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	var sitemap struct {
		URLs []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(perform(s, "GET", "/sitemap.xml", "").Body.Bytes(), &sitemap); err != nil || len(sitemap.URLs) != 7 {
		t.Fatalf("sitemap=%+v err=%v", sitemap, err)
	}
	var index struct {
		Skills []struct{ URL, Digest string }
	}
	if err := json.Unmarshal(perform(s, "GET", "/.well-known/agent-skills/index.json", "").Body.Bytes(), &index); err != nil || len(index.Skills) != 1 {
		t.Fatal(index, err)
	}
	artifact := perform(s, "GET", "/.well-known/agent-skills/is-optimized-audit/SKILL.md", "").Body.Bytes()
	if index.Skills[0].Digest != fmt.Sprintf("sha256:%x", sha256.Sum256(artifact)) {
		t.Fatal("skill digest does not match served bytes")
	}
	var api map[string]any
	if err := json.Unmarshal(perform(s, "GET", "/openapi.json", "").Body.Bytes(), &api); err != nil || api["openapi"] != "3.1.0" {
		t.Fatal(api, err)
	}
	if scans.calls != 0 || texts.calls != 0 {
		t.Fatal("discovery triggered side effects")
	}
}

func TestPublicMarkdownNegotiationAnd404(t *testing.T) {
	s, _, _ := setup(t)
	for _, path := range []string{"/", "/about", "/docs", "/some/missing/path", "/__missing"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Accept", "text/markdown")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		want := 200
		if strings.Contains(path, "missing") {
			want = 404
		}
		if w.Code != want || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/markdown") || !strings.Contains(w.Header().Get("Vary"), "Accept") || !strings.HasPrefix(w.Body.String(), "# ") || w.Body.Len() < 20 {
			t.Fatalf("%s: status=%d headers=%v body=%s", path, w.Code, w.Header(), w.Body.String())
		}
	}
	home := perform(s, "GET", "/", "").Body.String()
	for _, signal := range []string{"og:image", "og:type", `rel="canonical"`, `application/ld+json`, "SoftwareApplication"} {
		if !strings.Contains(home, signal) {
			t.Fatalf("missing homepage signal %s", signal)
		}
	}
}
