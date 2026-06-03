package proxy

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cors-vpn/internal/ca"
	"cors-vpn/internal/domain"
	"cors-vpn/internal/platform"
)

func TestHTTPProxyForwardsMatchedRequestsAndRepairsAllStatuses(t *testing.T) {
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

	entry, err := domain.ParseEntry(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	core := New(Options{Entries: []domain.Entry{entry}})
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

func TestProxyAnswersMatchedPreflightLocally(t *testing.T) {
	entry, err := domain.ParseEntry("api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	core := New(Options{Entries: []domain.Entry{entry}})
	req := httptest.NewRequest(http.MethodOptions, "http://api.example.test/v1/items", nil)
	req.Header.Set("Origin", "null")
	req.Header.Set("Access-Control-Request-Method", "PATCH")
	req.Header.Set("Access-Control-Request-Headers", "X-Dev")
	req.Header.Set("Access-Control-Request-Private-Network", "true")
	rec := httptest.NewRecorder()

	core.ServeHTTP(rec, req)

	resp := rec.Result()
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
}

func TestProxyGatewayErrorsAreJSONAndCORSReadable(t *testing.T) {
	entry, _ := domain.ParseEntry("api.example.test")
	core := New(Options{Entries: []domain.Entry{entry}})
	req := httptest.NewRequest(http.MethodGet, "http://other.example.test/v1", nil)
	req.Header.Set("Origin", "https://app.local")
	rec := httptest.NewRecorder()

	core.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "https://app.local" {
		t.Fatalf("missing CORS-readable gateway error")
	}
	if !strings.Contains(string(body), "Transparent CORS Gateway") {
		t.Fatalf("body = %s", body)
	}
}

func TestTrustedHTTPSInterceptionRepairsResponsesInsideConnectTunnel(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.Header.Get("Origin"); got != "https://app.local" {
			t.Fatalf("Origin was rewritten: %q", got)
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Upstream", "ok")
		_, _ = w.Write([]byte("secure upstream body"))
	}))
	defer upstream.Close()

	authority, err := ca.Create(t.TempDir(), platform.NoopAdapter{})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := domain.ParseEntry(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	core := New(Options{
		Entries:   []domain.Entry{entry},
		CATrusted: true,
		Authority: authority,
		Transport: upstream.Client().Transport,
	})
	proxyServer := httptest.NewServer(core)
	defer proxyServer.Close()

	proxyAddress := strings.TrimPrefix(proxyServer.URL, "http://")
	rawConn, err := net.Dial("tcp", proxyAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer rawConn.Close()
	upstreamAddress := strings.TrimPrefix(upstream.URL, "https://")
	if _, err := rawConn.Write([]byte("CONNECT " + upstreamAddress + " HTTP/1.1\r\nHost: " + upstreamAddress + "\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	connectResp, err := http.ReadResponse(bufio.NewReader(rawConn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	if connectResp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %d", connectResp.StatusCode)
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(authority.CertPEM) {
		t.Fatal("failed to trust gateway CA")
	}
	tlsConn := tls.Client(rawConn, &tls.Config{
		ServerName: strings.Split(upstreamAddress, ":")[0],
		RootCAs:    roots,
	})
	if err := tlsConn.Handshake(); err != nil {
		t.Fatal(err)
	}
	defer tlsConn.Close()

	req, err := http.NewRequest(http.MethodGet, "https://"+upstreamAddress+"/secure", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://app.local")
	req.Host = upstreamAddress
	if err := req.Write(tlsConn); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(tlsConn), req)
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
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "secure upstream body" {
		t.Fatalf("body = %q", string(body))
	}
}
