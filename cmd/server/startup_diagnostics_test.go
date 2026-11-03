package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/oauth2"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

const privateDiagnosticSentinel = "PRIVATE_BUCKET/PRIVATE_KEY?Authorization=PRIVATE_CREDENTIAL"

type diagnosticTimeout struct{}

func (diagnosticTimeout) Error() string   { return privateDiagnosticSentinel }
func (diagnosticTimeout) Timeout() bool   { return true }
func (diagnosticTimeout) Temporary() bool { return true }

func captureStartupDiagnostics(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &output
}

func TestStartupTypedCausesStayPrivate(t *testing.T) {
	for _, test := range []struct {
		name  string
		cause error
		want  string
	}{
		{"deadline", context.DeadlineExceeded, "timeout"},
		{"network deadline", &url.Error{Op: "Get", URL: privateDiagnosticSentinel, Err: diagnosticTimeout{}}, "timeout"},
		{"TLS verification", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, "tls_verification_failed"},
		{"unknown authority", x509.UnknownAuthorityError{}, "tls_verification_failed"},
		{"certificate invalid", x509.CertificateInvalidError{}, "tls_verification_failed"},
		{"hostname", x509.HostnameError{}, "tls_verification_failed"},
		{"system roots", x509.SystemRootsError{Err: errors.New(privateDiagnosticSentinel)}, "tls_verification_failed"},
		{"unauthenticated", &googleapi.Error{Code: 401, Message: privateDiagnosticSentinel}, "authentication_rejected"},
		{"unauthorized", &googleapi.Error{Code: 403, Message: privateDiagnosticSentinel}, "authorization_rejected"},
		{"missing", &googleapi.Error{Code: 404, Message: privateDiagnosticSentinel}, "not_found"},
		{"rate limit", &googleapi.Error{Code: 429, Message: privateDiagnosticSentinel}, "provider_unavailable"},
		{"provider unavailable", &googleapi.Error{Code: 503, Message: privateDiagnosticSentinel}, "provider_unavailable"},
		{"unclassified status", &googleapi.Error{Code: 409, Message: privateDiagnosticSentinel}, "initialization_or_runtime_error"},
		{"token rejected", &oauth2.RetrieveError{Response: &http.Response{StatusCode: 403}, Body: []byte(privateDiagnosticSentinel)}, "authorization_rejected"},
		{"cache miss", autocert.ErrCacheMiss, "not_found"},
		{"unknown", errors.New(privateDiagnosticSentinel), "initialization_or_runtime_error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := captureStartupDiagnostics(t)
			wrapped := &safeStartupError{message: "TLS cache operation failed", cause: test.cause}
			err := &startupInitializationError{cause: wrapped}
			if got := startupErrorCategory(err); got != test.want {
				t.Fatalf("category=%s want=%s", got, test.want)
			}
			if !errors.Is(err, test.cause) {
				t.Fatal("typed cause was discarded")
			}
			if err.Error() != "service initialization failed" || wrapped.Error() != "TLS cache operation failed" {
				t.Fatal("nonconstant error message")
			}
			startupFailure("https", err)
			if strings.Contains(output.String(), "PRIVATE_") || strings.Contains(fmt.Sprint(err, wrapped), "PRIVATE_") {
				t.Fatal("private provider information escaped")
			}
			if !strings.Contains(output.String(), `"reason":"`+test.want+`"`) {
				t.Fatalf("diagnostic missing bounded category: %s", output.String())
			}
		})
	}
}

type diagnosticCache struct {
	data                      []byte
	calls                     []string
	putErr, getErr, deleteErr error
	mismatch                  bool
}

func (c *diagnosticCache) Put(_ context.Context, _ string, data []byte) error {
	c.calls = append(c.calls, "write")
	c.data = append([]byte(nil), data...)
	return c.putErr
}
func (c *diagnosticCache) Get(_ context.Context, _ string) ([]byte, error) {
	c.calls = append(c.calls, "read")
	if c.mismatch {
		return []byte("PRIVATE_PROBE_VALUE"), nil
	}
	return c.data, c.getErr
}
func (c *diagnosticCache) Delete(_ context.Context, _ string) error {
	c.calls = append(c.calls, "delete")
	return c.deleteErr
}

func TestStartupCacheSubstageOrderingAndCleanup(t *testing.T) {
	private := &googleapi.Error{Code: 403, Message: privateDiagnosticSentinel}
	for _, test := range []struct {
		name                  string
		cache                 diagnosticCache
		wantCalls, wantEvents []string
		wantErr               bool
	}{
		{"success", diagnosticCache{}, []string{"write", "read", "delete"}, []string{"cache_write:starting", "cache_write:complete", "cache_read:starting", "cache_read:complete", "cache_delete:starting", "cache_delete:complete"}, false},
		{"write failure", diagnosticCache{putErr: private}, []string{"write"}, []string{"cache_write:starting", "cache_write:failed"}, true},
		{"read failure still cleans up", diagnosticCache{getErr: private}, []string{"write", "read", "delete"}, []string{"cache_write:starting", "cache_write:complete", "cache_read:starting", "cache_read:failed", "cache_delete:starting", "cache_delete:complete"}, true},
		{"delete failure", diagnosticCache{deleteErr: private}, []string{"write", "read", "delete"}, []string{"cache_write:starting", "cache_write:complete", "cache_read:starting", "cache_read:complete", "cache_delete:starting", "cache_delete:failed"}, true},
		{"mismatch still cleans up", diagnosticCache{mismatch: true}, []string{"write", "read", "delete"}, []string{"cache_write:starting", "cache_write:complete", "cache_read:starting", "cache_read:failed", "cache_delete:starting", "cache_delete:complete"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := captureStartupDiagnostics(t)
			err := verifyACMECache(context.Background(), &test.cache)
			if (err != nil) != test.wantErr {
				t.Fatalf("unexpected result: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE_") {
				t.Fatal("provider message escaped through Error")
			}
			if err != nil && !test.cache.mismatch && !errors.Is(err, private) {
				t.Fatal("cache verification discarded typed cause")
			}
			if !reflect.DeepEqual(test.cache.calls, test.wantCalls) {
				t.Fatalf("calls=%v want=%v", test.cache.calls, test.wantCalls)
			}
			if strings.Contains(output.String(), "PRIVATE_") || strings.Contains(output.String(), "startup-probe-") {
				t.Fatal("provider details or object key escaped into logs")
			}
			decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
			var events []string
			for {
				var event map[string]any
				if err := decoder.Decode(&event); err == io.EOF {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				if event["stage"] != "https" {
					t.Fatalf("unexpected stage=%v", event)
				}
				elapsed, ok := event["elapsed_ms"].(float64)
				if !ok || elapsed < 0 {
					t.Fatalf("missing elapsed time=%v", event)
				}
				events = append(events, event["substage"].(string)+":"+event["status"].(string))
			}
			if !reflect.DeepEqual(events, test.wantEvents) {
				t.Fatalf("events=%v want=%v", events, test.wantEvents)
			}
		})
	}
}

func TestStartupClientDiagnosticOperationPreservesCause(t *testing.T) {
	output := captureStartupDiagnostics(t)
	cause := &googleapi.Error{Code: 401, Message: privateDiagnosticSentinel}
	err := startupHTTPSOperation(httpsCacheClient, func() error { return &safeStartupError{message: "TLS cache identity unavailable", cause: cause} })
	if !errors.Is(err, cause) || startupErrorCategory(err) != "authentication_rejected" {
		t.Fatal("client cause was not preserved")
	}
	for _, want := range []string{`"substage":"cache_client"`, `"status":"starting"`, `"status":"failed"`, `"reason":"authentication_rejected"`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing diagnostic %s", want)
		}
	}
	if strings.Contains(output.String(), "PRIVATE_") {
		t.Fatal("provider information escaped")
	}
}

type diagnosticHTTPTransport struct{}

func (diagnosticHTTPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	// An injected in-memory transport prevents credential lookup and provider
	// traffic while exercising the storage SDK's real error conversion.
	return &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":403,"message":"` + privateDiagnosticSentinel + `"}}`)), Request: request}, nil
}

func TestStartupGCSCacheErrorsKeepTypedCauseAndHideProviderText(t *testing.T) {
	output := captureStartupDiagnostics(t)
	client, err := storage.NewClient(context.Background(), option.WithHTTPClient(&http.Client{Transport: diagnosticHTTPTransport{}}), option.WithoutAuthentication(), storage.WithJSONReads())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	cache := &gcsACMECache{bucket: client.Bucket("PRIVATE_BUCKET"), prefix: "PRIVATE_PREFIX/"}
	for _, operation := range []func() error{
		func() error { return cache.Put(context.Background(), "PRIVATE_KEY", []byte("PRIVATE_VALUE")) },
		func() error { _, err := cache.Get(context.Background(), "PRIVATE_KEY"); return err },
		func() error { return cache.Delete(context.Background(), "PRIVATE_KEY") },
	} {
		err := operation()
		var provider *googleapi.Error
		if err == nil || !errors.As(err, &provider) || provider.Code != http.StatusForbidden {
			t.Fatalf("cache operation did not retain provider type: %v", err)
		}
		if strings.Contains(err.Error(), "PRIVATE_") {
			t.Fatal("cache Error exposed provider data")
		}
		if startupErrorCategory(err) != "authorization_rejected" {
			t.Fatalf("wrong category: %s", startupErrorCategory(err))
		}
		startupFailure("https", err)
	}
	if strings.Contains(output.String(), "PRIVATE_") {
		t.Fatal("cache log exposed provider data")
	}
}

func TestStartupDiagnosticGraceIsBoundedAndOnlyForGCPInitialization(t *testing.T) {
	for _, test := range []struct {
		name, metadata string
		err            error
		want           bool
	}{
		{"successful run", "true", nil, false},
		{"local startup failure", "false", &startupInitializationError{cause: errors.New("failure")}, false},
		{"GCP runtime failure", "true", errors.New("runtime failure"), false},
		{"GCP startup failure", "true", &startupInitializationError{cause: context.DeadlineExceeded}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CONFIG_FROM_METADATA", test.metadata)
			var waits []time.Duration
			waitForStartupDiagnostics(test.err, func(d time.Duration) { waits = append(waits, d) })
			if test.want {
				if !reflect.DeepEqual(waits, []time.Duration{30 * time.Second}) {
					t.Fatalf("waits=%v", waits)
				}
			} else if len(waits) != 0 {
				t.Fatalf("unexpected delay: %v", waits)
			}
		})
	}
}
