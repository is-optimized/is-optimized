package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/is-optimized/is-optimized/internal/messaging"
	"github.com/is-optimized/is-optimized/internal/store"
)

// Approved request copy. Correlation is appended internally, never supplied by a visitor.
const ApprovedRequestWording = "Hi, please send me the free website audit report for {domain}. My name is {name}."

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
var tokenPattern = regexp.MustCompile(`\b[a-f0-9]{36}\b`)
var correlationPattern = regexp.MustCompile(`(?:^|\n)Report code: ([a-f0-9]{36})$`)

func (s *Server) requestReport(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.SMS {
		http.Error(w, "Text delivery is not enabled. View the report online.", 503)
		return
	}
	var in struct {
		ReportID  string `json:"report_id"`
		Marketing bool   `json:"marketing"`
		Name      string `json:"name"`
		Phone     string `json:"phone"`
	}
	if decode(r, &in) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	snapshot, ok := s.load(w, r, in.ReportID)
	if !ok {
		return
	}
	if !s.allow(r) {
		http.Error(w, "request limit reached", 429)
		return
	}
	in.Phone = strings.TrimSpace(in.Phone)
	if in.Phone != "" && !phonePattern.MatchString(in.Phone) {
		http.Error(w, "invalid mobile phone number", 400)
		return
	}
	name := strings.TrimSpace(in.Name)
	if utf8.RuneCountInString(name) > 80 || strings.ContainsAny(name, "{}") || tokenPattern.MatchString(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		http.Error(w, "Use a name of up to 80 characters, or leave it blank.", 400)
		return
	}
	wording := s.cfg.SMSWording
	if name == "" {
		wording = strings.ReplaceAll(wording, " My name is {name}.", "")
	}
	wording = strings.NewReplacer("{domain}", snapshot.Report.Domain, "{name}", name).Replace(wording)
	req := Request{IntendedPhone: in.Phone, Token: id(), ReportID: in.ReportID, Disclosure: s.cfg.Disclosure, DisclosureVersion: s.cfg.DisclosureVersion, Wording: wording, Marketing: in.Marketing, Created: time.Now().UTC(), Expires: time.Now().Add(30 * time.Minute), State: "unconsumed"}
	if s.db.Update(r.Context(), "request_"+req.Token, func(raw json.RawMessage) (any, error) {
		if len(raw) > 0 {
			return nil, errConflict
		}
		return req, nil
	}) != nil {
		http.Error(w, "request unavailable", 503)
		return
	}
	body := strings.ReplaceAll(req.Wording, "{token}", req.Token)
	jsonOut(w, 201, map[string]string{"to": s.cfg.TwilioFrom, "body": body, "sms_url": "sms:" + s.cfg.TwilioFrom + "?body=" + url.QueryEscape(body)})
}
func (s *Server) webhook(w http.ResponseWriter, r *http.Request) bool {
	if !s.cfg.SMS || r.ParseForm() != nil {
		http.Error(w, "unavailable", 400)
		return false
	}
	if !messaging.ValidSignature(s.cfg.TwilioToken, s.cfg.PublicURL+r.URL.RequestURI(), r.Header.Get("X-Twilio-Signature"), r.PostForm) || r.PostForm.Get("AccountSid") != s.cfg.TwilioAccount {
		http.Error(w, "invalid signature", 403)
		return false
	}
	return true
}
func ack(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Response/>`))
}
func (s *Server) inbound(w http.ResponseWriter, r *http.Request) {
	if !s.webhook(w, r) {
		return
	}
	from := r.PostForm.Get("From")
	if !phonePattern.MatchString(from) || r.PostForm.Get("To") != s.cfg.TwilioFrom {
		http.Error(w, "invalid sender context", 400)
		return
	}
	body := strings.TrimSpace(r.PostForm.Get("Body"))
	opt := r.PostForm.Get("OptOutType")
	command := strings.ToUpper(body)
	if opt != "" || command == "STOP" || command == "HELP" || command == "START" || command == "UNSUBSCRIBE" || command == "CANCEL" || command == "END" || command == "QUIT" {
		if opt == "STOP" || command == "STOP" || command == "UNSUBSCRIBE" || command == "CANCEL" || command == "END" || command == "QUIT" {
			if s.db.Update(r.Context(), "suppressed_"+hash(from), func(json.RawMessage) (any, error) {
				return map[string]any{"suppressed": true, "at": time.Now().UTC(), "provider_event": r.PostForm.Get("MessageSid")}, nil
			}) != nil {
				http.Error(w, "persistence unavailable", 503)
				return
			}
		}
		ack(w)
		return
	}
	// Prefer our explicit trailing code: a domain itself may contain a
	// 36-character hexadecimal label. Stored exact-message validation below
	// still rejects altered requests. Retain legacy single-token wording.
	var token string
	if match := correlationPattern.FindStringSubmatch(body); len(match) == 2 {
		token = match[1]
	} else {
		tokens := tokenPattern.FindAllString(body, -1)
		if len(tokens) != 1 {
			ack(w)
			return
		}
		token = tokens[0]
	}
	messageID := r.PostForm.Get("MessageSid")
	if messageID == "" {
		http.Error(w, "missing message ID", 400)
		return
	}
	claimed := false
	err := s.db.Update(r.Context(), "request_"+token, func(raw json.RawMessage) (any, error) {
		claimed = false
		if len(raw) == 0 {
			return nil, store.ErrNotFound
		}
		var req Request
		if e := json.Unmarshal(raw, &req); e != nil {
			return nil, e
		}
		if req.State != "unconsumed" {
			return req, nil
		}
		if (req.IntendedPhone != "" && req.IntendedPhone != from) || time.Now().After(req.Expires) || body != strings.ReplaceAll(req.Wording, "{token}", token) {
			return nil, errConflict
		}
		req.Sender = from
		req.InboundID = messageID
		req.State = "pending"
		req.Updated = time.Now().UTC()
		claimed = true
		return req, nil
	})
	if err != nil {
		if err == store.ErrNotFound || err == errConflict {
			ack(w)
		} else {
			http.Error(w, "persistence unavailable", 503)
		}
		return
	}
	if !claimed {
		ack(w)
		return
	}
	ack(w)
}
func (s *Server) dispatchMessages(ctx context.Context) {
	if !s.cfg.SMS {
		return
	}
	rows, e := s.db.List(ctx, "request_")
	if e != nil {
		return
	}
	for _, raw := range rows {
		if ctx.Err() != nil {
			return
		}
		var req Request
		if json.Unmarshal(raw, &req) != nil {
			continue
		}
		if req.State == "sending" && time.Since(req.Updated) > time.Minute {
			s.db.Update(ctx, "request_"+req.Token, func(b json.RawMessage) (any, error) {
				var cur Request
				if e := json.Unmarshal(b, &cur); e != nil {
					return nil, e
				}
				if cur.State == "sending" && time.Since(cur.Updated) > time.Minute {
					cur.State = "outcome-unknown"
					cur.Updated = time.Now().UTC()
				}
				return cur, nil
			})
			continue
		}
		if req.State == "pending" {
			s.sendPending(ctx, req)
		}
	}
}
func (s *Server) sendPending(ctx context.Context, req Request) {
	var suppressed struct {
		Suppressed bool `json:"suppressed"`
	}
	e := s.db.Get(ctx, "suppressed_"+hash(req.Sender), &suppressed)
	if e != nil && e != store.ErrNotFound {
		return
	}
	// A request is charged once, including after a dispatcher restart.
	allowed := false
	e = s.db.Update(ctx, "quota_"+hash(req.Sender), func(raw json.RawMessage) (any, error) {
		allowed = false
		var q struct {
			Day    string
			Tokens []string
		}
		if len(raw) > 0 {
			if e := json.Unmarshal(raw, &q); e != nil {
				return nil, e
			}
		}
		day := time.Now().UTC().Format("2006-01-02")
		if q.Day != day {
			q.Day = day
			q.Tokens = nil
		}
		for _, t := range q.Tokens {
			if t == req.Token {
				allowed = true
				return q, nil
			}
		}
		if len(q.Tokens) < 5 {
			q.Tokens = append(q.Tokens, req.Token)
			allowed = true
		}
		return q, nil
	})
	if e != nil {
		return
	}
	claimed := false
	e = s.db.Update(ctx, "request_"+req.Token, func(raw json.RawMessage) (any, error) {
		claimed = false
		var cur Request
		if e := json.Unmarshal(raw, &cur); e != nil {
			return nil, e
		}
		if cur.State != "pending" {
			return cur, nil
		}
		if suppressed.Suppressed || !allowed {
			cur.State = "suppressed"
		} else {
			cur.State = "sending"
			claimed = true
		}
		cur.Updated = time.Now().UTC()
		req = cur
		return cur, nil
	})
	if e != nil || !claimed {
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	sid, state, _ := s.sender.Send(sendCtx, req.Sender, "Sire Run, Inc.: Here’s your free website audit report: "+s.cfg.PublicURL+"/reports/"+req.ReportID+"/pdf. Reply STOP to opt out.", s.cfg.PublicURL+"/webhooks/twilio/status?request="+req.Token)
	s.setDelivery((&http.Request{Method: "POST"}).WithContext(ctx), req.Token, state, sid)
}

func (s *Server) setDelivery(r *http.Request, token, state, sid string) bool {
	return s.db.Update(r.Context(), "request_"+token, func(raw json.RawMessage) (any, error) {
		var req Request
		if e := json.Unmarshal(raw, &req); e != nil {
			return nil, e
		}
		if req.State == "delivered" || req.State == "failed" || req.State == "suppressed" {
			return req, nil
		}
		req.State = state
		if sid != "" {
			req.ProviderID = sid
		}
		req.Updated = time.Now().UTC()
		return req, nil
	}) == nil
}
func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	if !s.webhook(w, r) {
		return
	}
	token := r.URL.Query().Get("request")
	if !store.KeyOK(token) {
		http.Error(w, "invalid request", 400)
		return
	}
	state := r.PostForm.Get("MessageStatus")
	switch state {
	case "delivered":
	case "failed", "undelivered":
		state = "failed"
	case "accepted", "queued", "sending", "sent":
		state = "accepted"
	default:
		ack(w)
		return
	}
	err := s.db.Update(r.Context(), "request_"+token, func(raw json.RawMessage) (any, error) {
		if len(raw) == 0 {
			return nil, store.ErrNotFound
		}
		var req Request
		if e := json.Unmarshal(raw, &req); e != nil {
			return nil, e
		}
		sid := r.PostForm.Get("MessageSid")
		if sid == "" || (req.ProviderID != "" && req.ProviderID != sid) || req.Sender != r.PostForm.Get("To") || r.PostForm.Get("From") != s.cfg.TwilioFrom {
			return nil, errConflict
		}
		if req.State == "delivered" || req.State == "suppressed" || req.State == "failed" {
			return req, nil
		}
		if req.State != "sending" && req.State != "accepted" && req.State != "outcome-unknown" {
			return nil, errConflict
		}
		req.State = state
		req.ProviderID = sid
		req.Updated = time.Now().UTC()
		return req, nil
	})
	if err != nil {
		http.Error(w, "callback rejected", 409)
		return
	}
	ack(w)
}
