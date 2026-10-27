package messaging

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSignatureCanonicalFixture(t *testing.T) {
	v := url.Values{"CallSid": {"CA1234567890ABCDE"}, "Caller": {"+14158675310"}, "Digits": {"1234"}, "From": {"+14158675310"}, "To": {"+18005551212"}}
	if !ValidSignature("12345", "https://mycompany.com/myapp.php?foo=1&bar=2", "GvWf1cFY/Q7PnoempGyD5oXAezc=", v) {
		t.Fatal("signature fixture mismatch")
	}
	v.Set("Digits", "0000")
	if ValidSignature("12345", "https://mycompany.com/myapp.php?foo=1&bar=2", "GvWf1cFY/Q7PnoempGyD5oXAezc=", v) {
		t.Fatal("tampered payload accepted")
	}
}
func TestProviderOutcomes(t *testing.T) {
	for _, tc := range []struct {
		code        int
		body, state string
	}{{201, `{"sid":"SM1"}`, "accepted"}, {400, `{"code":21610}`, "suppressed"}, {503, `{}`, "outcome-unknown"}, {200, `not json`, "outcome-unknown"}} {
		t.Run(tc.state, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); w.Write([]byte(tc.body)) }))
			defer srv.Close()
			tw := Twilio{Account: "test", Token: "test", From: "+15555550100", Endpoint: srv.URL, Client: srv.Client()}
			_, state, _ := tw.Send(context.Background(), "+15555550123", "fixture", "https://example.com/callback")
			if state != tc.state {
				t.Fatal(state)
			}
		})
	}
}
