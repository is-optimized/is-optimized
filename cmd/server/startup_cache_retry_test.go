package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
)

type retryProbeWrite struct {
	key   string
	nonce []byte
}
type retryProbeCache struct {
	puts     []retryProbeWrite
	results  []error
	afterPut func()
}

func (c *retryProbeCache) Put(_ context.Context, key string, data []byte) error {
	c.puts = append(c.puts, retryProbeWrite{key: key, nonce: append([]byte(nil), data...)})
	if c.afterPut != nil {
		c.afterPut()
	}
	if len(c.results) < len(c.puts) {
		return nil
	}
	return c.results[len(c.puts)-1]
}
func (c *retryProbeCache) Get(context.Context, string) ([]byte, error) {
	panic("Put retry helper must not read")
}
func (c *retryProbeCache) Delete(context.Context, string) error {
	panic("Put retry helper must not delete")
}

func TestStartupCacheWriteRetriesOnlyTypedDeadlineWithSameIdentity(t *testing.T) {
	for _, test := range []struct {
		name         string
		results      []error
		wantAttempts int
		wantWaits    []time.Duration
		wantErr      error
	}{
		{"first timeout then success", []error{context.DeadlineExceeded, nil}, 2, []time.Duration{time.Second}, nil},
		{"wrapped timeout then success", []error{&safeStartupError{message: "TLS cache write failed", cause: context.DeadlineExceeded}, nil}, 2, []time.Duration{time.Second}, nil},
		{"two timeouts then success", []error{context.DeadlineExceeded, context.DeadlineExceeded, nil}, 3, []time.Duration{time.Second, 2 * time.Second}, nil},
		{"three timeouts exhaust limit", []error{context.DeadlineExceeded, context.DeadlineExceeded, context.DeadlineExceeded, nil}, 3, []time.Duration{time.Second, 2 * time.Second}, context.DeadlineExceeded},
		{"success needs no retry", nil, 1, nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := captureStartupDiagnostics(t)
			cache := &retryProbeCache{results: test.results}
			var waits []time.Duration
			err := putStartupACMEProbe(context.Background(), cache, privateDiagnosticSentinel, []byte("PRIVATE_NONCE"), func(ctx context.Context, d time.Duration) error {
				if ctx.Err() != nil {
					t.Fatal("retry wait received canceled parent")
				}
				waits = append(waits, d)
				return nil
			})
			if !errors.Is(err, test.wantErr) || len(cache.puts) != test.wantAttempts || !reflect.DeepEqual(waits, test.wantWaits) {
				t.Fatalf("err=%v attempts=%d waits=%v", err, len(cache.puts), waits)
			}
			for _, put := range cache.puts {
				if put.key != privateDiagnosticSentinel || !bytes.Equal(put.nonce, []byte("PRIVATE_NONCE")) {
					t.Fatal("retry changed probe identity or data")
				}
			}
			if strings.Contains(output.String(), "PRIVATE_") {
				t.Fatal("retry logs exposed object identity or data")
			}
		})
	}
}

func TestStartupCacheWriteDoesNotRetryPermanentOrUnknownErrors(t *testing.T) {
	for _, cause := range []error{
		&googleapi.Error{Code: 403, Message: privateDiagnosticSentinel},
		&googleapi.Error{Code: 401, Message: privateDiagnosticSentinel},
		&googleapi.Error{Code: 429, Message: privateDiagnosticSentinel},
		&googleapi.Error{Code: 503, Message: privateDiagnosticSentinel},
		x509.HostnameError{Host: privateDiagnosticSentinel},
		diagnosticTimeout{}, // A timeout label alone is not a typed context deadline.
		errors.New("context deadline exceeded " + privateDiagnosticSentinel),
		context.Canceled,
	} {
		output := captureStartupDiagnostics(t)
		cache := &retryProbeCache{results: []error{cause, nil}}
		err := putStartupACMEProbe(context.Background(), cache, "PRIVATE_KEY", []byte("PRIVATE_NONCE"), func(context.Context, time.Duration) error { t.Fatal("non-deadline error retried"); return nil })
		if !errors.Is(err, cause) || len(cache.puts) != 1 {
			t.Fatalf("unexpected err=%v attempts=%d", err, len(cache.puts))
		}
		if strings.Contains(output.String(), "PRIVATE_") {
			t.Fatal("provider details escaped retry log")
		}
	}
}

func TestStartupCacheRetryRespectsParentCancellation(t *testing.T) {
	for _, when := range []string{"before first attempt", "during attempt", "during backoff", "after backoff"} {
		t.Run(when, func(t *testing.T) {
			captureStartupDiagnostics(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cache := &retryProbeCache{results: []error{context.DeadlineExceeded, nil}}
			if when == "before first attempt" {
				cancel()
			}
			if when == "during attempt" {
				cache.afterPut = cancel
			}
			waits := 0
			err := putStartupACMEProbe(ctx, cache, "PRIVATE_KEY", []byte("PRIVATE_NONCE"), func(ctx context.Context, d time.Duration) error {
				waits++
				cancel()
				if when == "after backoff" {
					return nil
				} // Loop must re-check even an injected successful wait.
				return startupCacheRetryWait(ctx, d)
			})
			want := 1
			if when == "before first attempt" {
				want = 0
			}
			if !errors.Is(err, context.Canceled) || len(cache.puts) != want {
				t.Fatalf("err=%v attempts=%d", err, len(cache.puts))
			}
			if (when == "before first attempt" || when == "during attempt") && waits != 0 {
				t.Fatal("canceled parent entered backoff")
			}
			if (when == "during backoff" || when == "after backoff") && waits != 1 {
				t.Fatal("expected one cancellable backoff")
			}
		})
	}
}

func TestStartupCacheRetryAttemptEventsAreBoundedAndPrivate(t *testing.T) {
	output := captureStartupDiagnostics(t)
	cache := &retryProbeCache{results: []error{context.DeadlineExceeded, context.DeadlineExceeded, nil}}
	if err := putStartupACMEProbe(context.Background(), cache, privateDiagnosticSentinel, []byte("PRIVATE_NONCE"), func(context.Context, time.Duration) error { return nil }); err != nil {
		t.Fatal(err)
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
			t.Fatal("unexpected stage")
		}
		attempt, ok := event["attempt"].(float64)
		if !ok || attempt < 1 || attempt > 3 {
			t.Fatal("unbounded attempt field")
		}
		for key := range event {
			switch key {
			case "time", "level", "msg", "stage", "substage", "status", "attempt", "elapsed_ms", "reason", "backoff_ms":
			default:
				t.Fatalf("unexpected log field %s", key)
			}
		}
		events = append(events, event["substage"].(string)+":"+event["status"].(string))
		if event["substage"] == "cache_write_retry" {
			if delay := event["backoff_ms"]; delay != float64(1000) && delay != float64(2000) {
				t.Fatal("unbounded backoff")
			}
		} else {
			if elapsed, ok := event["elapsed_ms"].(float64); !ok || elapsed < 0 {
				t.Fatal("missing attempt elapsed time")
			}
		}
	}
	want := []string{"cache_write_attempt:starting", "cache_write_attempt:failed", "cache_write_retry:waiting", "cache_write_attempt:starting", "cache_write_attempt:failed", "cache_write_retry:waiting", "cache_write_attempt:starting", "cache_write_attempt:complete"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events=%v", events)
	}
	if strings.Contains(output.String(), "PRIVATE_") {
		t.Fatal("private retry data logged")
	}
}
