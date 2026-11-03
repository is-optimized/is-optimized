package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

func TestStartupHTTPTraceDropsAllPrivateCallbackArguments(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "failures"
		}
		t.Run(name, func(t *testing.T) {
			output := captureStartupDiagnostics(t)
			parent := context.Background()
			traced := startupACMEProbeContext(parent)
			trace := httptrace.ContextClientTrace(traced)
			if trace == nil || httptrace.ContextClientTrace(parent) != nil {
				t.Fatal("trace was not isolated to child context")
			}
			var dnsErr, connectErr, tlsErr, writeErr error
			if fail {
				dnsErr = &url.Error{Op: privateDiagnosticSentinel, URL: privateDiagnosticSentinel, Err: context.DeadlineExceeded}
				connectErr = &googleapi.Error{Code: 403, Message: privateDiagnosticSentinel}
				tlsErr = x509.HostnameError{Host: privateDiagnosticSentinel}
				writeErr = errors.New(privateDiagnosticSentinel)
			}
			trace.DNSStart(httptrace.DNSStartInfo{Host: privateDiagnosticSentinel})
			trace.DNSDone(httptrace.DNSDoneInfo{Addrs: []net.IPAddr{{IP: net.ParseIP("192.0.2.123"), Zone: privateDiagnosticSentinel}}, Err: dnsErr})
			trace.ConnectStart(privateDiagnosticSentinel, privateDiagnosticSentinel)
			trace.ConnectDone(privateDiagnosticSentinel, privateDiagnosticSentinel, connectErr)
			trace.TLSHandshakeStart()
			trace.TLSHandshakeDone(tls.ConnectionState{ServerName: privateDiagnosticSentinel}, tlsErr)
			trace.GotConn(httptrace.GotConnInfo{Reused: true})
			trace.WroteRequest(httptrace.WroteRequestInfo{Err: writeErr})
			trace.GotFirstResponseByte()
			if strings.Contains(output.String(), "PRIVATE_") || strings.Contains(output.String(), "192.0.2.123") {
				t.Fatal("HTTP trace exposed callback data")
			}
			decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
			var phases, statuses, reasons []string
			allowed := map[string]bool{"time": true, "level": true, "msg": true, "stage": true, "substage": true, "status": true, "elapsed_ms": true, "reason": true}
			for {
				var event map[string]any
				if err := decoder.Decode(&event); err == io.EOF {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				for key := range event {
					if !allowed[key] {
						t.Fatalf("unexpected trace attribute %s", key)
					}
				}
				if event["stage"] != "https" {
					t.Fatalf("unexpected stage: %v", event)
				}
				if elapsed, ok := event["elapsed_ms"].(float64); !ok || elapsed < 0 {
					t.Fatalf("missing elapsed time: %v", event)
				}
				phases = append(phases, event["substage"].(string))
				statuses = append(statuses, event["status"].(string))
				if reason, ok := event["reason"].(string); ok {
					reasons = append(reasons, reason)
				}
			}
			if want := []string{"probe_dns", "probe_dns", "probe_connect", "probe_connect", "probe_tls", "probe_tls", "probe_connection", "probe_request_written", "probe_first_response_byte"}; !reflect.DeepEqual(phases, want) {
				t.Fatalf("phases=%v", phases)
			}
			want := []string{"starting", "complete", "starting", "complete", "starting", "complete", "complete", "complete", "complete"}
			if fail {
				want = []string{"starting", "failed", "starting", "failed", "starting", "failed", "complete", "failed", "complete"}
			}
			if !reflect.DeepEqual(statuses, want) {
				t.Fatalf("statuses=%v", statuses)
			}
			if fail && !reflect.DeepEqual(reasons, []string{"timeout", "authorization_rejected", "tls_verification_failed", "initialization_or_runtime_error"}) {
				t.Fatalf("categories=%v", reasons)
			}
		})
	}
}

type startupTraceTransport struct{ traced []bool }

func (t *startupTraceTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.traced = append(t.traced, httptrace.ContextClientTrace(r.Context()) != nil)
	return diagnosticHTTPTransport{}.RoundTrip(r)
}

func TestStartupHTTPTraceOnlyWrapsPersistenceProbe(t *testing.T) {
	captureStartupDiagnostics(t)
	transport := &startupTraceTransport{}
	client, err := storage.NewClient(context.Background(), option.WithHTTPClient(&http.Client{Transport: transport}), option.WithoutAuthentication(), storage.WithJSONReads())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	cache := &gcsACMECache{bucket: client.Bucket("PRIVATE_BUCKET"), prefix: "PRIVATE_PREFIX/"}
	ctx := context.Background()
	_ = cache.Put(ctx, "PRIVATE_KEY", []byte("PRIVATE_VALUE"))
	_, _ = cache.Get(ctx, "PRIVATE_KEY")
	_ = cache.Delete(ctx, "PRIVATE_KEY")
	if !reflect.DeepEqual(transport.traced, []bool{false, false, false}) {
		t.Fatalf("normal operations were traced: %v", transport.traced)
	}
	if err := verifyACMECache(ctx, cache); err == nil {
		t.Fatal("fixture should reject probe write")
	}
	if !reflect.DeepEqual(transport.traced, []bool{false, false, false, true}) {
		t.Fatalf("probe context trace missing or leaked: %v", transport.traced)
	}
	if httptrace.ContextClientTrace(ctx) != nil {
		t.Fatal("probe mutated parent context")
	}
	_ = cache.Put(ctx, "PRIVATE_KEY", []byte("PRIVATE_VALUE"))
	_, _ = cache.Get(ctx, "PRIVATE_KEY")
	if !reflect.DeepEqual(transport.traced, []bool{false, false, false, true, false, false}) {
		t.Fatalf("trace leaked into subsequent normal operations: %v", transport.traced)
	}
}
