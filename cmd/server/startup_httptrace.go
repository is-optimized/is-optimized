package main

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http/httptrace"
	"time"
)

type startupHTTPPhase string

const (
	startupProbeDNS        startupHTTPPhase = "probe_dns"
	startupProbeConnect    startupHTTPPhase = "probe_connect"
	startupProbeTLS        startupHTTPPhase = "probe_tls"
	startupProbeConnection startupHTTPPhase = "probe_connection"
	startupProbeRequest    startupHTTPPhase = "probe_request_written"
	startupProbeResponse   startupHTTPPhase = "probe_first_response_byte"
)

// Attach tracing only to the startup persistence probe, never the storage client
// or its normal Get/Put contexts. Each elapsed_ms is measured from attachment of
// this probe's trace, allowing phase durations to be compared without request
// identifiers. Retries and credential subrequests may produce repeated phases;
// absent phases do not establish the behavior of untraced library internals.
// Callback arguments contain private hosts, addresses, certificate identities,
// and possibly provider error text: deliberately discard them. Only fixed phase
// and status labels, elapsed time and the existing error category are recorded.
func startupACMEProbeContext(ctx context.Context) context.Context {
	start := time.Now()
	record := func(phase startupHTTPPhase, starting bool, err error) {
		status := "complete"
		if starting {
			status = "starting"
		} else if err != nil {
			status = "failed"
		}
		args := []any{"stage", "https", "substage", string(phase), "status", status, "elapsed_ms", time.Since(start).Milliseconds()}
		if err != nil {
			args = append(args, "reason", startupErrorCategory(err))
			slog.Error("startup stage", args...)
		} else {
			slog.Info("startup stage", args...)
		}
	}
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		DNSStart:             func(_ httptrace.DNSStartInfo) { record(startupProbeDNS, true, nil) },
		DNSDone:              func(info httptrace.DNSDoneInfo) { record(startupProbeDNS, false, info.Err) },
		ConnectStart:         func(_, _ string) { record(startupProbeConnect, true, nil) },
		ConnectDone:          func(_, _ string, err error) { record(startupProbeConnect, false, err) },
		TLSHandshakeStart:    func() { record(startupProbeTLS, true, nil) },
		TLSHandshakeDone:     func(_ tls.ConnectionState, err error) { record(startupProbeTLS, false, err) },
		GotConn:              func(_ httptrace.GotConnInfo) { record(startupProbeConnection, false, nil) },
		WroteRequest:         func(info httptrace.WroteRequestInfo) { record(startupProbeRequest, false, info.Err) },
		GotFirstResponseByte: func() { record(startupProbeResponse, false, nil) },
	})
}
