package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

func TestCertificateWarmupLogsExcludeProviderResponse(t *testing.T) {
	t.Setenv("HTTPS_ENABLED", "true")
	t.Setenv("PUBLIC_URL", "https://example.com")
	t.Setenv("ACME_HOSTS", "")
	t.Setenv("ACME_CACHE_DIR", t.TempDir())
	t.Setenv("GCS_TLS_BUCKET", "")
	t.Setenv("GCP_PROJECT_ID", "")
	runtime, err := newRuntimeEndpoints(context.Background(), http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeCache()
	// Exercise the real warmup closure while replacing only certificate
	// retrieval, so this test never contacts an ACME or cloud provider.
	runtime.endpoints[1].server.TLSConfig.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return nil, errors.New("private provider body token=secret-value")
	}
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)
	runtime.warmup()
	if !strings.Contains(output.String(), "origin certificate not yet available") || !strings.Contains(output.String(), `"reason":`) {
		t.Fatalf("warmup diagnostic missing: %s", output.String())
	}
	for _, forbidden := range []string{"private provider", "secret-value", `"error":`} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("warmup exposed provider response: %s", output.String())
		}
	}
}
