package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/net/idna"
)

const acmeStagingDirectory = "https://acme-staging-v02.api.letsencrypt.org/directory"

type httpsOptions struct {
	origin    *url.URL
	hosts     []string
	directory string
	email     string
}

func parseHTTPSOptions(publicURL, names, directory, email string) (httpsOptions, error) {
	var out httpsOptions
	u, err := url.Parse(publicURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return out, errors.New("HTTPS requires a public HTTPS origin on port 443")
	}
	host, err := certificateHost(u.Hostname())
	if err != nil {
		return out, err
	}
	u.Host, u.Path = host, ""
	out.origin = u
	out.hosts = []string{host}
	seen := map[string]bool{host: true}
	if names != "" {
		for _, raw := range strings.Split(names, ",") {
			name, err := certificateHost(strings.TrimSpace(raw))
			if err != nil {
				return out, err
			}
			if !seen[name] {
				out.hosts = append(out.hosts, name)
				seen[name] = true
			}
		}
	}
	if len(out.hosts) > 8 {
		return out, errors.New("ACME host allowlist exceeds eight names")
	}
	if directory == "" {
		directory = acme.LetsEncryptURL
	}
	if directory != acme.LetsEncryptURL && directory != acmeStagingDirectory {
		return out, errors.New("unsupported ACME directory")
	}
	out.directory, out.email = directory, email
	return out, nil
}

func certificateHost(raw string) (string, error) {
	host, err := idna.Lookup.ToASCII(strings.ToLower(raw))
	if err != nil || host == "" || strings.TrimSpace(host) != host || strings.HasSuffix(host, ".") || net.ParseIP(host) != nil || !strings.Contains(host, ".") || len(host) > 253 {
		return "", errors.New("ACME requires explicit DNS hostnames")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("invalid ACME hostname")
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return "", errors.New("invalid ACME hostname")
			}
		}
	}
	return host, nil
}

func requestHostname(authority string) string {
	host := authority
	if strings.Contains(authority, ":") {
		var err error
		host, _, err = net.SplitHostPort(authority)
		if err != nil {
			return ""
		}
	}
	h, err := certificateHost(host)
	if err != nil {
		return ""
	}
	return h
}

func (o httpsOptions) restrictHosts(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, host := range o.hosts {
		allowed[host] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[requestHostname(r.Host)] {
			http.Error(w, "Unrecognized host", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (o httpsOptions) redirect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Use the canonical HTTPS origin", http.StatusUpgradeRequired)
		return
	}
	// Copy only path/query; an attacker-controlled Host or absolute-form URL
	// cannot turn this into an external redirect.
	dest := *o.origin
	dest.Path, dest.RawPath, dest.RawQuery = r.URL.Path, r.URL.RawPath, r.URL.RawQuery
	if dest.Path == "" {
		dest.Path = "/"
	}
	http.Redirect(w, r, dest.String(), http.StatusPermanentRedirect)
}
func (o httpsOptions) canonicalApp(app http.Handler) http.Handler {
	return o.restrictHosts(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestHostname(r.Host) != o.origin.Hostname() {
			o.redirect(w, r)
			return
		}
		app.ServeHTTP(w, r)
	}))
}

type servingEndpoint struct {
	server *http.Server
	tls    bool
}
type runtimeEndpoints struct {
	endpoints  []servingEndpoint
	closeCache func() error
	warmup     func()
}

func standardHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
}
func newRuntimeEndpoints(ctx context.Context, app http.Handler) (runtimeEndpoints, error) {
	out := runtimeEndpoints{closeCache: func() error { return nil }}
	if os.Getenv("HTTPS_ENABLED") != "true" {
		out.endpoints = []servingEndpoint{{server: standardHTTPServer(env("LISTEN_ADDR", ":8080"), app)}}
		return out, nil
	}
	o, err := parseHTTPSOptions(os.Getenv("PUBLIC_URL"), os.Getenv("ACME_HOSTS"), os.Getenv("ACME_DIRECTORY_URL"), os.Getenv("ACME_EMAIL"))
	if err != nil {
		return out, err
	}
	var cache autocert.Cache
	namespace := sha256.Sum256([]byte(o.directory))
	prefix := "acme/v1/" + hex.EncodeToString(namespace[:]) + "/"
	if bucket := os.Getenv("GCS_TLS_BUCKET"); bucket != "" {
		if bucket == os.Getenv("GCS_REPORT_BUCKET") {
			return out, errors.New("TLS cache must use a dedicated bucket without report retention rules")
		}
		cache, out.closeCache, err = newGCSACMECache(ctx, bucket, prefix)
		if err != nil {
			return out, err
		}
	} else {
		// A cloud deployment must not silently fall back to unqualified boot-disk
		// persistence. Local development can explicitly choose a durable directory.
		if os.Getenv("GCP_PROJECT_ID") != "" {
			return out, errors.New("HTTPS in GCP requires a dedicated private GCS_TLS_BUCKET")
		}
		dir := os.Getenv("ACME_CACHE_DIR")
		if dir == "" {
			return out, errors.New("HTTPS requires a persistent ACME cache")
		}
		cache = autocert.DirCache(filepath.Join(dir, hex.EncodeToString(namespace[:])))
	}
	if err := verifyACMECache(ctx, cache); err != nil {
		_ = out.closeCache()
		return out, err
	}
	manager := &autocert.Manager{Prompt: autocert.AcceptTOS, Cache: cache, Email: o.email, HostPolicy: autocert.HostWhitelist(o.hosts...), Client: &acme.Client{DirectoryURL: o.directory}}
	// Calling HTTPHandler enables HTTP-01. autocert first tries TLS-ALPN-01,
	// then falls back to HTTP-01 when a proxy cannot pass the ALPN challenge.
	challengeHandler := manager.HTTPHandler(http.HandlerFunc(o.redirect))
	httpHandler := o.restrictHosts(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Use the canonical HTTPS origin", http.StatusUpgradeRequired)
			return
		}
		request := r.Clone(r.Context())
		request.Host = requestHostname(r.Host)
		challengeHandler.ServeHTTP(w, request)
	}))
	tlsConfig := manager.TLSConfig()
	tlsConfig.MinVersion = tls.VersionTLS12
	getCertificate := tlsConfig.GetCertificate
	tlsConfig.GetCertificate = func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		// Manager.HostPolicy is not consulted for cached certificates; enforce
		// the current allowlist on every handshake as well.
		if err := manager.HostPolicy(hello.Context(), hello.ServerName); err != nil {
			return nil, errors.New("unrecognized TLS server name")
		}
		return getCertificate(hello)
	}
	secure := standardHTTPServer(env("HTTPS_LISTEN_ADDR", ":443"), o.canonicalApp(app))
	secure.TLSConfig = tlsConfig
	out.endpoints = []servingEndpoint{{server: standardHTTPServer(env("HTTP_LISTEN_ADDR", ":80"), httpHandler)}, {server: secure, tls: true}}
	out.warmup = func() {
		for _, host := range o.hosts {
			_, err := tlsConfig.GetCertificate(&tls.ClientHelloInfo{ServerName: host, SignatureSchemes: []tls.SignatureScheme{tls.ECDSAWithP256AndSHA256}, SupportedCurves: []tls.CurveID{tls.CurveP256}, CipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}})
			if err != nil {
				slog.Warn("origin certificate not yet available", "host", host, "reason", startupErrorCategory(err))
			} else {
				slog.Info("origin certificate available", "host", host)
			}
		}
	}
	return out, nil
}

// Bind every required port before starting any service. Failure of either
// listener shuts down the other, so plaintext cannot remain as a partial start.
func serveRuntime(ctx context.Context, runtime runtimeEndpoints) error {
	listeners := make([]net.Listener, 0, len(runtime.endpoints))
	defer func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}()
	for _, endpoint := range runtime.endpoints {
		listener, err := net.Listen("tcp", endpoint.server.Addr)
		if err != nil {
			return fmt.Errorf("bind service listener: %w", err)
		}
		listeners = append(listeners, listener)
	}
	done := make(chan error, len(runtime.endpoints))
	for i, endpoint := range runtime.endpoints {
		go func(e servingEndpoint, l net.Listener) {
			slog.Info("service listener bound", "address", l.Addr().String(), "tls", e.tls)
			if e.tls {
				done <- e.server.ServeTLS(l, "", "")
			} else {
				done <- e.server.Serve(l)
			}
		}(endpoint, listeners[i])
	}
	if runtime.warmup != nil {
		go runtime.warmup()
	}
	var result error
	select {
	case result = <-done:
	case <-ctx.Done():
	}
	stop, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, endpoint := range runtime.endpoints {
		if err := endpoint.server.Shutdown(stop); err != nil {
			_ = endpoint.server.Close()
			if result == nil {
				result = err
			}
		}
	}
	if errors.Is(result, http.ErrServerClosed) {
		return nil
	}
	return result
}

// Reject an unavailable/unwritable persistent cache before any certificate order.
func verifyACMECache(ctx context.Context, cache autocert.Cache) error {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return errors.New("TLS cache probe entropy unavailable")
	}
	key := "startup-probe-" + hex.EncodeToString(nonce[:])
	if err := cache.Put(ctx, key, nonce[:]); err != nil {
		return errors.New("TLS cache is not writable")
	}
	data, err := cache.Get(ctx, key)
	deleteErr := cache.Delete(ctx, key)
	if err != nil || !bytes.Equal(data, nonce[:]) {
		return errors.New("TLS cache persistence check failed")
	}
	if deleteErr != nil {
		return errors.New("TLS cache cleanup check failed")
	}
	return nil
}
