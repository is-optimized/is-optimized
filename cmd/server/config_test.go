package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/compute/metadata"
)

func fastMetadataPolicy() metadataRetryPolicy {
	return metadataRetryPolicy{totalTimeout: 200 * time.Millisecond, attemptTimeout: 10 * time.Millisecond, initialBackoff: time.Millisecond, maxBackoff: 2 * time.Millisecond}
}

func TestMetadataRetriesTransientUnavailableThenAppliesValidDocument(t *testing.T) {
	t.Setenv("PUBLIC_URL", "https://before.example.com")
	calls := 0
	err := loadMetadataConfig(context.Background(), func(ctx context.Context, key string) (string, error) {
		calls++
		if key != "instance/attributes/is-optimized-config" {
			t.Fatalf("unexpected metadata path %q", key)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > fastMetadataPolicy().attemptTimeout {
			t.Fatal("attempt lacks timeout")
		}
		if calls < 3 {
			if got := os.Getenv("PUBLIC_URL"); got != "https://before.example.com" {
				t.Fatalf("configuration applied before valid response: %q", got)
			}
			return "", &metadata.Error{Code: 503, Message: "unavailable"}
		}
		return `{"PUBLIC_URL":"https://after.example.com"}`, nil
	}, fastMetadataPolicy())
	if err != nil || calls != 3 || os.Getenv("PUBLIC_URL") != "https://after.example.com" {
		t.Fatalf("retry/application result: calls=%d error=%v", calls, err)
	}
}

func TestMetadataAttemptTimeoutIsRetriedInsideOverallBudget(t *testing.T) {
	t.Setenv("PUBLIC_URL", "https://before.example.com")
	policy := fastMetadataPolicy()
	policy.attemptTimeout = time.Millisecond
	calls := 0
	err := loadMetadataConfig(context.Background(), func(ctx context.Context, _ string) (string, error) {
		calls++
		if calls == 1 {
			<-ctx.Done()
			return "", ctx.Err()
		}
		return `{"PUBLIC_URL":"https://after.example.com"}`, nil
	}, policy)
	if err != nil || calls != 2 {
		t.Fatalf("attempt timeout not recovered: calls=%d error=%v", calls, err)
	}
}

func TestMetadataCancellationIsHonored(t *testing.T) {
	for _, cancelInside := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		if !cancelInside {
			cancel()
		}
		err := loadMetadataConfig(ctx, func(context.Context, string) (string, error) {
			calls++
			cancel()
			return "", &metadata.Error{Code: 503, Message: "unavailable"}
		}, fastMetadataPolicy())
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
		wantCalls := 0
		if cancelInside {
			wantCalls = 1
		}
		if calls != wantCalls {
			t.Fatalf("retried cancellation: calls=%d want=%d", calls, wantCalls)
		}
	}
}

func TestMetadataCanceledSuccessfulResponseDoesNotApplyConfiguration(t *testing.T) {
	t.Setenv("PUBLIC_URL", "https://before.example.com")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := loadMetadataConfig(ctx, func(context.Context, string) (string, error) {
		cancel()
		return `{"PUBLIC_URL":"https://changed.example.com"}`, nil
	}, fastMetadataPolicy())
	if !errors.Is(err, context.Canceled) || os.Getenv("PUBLIC_URL") != "https://before.example.com" {
		t.Fatalf("canceled response applied configuration: %v", err)
	}
}

func TestMetadataOverallTimeoutBoundsUnavailableRetries(t *testing.T) {
	policy := fastMetadataPolicy()
	policy.totalTimeout = 15 * time.Millisecond
	calls := 0
	started := time.Now()
	err := loadMetadataConfig(context.Background(), func(context.Context, string) (string, error) {
		calls++
		return "", &metadata.Error{Code: 503, Message: "unavailable"}
	}, policy)
	if !errors.Is(err, context.DeadlineExceeded) || calls < 1 {
		t.Fatalf("total timeout missing: calls=%d error=%v", calls, err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("metadata wait exceeded bounded test budget")
	}
}

func TestMetadataInvalidDocumentFailsWithoutRetryOrPartialEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        error
	}{
		{"malformed", `{"PUBLIC_URL":`, errMetadataInvalid},
		{"unknown key", `{"PUBLIC_URL":"https://changed.example.com","UNSUPPORTED_SECRET":"private"}`, errMetadataUnsupported},
		{"wrong value type", `{"PUBLIC_URL":"https://changed.example.com","SMS_ENABLED":true}`, errMetadataInvalid},
		{"nul value", `{"PUBLIC_URL":"https://changed.example.com","TWILIO_FROM":"\u0000"}`, errMetadataInvalid},
		{"unpinned secret", `{"PUBLIC_URL":"https://changed.example.com","TWILIO_AUTH_TOKEN_SECRET":"projects/demo/secrets/token/versions/latest"}`, errMetadataInvalid},
		{"null", `null`, errMetadataInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PUBLIC_URL", "https://before.example.com")
			t.Setenv("TWILIO_FROM", "before")
			t.Setenv("TWILIO_AUTH_TOKEN_SECRET", "")
			calls := 0
			err := loadMetadataConfig(context.Background(), func(context.Context, string) (string, error) { calls++; return tc.value, nil }, fastMetadataPolicy())
			if !errors.Is(err, tc.want) || calls != 1 {
				t.Fatalf("invalid document retried: calls=%d error=%v", calls, err)
			}
			if os.Getenv("PUBLIC_URL") != "https://before.example.com" || os.Getenv("TWILIO_FROM") != "before" || os.Getenv("TWILIO_AUTH_TOKEN_SECRET") != "" {
				t.Fatal("invalid document left partial configuration")
			}
		})
	}
}

func TestMetadataPermanentErrorsAreNotRetriedOrExposed(t *testing.T) {
	for _, upstream := range []error{&metadata.Error{Code: 404, Message: "private upstream response"}, &metadata.Error{Code: 403, Message: "private upstream response"}, errors.New("private upstream response")} {
		calls := 0
		err := loadMetadataConfig(context.Background(), func(context.Context, string) (string, error) { calls++; return "", upstream }, fastMetadataPolicy())
		if !errors.Is(err, errMetadataUnavailable) || calls != 1 || strings.Contains(err.Error(), "private") {
			t.Fatalf("permanent upstream error handling wrong: calls=%d error=%v", calls, err)
		}
	}
}

func TestStartupLogsNeverIncludeRawProviderError(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)
	startupStage("metadata", "starting")
	startupFailure("storage", errors.New("Authorization: Bearer PRIVATE_TOKEN; project config PRIVATE_VALUE"))
	startupFailure("metadata", errMetadataInvalid)
	startupFailure("metadata", context.DeadlineExceeded)
	text := output.String()
	for _, secret := range []string{"PRIVATE_TOKEN", "PRIVATE_VALUE", "Authorization", "Bearer"} {
		if strings.Contains(text, secret) {
			t.Fatalf("startup logs exposed provider details: %s", text)
		}
	}
	for _, want := range []string{`"stage":"metadata"`, `"stage":"storage"`, `"reason":"invalid_metadata"`, `"reason":"timeout"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("startup diagnostic missing %s", want)
		}
	}
}
