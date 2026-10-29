package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"golang.org/x/crypto/acme/autocert"
	"google.golang.org/api/option"
)

func TestHTTPSHostAndRedirectPolicy(t *testing.T) {
	opts, err := parseHTTPSOptions("https://example.com", "www.example.com", "", "")
	if err != nil {
		t.Fatal(err)
	}
	handler := opts.restrictHosts(http.HandlerFunc(opts.redirect))
	for _, tc := range []struct {
		method, host, target string
		status               int
		location             string
	}{
		{"GET", "example.com", "http://example.com/a?q=hello", 308, "https://example.com/a?q=hello"},
		{"HEAD", "www.example.com", "http://www.example.com/a", 308, "https://example.com/a"},
		{"GET", "evil.example", "http://evil.example/a", 421, ""},
		{"GET", "example.com", "http://evil.example/path", 308, "https://example.com/path"},
		{"POST", "example.com", "http://example.com/api/request", 426, ""},
	} {
		r := httptest.NewRequest(tc.method, tc.target, nil)
		r.Host = tc.host
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status || w.Header().Get("Location") != tc.location {
			t.Errorf("%+v: status=%d location=%q", tc, w.Code, w.Header().Get("Location"))
		}
	}
	for _, raw := range []string{"http://example.com", "https://example.com:8443", "https://user@example.com", "https://example.com/path", "https://example.com/?x=y", "https://127.0.0.1", "https://*.example.com"} {
		if _, err := parseHTTPSOptions(raw, "", "", ""); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	for _, names := range []string{"*.example.com", "example.com:443", "evil.com/", "example.com,"} {
		if _, err := parseHTTPSOptions("https://example.com", names, "", ""); err == nil {
			t.Errorf("accepted hosts %q", names)
		}
	}
	if _, err := parseHTTPSOptions("https://example.com", "", "http://other.test/directory", ""); err == nil {
		t.Fatal("accepted arbitrary ACME endpoint")
	}
}

func TestHTTPSRuntimeUsesPersistentCertAndRejectsUnknownSNI(t *testing.T) {
	t.Setenv("HTTPS_ENABLED", "true")
	t.Setenv("PUBLIC_URL", "https://example.com")
	t.Setenv("ACME_HOSTS", "www.example.com")
	t.Setenv("ACME_CACHE_DIR", t.TempDir())
	t.Setenv("GCS_TLS_BUCKET", "")
	t.Setenv("GCP_PROJECT_ID", "")
	opts, err := parseHTTPSOptions("https://example.com", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Build through production construction once, then persist a fixture cert in
	// the actual local cache namespace. Reconstructing simulates process restart.
	runtime, err := newRuntimeEndpoints(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "secure app") }))
	if err != nil {
		t.Fatal(err)
	}
	cert, key := testCertificate(t, "example.com")
	cache := localCache(t, opts)
	if err = cache.Put(context.Background(), "example.com", append(key, cert...)); err != nil {
		t.Fatal(err)
	}
	runtime, err = newRuntimeEndpoints(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "secure app") }))
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.endpoints) != 2 || runtime.endpoints[0].tls || !runtime.endpoints[1].tls {
		t.Fatal("expected HTTP and HTTPS listeners")
	}
	// Do not call runtime.warmup: it would try to issue the intentionally missing
	// www certificate. This fixture never calls a real ACME or cloud provider.
	srv := httptest.NewUnstartedServer(runtime.endpoints[1].server.Handler)
	srv.TLS = runtime.endpoints[1].server.TLSConfig
	srv.StartTLS()
	defer srv.Close()
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(cert)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "example.com"}}, Timeout: 3 * time.Second}
	req, _ := http.NewRequest("GET", srv.URL+"/", nil)
	req.Host = "example.com"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(data) != "secure app" {
		t.Fatalf("HTTPS response %d %q", resp.StatusCode, data)
	}
	client.Transport.(*http.Transport).CloseIdleConnections()
	client.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "evil.example"}}
	if _, err := client.Do(req); err == nil {
		t.Fatal("unknown SNI accepted")
	}
	if err := cache.Put(context.Background(), "fixture+http-01", []byte("public-challenge-token")); err != nil {
		t.Fatal(err)
	}
	challenge := httptest.NewRecorder()
	challengeRequest := httptest.NewRequest("GET", "http://example.com:80/.well-known/acme-challenge/fixture", nil)
	runtime.endpoints[0].server.Handler.ServeHTTP(challenge, challengeRequest)
	if challenge.Code != http.StatusOK || challenge.Body.String() != "public-challenge-token" {
		t.Fatalf("HTTP-01: %d %q", challenge.Code, challenge.Body.String())
	}
	// Plain HTTP is challenge/redirect-only, including when a caller attempts to
	// reach the audit API directly.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "http://example.com/api/request", nil)
	runtime.endpoints[0].server.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusUpgradeRequired {
		t.Fatalf("plaintext API returned %d", w.Code)
	}
}

func localCache(t *testing.T, opts httpsOptions) autocert.DirCache {
	t.Helper()
	// The runtime's directory layout intentionally namespaces ACME production
	// and staging accounts independently.
	raw := sha256.Sum256([]byte(opts.directory))
	sum := hex.EncodeToString(raw[:])
	return autocert.DirCache(fmt.Sprintf("%s/%s", env("ACME_CACHE_DIR", ""), sum))
}
func testCertificate(t *testing.T, host string) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: host}, DNSNames: []string{host}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(365 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw})
}

func TestPrivateGCSCachePersistenceAndErrors(t *testing.T) {
	var mu sync.Mutex
	objects := map[string][]byte{}
	deny := false
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if deny {
			w.WriteHeader(403)
			io.WriteString(w, `{"error":{"code":403,"message":"denied"}}`)
			return
		}
		if r.Method == "POST" {
			_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			reader := multipart.NewReader(r.Body, params["boundary"])
			part, err := reader.NextPart()
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			var meta struct {
				Name string `json:"name"`
			}
			if err = json.NewDecoder(part).Decode(&meta); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			part, err = reader.NextPart()
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			data, _ := io.ReadAll(part)
			objects[meta.Name] = data
			w.Header().Set("Content-Type", "application/json")
			var sum [4]byte
			binary.BigEndian.PutUint32(sum[:], crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli)))
			fmt.Fprintf(w, `{"name":%q,"generation":"1","size":%q,"crc32c":%q}`, meta.Name, fmt.Sprint(len(data)), base64.StdEncoding.EncodeToString(sum[:]))
			return
		}
		segments := strings.SplitN(r.URL.Path, "/o/", 2)
		if len(segments) != 2 {
			t.Errorf("unexpected cache request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(400)
			return
		}
		name := segments[1]
		if r.Method == "DELETE" {
			delete(objects, name)
			w.WriteHeader(204)
			return
		}
		data, ok := objects[name]
		if !ok {
			w.WriteHeader(404)
			io.WriteString(w, `{"error":{"code":404}}`)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.Write(data)
	}))
	defer fake.Close()
	client, err := storage.NewClient(context.Background(), option.WithEndpoint(fake.URL), option.WithHTTPClient(fake.Client()), option.WithoutAuthentication(), storage.WithJSONReads())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	first := &gcsACMECache{bucket: client.Bucket("private-tls"), prefix: "acme/v1/test/"}
	ctx := context.Background()
	if _, err = first.Get(ctx, "missing"); !errors.Is(err, autocert.ErrCacheMiss) {
		t.Fatalf("missing error %v", err)
	}
	if err = verifyACMECache(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err = first.Put(ctx, "example.com", []byte("fixture-only-key-and-certificate")); err != nil {
		t.Fatal(err)
	}
	second := &gcsACMECache{bucket: client.Bucket("private-tls"), prefix: first.prefix}
	got, err := second.Get(ctx, "example.com")
	if err != nil || string(got) != "fixture-only-key-and-certificate" {
		t.Fatalf("persistent read %q %v", got, err)
	}
	if err = first.Put(ctx, "too-big", make([]byte, maxACMECacheBytes+1)); err == nil {
		t.Fatal("oversized entry accepted")
	}
	mu.Lock()
	deny = true
	mu.Unlock()
	if _, err = second.Get(ctx, "example.com"); err == nil || errors.Is(err, autocert.ErrCacheMiss) {
		t.Fatalf("permission error became cache miss: %v", err)
	}
	mu.Lock()
	deny = false
	mu.Unlock()
	if err = second.Delete(ctx, "example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err = first.Get(ctx, "example.com"); !errors.Is(err, autocert.ErrCacheMiss) {
		t.Fatalf("deleted read %v", err)
	}
}
