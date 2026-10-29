package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"

	"cloud.google.com/go/compute/metadata"
	"golang.org/x/oauth2/google"
)

// This credential/bootstrap client is isolated from all target-controlled URLs.
// Metadata includes only configuration and secret resource names, never values.
func loadRuntimeConfig(ctx context.Context) error {
	if os.Getenv("CONFIG_FROM_METADATA") == "true" {
		if err := loadMetadataConfig(ctx, metadata.GetWithContext, defaultMetadataRetryPolicy); err != nil {
			return err
		}
	}
	resource := os.Getenv("TWILIO_AUTH_TOKEN_SECRET")
	if resource == "" {
		return nil
	}
	if !secretVersion.MatchString(resource) {
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

// Metadata can be transiently unreachable while a cold guest establishes its
// network. This policy bounds that wait; it is not evidence of the failure's cause.
type metadataRetryPolicy struct {
	totalTimeout, attemptTimeout, initialBackoff, maxBackoff time.Duration
}

var defaultMetadataRetryPolicy = metadataRetryPolicy{
	totalTimeout: 60 * time.Second, attemptTimeout: 5 * time.Second,
	initialBackoff: time.Second, maxBackoff: 5 * time.Second,
}

type metadataGetter func(context.Context, string) (string, error)

var (
	errMetadataUnavailable = errors.New("runtime metadata unavailable")
	errMetadataInvalid     = errors.New("runtime metadata configuration is invalid")
	errMetadataUnsupported = errors.New("runtime metadata contains an unsupported key")
)

func loadMetadataConfig(parent context.Context, get metadataGetter, policy metadataRetryPolicy) error {
	if policy.totalTimeout <= 0 || policy.attemptTimeout <= 0 || policy.initialBackoff <= 0 || policy.maxBackoff < policy.initialBackoff {
		return errors.New("invalid runtime metadata retry policy")
	}
	ctx, cancel := context.WithTimeout(parent, policy.totalTimeout)
	defer cancel()
	backoff := policy.initialBackoff
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		attempt, stop := context.WithTimeout(ctx, policy.attemptTimeout)
		value, err := get(attempt, "instance/attributes/is-optimized-config")
		attemptErr := attempt.Err()
		stop()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil && attemptErr != nil {
			err = attemptErr
		}
		if err == nil {
			return applyMetadataConfig(value)
		}
		if !transientMetadataError(err) {
			return errMetadataUnavailable
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if backoff < policy.maxBackoff {
			backoff *= 2
			if backoff > policy.maxBackoff {
				backoff = policy.maxBackoff
			}
		}
	}
}

func transientMetadataError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var response *metadata.Error
	if errors.As(err, &response) {
		return response.Code == http.StatusRequestTimeout || response.Code == http.StatusTooManyRequests || response.Code >= 500 && response.Code <= 599
	}
	var network net.Error
	if errors.As(err, &network) && (network.Timeout() || network.Temporary()) {
		return true
	}
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.EPIPE)
}

func applyMetadataConfig(value string) error {
	var values map[string]string
	if json.Unmarshal([]byte(value), &values) != nil || values == nil {
		return errMetadataInvalid
	}
	// Validate the complete document before changing process configuration. JSON
	// maps are unordered, so validation during application risks partial updates.
	for key, value := range values {
		if !metadataKeys[key] {
			return errMetadataUnsupported
		}
		if strings.ContainsRune(value, 0) {
			return errMetadataInvalid
		}
		if key == "TWILIO_AUTH_TOKEN_SECRET" && value != "" && !secretVersion.MatchString(value) {
			return errMetadataInvalid
		}
	}
	for key, value := range values {
		// Keys come from a fixed allowlist and values have no NUL, the only
		// invalid value for Setenv on this platform. No metadata value is logged.
		if os.Setenv(key, value) != nil {
			return errMetadataInvalid
		}
	}
	return nil
}

var secretVersion = regexp.MustCompile(`^projects/[a-zA-Z0-9_-]+/secrets/[a-zA-Z0-9_-]+/versions/[0-9]+$`)

var metadataKeys = func() map[string]bool {
	allowed := map[string]bool{}
	for _, key := range []string{"PUBLIC_URL", "GCP_PROJECT_ID", "GCS_REPORT_BUCKET", "TRUSTED_PROXY_CIDRS", "SMS_ENABLED", "TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN_SECRET", "TWILIO_FROM", "CONSENT_DISCLOSURE", "CONSENT_VERSION", "SMS_REQUEST_WORDING", "HTTPS_ENABLED", "ACME_EMAIL", "ACME_HOSTS", "ACME_DIRECTORY_URL", "GCS_TLS_BUCKET"} {
		allowed[key] = true
	}
	return allowed
}()
