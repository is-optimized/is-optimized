package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"time"

	"cloud.google.com/go/compute/metadata"
	"golang.org/x/oauth2/google"
)

// This credential/bootstrap client is isolated from all target-controlled URLs.
// Metadata includes only configuration and secret resource names, never values.
func loadRuntimeConfig(ctx context.Context) error {
	if os.Getenv("CONFIG_FROM_METADATA") == "true" {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		value, e := metadata.GetWithContext(ctx, "instance/attributes/is-optimized-config")
		if e != nil {
			return errors.New("runtime metadata configuration unavailable")
		}
		var config map[string]string
		if json.Unmarshal([]byte(value), &config) != nil {
			return errors.New("invalid runtime metadata configuration")
		}
		allowed := map[string]bool{}
		for _, k := range []string{"PUBLIC_URL", "GCP_PROJECT_ID", "GCS_REPORT_BUCKET", "TRUSTED_PROXY_CIDRS", "SMS_ENABLED", "TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN_SECRET", "TWILIO_FROM", "CONSENT_DISCLOSURE", "CONSENT_VERSION", "SMS_REQUEST_WORDING"} {
			allowed[k] = true
		}
		for k, v := range config {
			if !allowed[k] {
				return errors.New("unsupported metadata configuration key")
			}
			if e := os.Setenv(k, v); e != nil {
				return e
			}
		}
	}
	resource := os.Getenv("TWILIO_AUTH_TOKEN_SECRET")
	if resource == "" {
		return nil
	}
	if !regexp.MustCompile(`^projects/[a-zA-Z0-9_-]+/secrets/[a-zA-Z0-9_-]+/versions/[0-9]+$`).MatchString(resource) {
		return errors.New("secret reference must pin a numeric version")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client, e := google.DefaultClient(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if e != nil {
		return errors.New("secret identity unavailable")
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, e := http.NewRequestWithContext(ctx, "GET", "https://secretmanager.googleapis.com/v1/"+resource+":access", nil)
	if e != nil {
		return e
	}
	resp, e := client.Do(req)
	if e != nil {
		return errors.New("secret retrieval failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("secret retrieval rejected")
	}
	var body struct {
		Payload struct {
			Data string `json:"data"`
		} `json:"payload"`
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); e != nil {
		return errors.New("invalid secret response")
	}
	token, e := base64.StdEncoding.DecodeString(body.Payload.Data)
	if e != nil || len(token) == 0 {
		return errors.New("invalid secret payload")
	}
	return os.Setenv("TWILIO_AUTH_TOKEN", string(token))
}
