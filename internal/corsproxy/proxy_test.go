package corsproxy

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"seamless-cors/internal/ca"
	"seamless-cors/internal/platform"
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

	core := New(Options{})
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
	core := New(Options{})
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
	core := New(Options{})
	req := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Path: "/v1"},
		Header: http.Header{},
	}
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
	if !strings.Contains(string(body), "seamless-cors") {
		t.Fatalf("body = %s", body)
	}
}

func TestHTTPProxyRepairsAnyTrafficThatReachesIt(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = w.Write([]byte("direct proxy body"))
	}))
	defer upstream.Close()

	core := New(Options{})
	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/manual", nil)
	req.Header.Set("Origin", "https://manual.local")
	rec := httptest.NewRecorder()

	core.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://manual.local" {
		t.Fatalf("allow origin = %q", got)
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
	core := New(Options{
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

func TestTrustedHTTPSInterceptionAnswersPreflightLocally(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer upstream.Close()

	authority, err := ca.Create(t.TempDir(), platform.NoopAdapter{})
	if err != nil {
		t.Fatal(err)
	}
	core := New(Options{
		CATrusted: true,
		Authority: authority,
		Transport: upstream.Client().Transport,
	})
	proxyServer := httptest.NewServer(core)
	defer proxyServer.Close()

	tlsConn, reqHost := openTrustedTunnel(t, proxyServer.URL, upstream.URL, authority)
	defer tlsConn.Close()

	req, err := http.NewRequest(http.MethodOptions, "https://"+reqHost+"/secure", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://app.local")
	req.Header.Set("Access-Control-Request-Method", "PATCH")
	req.Host = reqHost
	if err := req.Write(tlsConn); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(tlsConn), req)
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

func openTrustedTunnel(t *testing.T, proxyURL, upstreamURL string, authority *ca.EphemeralAuthority) (*tls.Conn, string) {
	t.Helper()
	proxyAddress := strings.TrimPrefix(proxyURL, "http://")
	rawConn, err := net.Dial("tcp", proxyAddress)
	if err != nil {
		t.Fatal(err)
	}
	upstreamAddress := strings.TrimPrefix(upstreamURL, "https://")
	if _, err := rawConn.Write([]byte("CONNECT " + upstreamAddress + " HTTP/1.1\r\nHost: " + upstreamAddress + "\r\n\r\n")); err != nil {
		_ = rawConn.Close()
		t.Fatal(err)
	}
	connectResp, err := http.ReadResponse(bufio.NewReader(rawConn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		_ = rawConn.Close()
		t.Fatal(err)
	}
	if connectResp.StatusCode != http.StatusOK {
		_ = rawConn.Close()
		t.Fatalf("CONNECT status = %d", connectResp.StatusCode)
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(authority.CertPEM) {
		_ = rawConn.Close()
		t.Fatal("failed to trust gateway CA")
	}
	tlsConn := tls.Client(rawConn, &tls.Config{
		ServerName: strings.Split(upstreamAddress, ":")[0],
		RootCAs:    roots,
	})
	if err := tlsConn.Handshake(); err != nil {
		_ = tlsConn.Close()
		t.Fatal(err)
	}
	return tlsConn, upstreamAddress
}
