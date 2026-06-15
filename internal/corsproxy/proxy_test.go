package corsproxy

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/elazarl/goproxy"

	"seamless-cors/internal/platform"
	"seamless-cors/internal/userca"
)

func TestHTTPProxyForwardsRequestsAndRepairsAllStatuses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.Header.Get("Origin"); got != "https://app.local" {
			t.Fatalf("Origin was rewritten: %q", got)
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Trace", "abc")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("real upstream body"))
	}))
	defer upstream.Close()

	core := newTestCore(t, Options{Transport: testTransport(t, upstream.Client())})
	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/v1/items?q=dev", nil)
	req.Header.Set("Origin", "https://app.local")
	rec := httptest.NewRecorder()

	core.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://app.local" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("credentials = %q", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "real upstream body" {
		t.Fatalf("body = %q", string(body))
	}
}

func TestProxyAnswersPreflightLocally(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer upstream.Close()

	core := newTestCore(t, Options{Transport: testTransport(t, upstream.Client())})
	req := httptest.NewRequest(http.MethodOptions, upstream.URL+"/v1/items", nil)
	req.Header.Set("Origin", "null")
	req.Header.Set("Access-Control-Request-Method", "PATCH")
	req.Header.Set("Access-Control-Request-Headers", "X-Dev")
	req.Header.Set("Access-Control-Request-Private-Network", "true")
	rec := httptest.NewRecorder()

	core.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	for name, want := range map[string]string{
		"Access-Control-Allow-Origin":          "null",
		"Access-Control-Allow-Methods":         "PATCH",
		"Access-Control-Allow-Headers":         "X-Dev",
		"Access-Control-Allow-Private-Network": "true",
		"Access-Control-Max-Age":               "600",
	} {
		if got := resp.Header.Get(name); got != want {
			t.Fatalf("%s = %q", name, got)
		}
	}
	if got := upstreamHits.Load(); got != 0 {
		t.Fatalf("upstream hits = %d", got)
	}
}

func TestTrustedHTTPSInterceptionRepairsResponseAndCompletes(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.Header.Get("Origin"); got != "https://app.local" {
			t.Fatalf("Origin was rewritten: %q", got)
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Upstream", "ok")
		_, _ = w.Write([]byte("secure upstream body"))
	}))
	defer upstream.Close()

	proxyServer, proxyURL, roots := trustedProxyServer(t, upstream.Client())
	defer proxyServer.Close()

	client := &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: roots,
	}}
	req, err := http.NewRequest(http.MethodGet, upstream.URL+"/secure", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://app.local")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://app.local" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("credentials = %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "secure upstream body" {
		t.Fatalf("body = %q", string(body))
	}
	if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 {
		t.Fatal("missing intercepted TLS peer certificate")
	}
	leaf := resp.TLS.PeerCertificates[0]
	if got := leaf.NotAfter.Sub(leaf.NotBefore); got != userca.LeafValidity {
		t.Fatalf("leaf validity = %s, want %s", got, userca.LeafValidity)
	}
}

func TestTrustedHTTPSInterceptionAnswersPreflightLocally(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer upstream.Close()

	proxyServer, proxyURL, roots := trustedProxyServer(t, upstream.Client())
	defer proxyServer.Close()

	client := &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: roots,
	}}
	req, err := http.NewRequest(http.MethodOptions, upstream.URL+"/secure", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://app.local")
	req.Header.Set("Access-Control-Request-Method", "PATCH")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://app.local" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := upstreamHits.Load(); got != 0 {
		t.Fatalf("upstream hits = %d", got)
	}
}

func TestTransportFailureUsesProxyDefaultErrorShape(t *testing.T) {
	core := newTestCore(t, Options{Transport: &http.Transport{}})
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:1/unreachable", nil)
	req.Header.Set("Origin", "https://app.local")
	rec := httptest.NewRecorder()

	core.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("unexpected custom JSON content type: %q", resp.Header.Get("Content-Type"))
	}
	if strings.Contains(string(body), `"source":"seamless-cors"`) {
		t.Fatalf("unexpected custom gateway body: %s", body)
	}
}

func TestProxyLoggingIsQuietByDefault(t *testing.T) {
	t.Setenv("SEAMLESS_CORS_DEBUG_PROXY", "")
	proxy := goproxy.NewProxyHttpServer()
	proxy.Logger = log.New(failingWriter{t: t}, "", 0)

	configureProxyLogging(proxy)

	if proxy.Verbose {
		t.Fatal("proxy should not be verbose by default")
	}
	proxy.Logger.Printf("should be discarded")
}

func TestProxyLoggingDebugEnvEnablesVerboseLogs(t *testing.T) {
	for _, value := range []string{"1", "true", "yes", "TRUE", "YES"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("SEAMLESS_CORS_DEBUG_PROXY", value)
			core := newTestCore(t, Options{})

			if !core.proxy.Verbose {
				t.Fatal("proxy should be verbose with debug env")
			}
		})
	}
}

type failingWriter struct {
	t *testing.T
}

func (w failingWriter) Write([]byte) (int, error) {
	w.t.Fatal("default proxy logger should discard output")
	return 0, errors.New("unexpected write")
}

func trustedProxyServer(t *testing.T, upstreamClient *http.Client) (*httptest.Server, *url.URL, *tls.Config) {
	t.Helper()
	store := &testTrustStore{}
	authority, _, err := userca.Ensure(t.TempDir(), store)
	if err != nil {
		t.Fatal(err)
	}
	core := newTestCore(t, Options{
		CATrusted: true,
		Authority: authority,
		Transport: testTransport(t, upstreamClient),
	})
	proxyServer := httptest.NewServer(core)
	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		proxyServer.Close()
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(authority.CertPEM) {
		proxyServer.Close()
		t.Fatal("failed to trust gateway CA")
	}
	return proxyServer, proxyURL, &tls.Config{RootCAs: roots}
}

type testTrustStore struct {
	records []platform.CARecord
}

func (s *testTrustStore) TrustedCAs() ([]platform.CARecord, error) {
	return append([]platform.CARecord(nil), s.records...), nil
}

func (s *testTrustStore) TrustCA(certPEM []byte) error {
	fingerprint, err := userca.SHA1Fingerprint(certPEM)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	s.records = []platform.CARecord{{SHA1: fingerprint, CertPEM: certPEM, NotAfter: cert.NotAfter}}
	return nil
}

func (s *testTrustStore) RemoveCAs([]string) error {
	s.records = nil
	return nil
}

func newTestCore(t *testing.T, opts Options) *Core {
	t.Helper()
	core, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return core
}

func testTransport(t *testing.T, client *http.Client) *http.Transport {
	t.Helper()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client transport is %T, want *http.Transport", client.Transport)
	}
	return transport.Clone()
}
