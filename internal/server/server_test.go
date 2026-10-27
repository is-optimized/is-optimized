package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/is-optimized/is-optimized/internal/reportmodel"
	"github.com/is-optimized/is-optimized/internal/scanner"
	"github.com/is-optimized/is-optimized/internal/store"
)

type fakeScanner struct{ calls int }

func (f *fakeScanner) Scan(context.Context, string) (reportmodel.Report, error) {
	f.calls++
	n := 75.0
	return reportmodel.Report{Domain: "example.com", StartedAt: time.Now(), CompletedAt: time.Now(), MethodologyVersion: scanner.MethodologyVersion, Pages: []reportmodel.Page{{URL: "https://example.com", StatusCode: 200}}, Pillars: []reportmodel.Pillar{{ID: "seo", Name: "SEO", Score: &n, Coverage: 1}}, Findings: []reportmodel.Finding{{ID: "title", Pillar: "seo", Title: "Title", Status: "fail", Evidence: "<script>alert(1)</script>", Remediation: "Add a descriptive title"}}}, nil
}

type fakeSender struct{ calls int }

func (f *fakeSender) Send(context.Context, string, string, string) (string, string, error) {
	f.calls++
	return "SMreply", "accepted", nil
}
func setup(t *testing.T) (*Server, *fakeScanner, *fakeSender) {
	t.Helper()
	db, e := store.NewLocal(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	scan := &fakeScanner{}
	sender := &fakeSender{}
	s, e := New(db, scan, sender, Config{PublicURL: "https://audit.example", SMS: true, TwilioAccount: "ACtest", TwilioToken: "test-only", TwilioFrom: "+15555550100", Disclosure: "Test disclosure", DisclosureVersion: "fixture-v1", SMSWording: "REPORT {token}"})
	if e != nil {
		t.Fatal(e)
	}
	return s, scan, sender
}
func perform(s *Server, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.10:1234"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func completed(t *testing.T, s *Server) string {
	t.Helper()
	w := perform(s, "POST", "/api/v1/scans", `{"domain":"example.com"}`)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	s.dispatch(context.Background())
	s.wg.Wait()
	return result["id"]
}
func TestCompleteReportFlow(t *testing.T) {
	s, scan, _ := setup(t)
	key := completed(t, s)
	w := perform(s, "GET", "/reports/"+key, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "&lt;script&gt;") || strings.Contains(w.Body.String(), "<script>alert(1)") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = perform(s, "GET", "/reports/"+key+"/pdf", "")
	if w.Code != 200 || !strings.HasPrefix(w.Body.String(), "%PDF-") {
		t.Fatal(w.Code)
	}
	w = perform(s, "GET", "/example.com", "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	w = perform(s, "POST", "/api/v1/scans", `{"domain":"example.com"}`)
	if w.Code != 200 || scan.calls != 1 {
		t.Fatal("cache did not reuse report")
	}
}
func signed(s *Server, path string, form url.Values) *httptest.ResponseRecorder {
	keys := []string{}
	for k := range form {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	data := s.cfg.PublicURL + path
	for _, k := range keys {
		data += k + form.Get(k)
	}
	m := hmac.New(sha1.New, []byte(s.cfg.TwilioToken))
	m.Write([]byte(data))
	r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Twilio-Signature", base64.StdEncoding.EncodeToString(m.Sum(nil)))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func requestToken(t *testing.T, s *Server, key string) string {
	t.Helper()
	w := perform(s, "POST", "/api/v1/report-requests", `{"report_id":"`+key+`","marketing":false}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var v map[string]string
	json.Unmarshal(w.Body.Bytes(), &v)
	return tokenPattern.FindString(v["body"])
}
func TestInboundDuplicateAndRestart(t *testing.T) {
	s, _, sender := setup(t)
	key := completed(t, s)
	token := requestToken(t, s, key)
	form := url.Values{"AccountSid": {"ACtest"}, "From": {"+15555550123"}, "To": {s.cfg.TwilioFrom}, "MessageSid": {"SMinbound"}, "Body": {"REPORT " + token}}
	if w := signed(s, "/webhooks/twilio/inbound", form); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if sender.calls != 0 {
		t.Fatal("inbound must persist before async delivery")
	}
	restarted, e := New(s.db, s.scan, sender, s.cfg)
	if e != nil {
		t.Fatal(e)
	}
	restarted.dispatchMessages(context.Background())
	form.Set("MessageSid", "SMdifferent")
	signed(restarted, "/webhooks/twilio/inbound", form)
	restarted.dispatchMessages(context.Background())
	if sender.calls != 1 {
		t.Fatalf("want one send, got %d", sender.calls)
	}
	var req Request
	s.db.Get(context.Background(), "request_"+token, &req)
	if req.Marketing || req.State != "accepted" {
		t.Fatal(req.State)
	}
}
func TestInvalidSignatureAndStop(t *testing.T) {
	s, _, sender := setup(t)
	w := perform(s, "POST", "/webhooks/twilio/inbound", "")
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	key := completed(t, s)
	token := requestToken(t, s, key)
	form := url.Values{"AccountSid": {"ACtest"}, "From": {"+15555550123"}, "To": {s.cfg.TwilioFrom}, "MessageSid": {"SMstop"}, "Body": {"STOP"}, "OptOutType": {"STOP"}}
	signed(s, "/webhooks/twilio/inbound", form)
	form.Del("OptOutType")
	form.Set("Body", "REPORT "+token)
	form.Set("MessageSid", "SMrequest")
	signed(s, "/webhooks/twilio/inbound", form)
	s.dispatchMessages(context.Background())
	if sender.calls != 0 {
		t.Fatal("suppressed sender was contacted")
	}
}
func TestSendingCrashDoesNotResend(t *testing.T) {
	s, _, sender := setup(t)
	req := Request{Token: id(), Sender: "+15555550123", State: "sending", Updated: time.Now().Add(-2 * time.Minute)}
	s.db.Update(context.Background(), "request_"+req.Token, func(json.RawMessage) (any, error) { return req, nil })
	s.dispatchMessages(context.Background())
	s.db.Get(context.Background(), "request_"+req.Token, &req)
	if req.State != "outcome-unknown" || sender.calls != 0 {
		t.Fatal(req.State, sender.calls)
	}
}
func TestTrustedIngress(t *testing.T) {
	s, _, _ := setup(t)
	s.cfg.TrustedProxyCIDRs = "10.0.0.0/8,203.0.113.8/32"
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.2:80"
	r.Header.Set("X-Forwarded-For", "192.0.2.77, 198.51.100.22, 203.0.113.8")
	if s.clientIP(r) != "198.51.100.22" {
		t.Fatal(s.clientIP(r))
	}
	r.RemoteAddr = "192.0.2.1:80"
	if s.clientIP(r) != "192.0.2.1" {
		t.Fatal("untrusted forwarding header accepted")
	}
}

type failPDF struct {
	store.Store
	failures int
}

func (f *failPDF) PutPDF(ctx context.Context, k string, b []byte) error {
	if f.failures > 0 {
		f.failures--
		return errors.New("fixture failure")
	}
	return f.Store.PutPDF(ctx, k, b)
}
func TestRenderRetryUsesSavedEvidence(t *testing.T) {
	s, scan, _ := setup(t)
	s.db = &failPDF{Store: s.db, failures: 1}
	w := perform(s, "POST", "/api/v1/scans", `{"domain":"example.com"}`)
	if w.Code != 202 {
		t.Fatal(w.Code)
	}
	s.dispatch(context.Background())
	s.wg.Wait()
	s.dispatch(context.Background())
	s.wg.Wait()
	if scan.calls != 1 {
		t.Fatalf("rescanned %d times", scan.calls)
	}
}
func TestStaleWorkerCannotPublish(t *testing.T) {
	s, _, _ := setup(t)
	j := Job{ID: id(), Domain: "example.com", State: "running", Lease: "old", LeaseUntil: time.Now().Add(time.Minute)}
	q := Queue{Jobs: map[string]Job{j.ID: {ID: j.ID, Domain: j.Domain, State: "running", Lease: "new", LeaseUntil: j.LeaseUntil}}}
	s.db.Update(context.Background(), "queue", func(json.RawMessage) (any, error) { return q, nil })
	s.work(context.Background(), j)
	var snap Snapshot
	if s.db.Get(context.Background(), "report_"+j.ID, &snap) != store.ErrNotFound {
		t.Fatal("stale report published")
	}
}

func TestTerminalHistoryDoesNotBlockAdmission(t *testing.T) {
	s, _, _ := setup(t)
	q := Queue{Jobs: map[string]Job{}}
	for i := 0; i < 64; i++ {
		key := id()
		q.Jobs[key] = Job{ID: key, Domain: "old.example", State: "completed", Updated: time.Now()}
	}
	s.db.Update(context.Background(), "queue", func(json.RawMessage) (any, error) { return q, nil })
	w := perform(s, "POST", "/api/v1/scans", `{"domain":"new.example.com"}`)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
}
