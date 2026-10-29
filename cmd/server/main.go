package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/is-optimized/is-optimized/internal/messaging"
	"github.com/is-optimized/is-optimized/internal/scanner"
	"github.com/is-optimized/is-optimized/internal/server"
	"github.com/is-optimized/is-optimized/internal/store"
)

func main() {
	if e := run(); e != nil {
		slog.Error("service stopped", "error", e)
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
	if err := loadRuntimeConfig(ctx); err != nil {
		return err
	}
	var db store.Store
	var e error
	if os.Getenv("GCP_PROJECT_ID") != "" {
		db, e = store.NewCloud(ctx, os.Getenv("GCP_PROJECT_ID"), os.Getenv("GCS_REPORT_BUCKET"))
	} else {
		db, e = store.NewLocal(env("DATA_DIR", ".data"))
	}
	if e != nil {
		return e
	}
	defer db.Close()
	cfg := server.Config{TrustedProxyCIDRs: os.Getenv("TRUSTED_PROXY_CIDRS"), PublicURL: env("PUBLIC_URL", "http://localhost:8080"), TwilioAccount: os.Getenv("TWILIO_ACCOUNT_SID"), TwilioToken: os.Getenv("TWILIO_AUTH_TOKEN"), TwilioFrom: os.Getenv("TWILIO_FROM"), Disclosure: os.Getenv("CONSENT_DISCLOSURE"), DisclosureVersion: os.Getenv("CONSENT_VERSION"), SMSWording: os.Getenv("SMS_REQUEST_WORDING"), SMS: os.Getenv("SMS_ENABLED") == "true"}
	sender := &messaging.Twilio{Account: cfg.TwilioAccount, Token: cfg.TwilioToken, From: cfg.TwilioFrom}
	app, e := server.New(db, scanner.New(), sender, cfg)
	if e != nil {
		return e
	}
	runtime, e := newRuntimeEndpoints(ctx, app)
	if e != nil {
		return e
	}
	defer runtime.closeCache()
	workerDone := make(chan struct{})
	go func() { app.Run(ctx); close(workerDone) }()
	e = serveRuntime(ctx, runtime)
	cancel()
	<-workerDone
	return e
}
