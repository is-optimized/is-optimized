package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/is-optimized/is-optimized/internal/messaging"
	"github.com/is-optimized/is-optimized/internal/scanner"
	"github.com/is-optimized/is-optimized/internal/server"
	"github.com/is-optimized/is-optimized/internal/store"
)

func main() {
	if e := run(); e != nil {
		slog.Error("service stopped", "reason", startupErrorCategory(e))
		os.Exit(1)
	}
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	startupStage("metadata", "starting")
	if err := loadRuntimeConfig(ctx); err != nil {
		startupFailure("metadata", err)
		return err
	}
	startupStage("metadata", "complete")
	startupStage("storage", "starting")
	var db store.Store
	var e error
	if os.Getenv("GCP_PROJECT_ID") != "" {
		db, e = store.NewCloud(ctx, os.Getenv("GCP_PROJECT_ID"), os.Getenv("GCS_REPORT_BUCKET"))
	} else {
		db, e = store.NewLocal(env("DATA_DIR", ".data"))
	}
	if e != nil {
		startupFailure("storage", e)
		return e
	}
	defer db.Close()
	startupStage("storage", "complete")
	cfg := server.Config{TrustedProxyCIDRs: os.Getenv("TRUSTED_PROXY_CIDRS"), PublicURL: env("PUBLIC_URL", "http://localhost:8080"), TwilioAccount: os.Getenv("TWILIO_ACCOUNT_SID"), TwilioToken: os.Getenv("TWILIO_AUTH_TOKEN"), TwilioFrom: os.Getenv("TWILIO_FROM"), Disclosure: os.Getenv("CONSENT_DISCLOSURE"), DisclosureVersion: os.Getenv("CONSENT_VERSION"), SMSWording: os.Getenv("SMS_REQUEST_WORDING"), SMS: os.Getenv("SMS_ENABLED") == "true"}
	sender := &messaging.Twilio{Account: cfg.TwilioAccount, Token: cfg.TwilioToken, From: cfg.TwilioFrom}
	app, e := server.New(db, scanner.New(), sender, cfg)
	if e != nil {
		startupFailure("application", e)
		return e
	}
	startupStage("https", "starting")
	runtime, e := newRuntimeEndpoints(ctx, app)
	if e != nil {
		startupFailure("https", e)
		return e
	}
	defer runtime.closeCache()
	startupStage("https", "configured")
	// Port binding happens inside serveRuntime. Do not claim ready until an
	// actual parsed request reaches a listener; HTTPS setup alone is not ready.
	var ready sync.Once
	for _, endpoint := range runtime.endpoints {
		previous := endpoint.server.ConnState
		endpoint.server.ConnState = func(c net.Conn, state http.ConnState) {
			if previous != nil {
				previous(c, state)
			}
			if state == http.StateActive {
				ready.Do(func() { startupStage("ready", "request_observed") })
			}
		}
	}
	startupStage("ready", "listeners_starting")
	workerDone := make(chan struct{})
	go func() { app.Run(ctx); close(workerDone) }()
	e = serveRuntime(ctx, runtime)
	if e != nil {
		startupFailure("serve", e)
	}
	cancel()
	<-workerDone
	return e
}

// Only fixed stage names/statuses and bounded categories are logged. Provider
// errors may contain request/response details, so never log their raw strings.
func startupStage(stage, status string) {
	slog.Info("startup stage", "stage", stage, "status", status)
}

func startupFailure(stage string, err error) {
	slog.Error("startup stage", "stage", stage, "status", "failed", "reason", startupErrorCategory(err))
}

func startupErrorCategory(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, errMetadataInvalid):
		return "invalid_metadata"
	case errors.Is(err, errMetadataUnsupported):
		return "unsupported_metadata_key"
	case errors.Is(err, errMetadataUnavailable):
		return "metadata_unavailable"
	default:
		return "initialization_or_runtime_error"
	}
}
