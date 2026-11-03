package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicAPIErrorsNegotiateProblemJSON(t *testing.T) {
	s, _, _ := setup(t)
	for _, accept := range []string{"application/json", "application/problem+json", "text/plain"} {
		r := httptest.NewRequest("POST", "/api/v1/scans", strings.NewReader(`{"domain":"localhost"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", accept)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
		if accept == "text/plain" {
			if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
				t.Fatal("legacy text error changed")
			}
			continue
		}
		var problem map[string]any
		if json.Unmarshal(w.Body.Bytes(), &problem) != nil || problem["status"] != float64(400) || problem["type"] != "about:blank" || w.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatalf("%v %s", w.Header(), w.Body.String())
		}
	}
}
