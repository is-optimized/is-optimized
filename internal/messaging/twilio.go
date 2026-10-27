package messaging

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

func ValidSignature(token, publicURL, signature string, form url.Values) bool {
	if token == "" || signature == "" {
		return false
	}
	keys := make([]string, 0, len(form))
	for k := range form {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := publicURL
	for _, k := range keys {
		values := append([]string(nil), form[k]...)
		sort.Strings(values)
		last := ""
		for i, v := range values {
			if i > 0 && v == last {
				continue
			}
			s += k + v
			last = v
		}
	}
	m := hmac.New(sha1.New, []byte(token))
	m.Write([]byte(s))
	expected := base64.StdEncoding.EncodeToString(m.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expected))
}

type Sender interface {
	Send(context.Context, string, string, string) (string, string, error)
}
type Twilio struct {
	Account, Token, From string
	Client               *http.Client
	Endpoint             string
}

func (t *Twilio) Send(ctx context.Context, to, body, callback string) (string, string, error) {
	endpoint := t.Endpoint
	if endpoint == "" {
		endpoint = "https://api.twilio.com"
	}
	form := url.Values{"To": {to}, "From": {t.From}, "Body": {body}, "StatusCallback": {callback}}
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint+"/2010-04-01/Accounts/"+url.PathEscape(t.Account)+"/Messages.json", strings.NewReader(form.Encode()))
	if e != nil {
		return "", "failed", e
	}
	req.SetBasicAuth(t.Account, t.Token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := t.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, e := client.Do(req)
	if e != nil {
		return "", "outcome-unknown", errors.New("provider outcome unknown")
	}
	defer resp.Body.Close()
	var result struct {
		SID  string `json:"sid"`
		Code int    `json:"code"`
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if e != nil {
		return "", "outcome-unknown", e
	}
	if json.Unmarshal(b, &result) != nil {
		return "", "outcome-unknown", errors.New("invalid provider response")
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && result.SID != "" {
		return result.SID, "accepted", nil
	}
	if result.Code == 21610 {
		return "", "suppressed", errors.New("provider suppression")
	}
	if resp.StatusCode >= 500 {
		return "", "outcome-unknown", errors.New("provider outcome unknown")
	}
	return "", "failed", errors.New("provider rejected message")
}
