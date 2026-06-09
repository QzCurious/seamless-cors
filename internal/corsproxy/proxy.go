package corsproxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"

	"seamless-cors/internal/ca"
	"seamless-cors/internal/cors"
)

type Options struct {
	CATrusted bool
	Authority *ca.EphemeralAuthority
	Transport http.RoundTripper
}

type Core struct {
	caTrusted bool
	authority *ca.EphemeralAuthority
	transport http.RoundTripper
}

func New(opts Options) *Core {
	transport := opts.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &Core{caTrusted: opts.CATrusted, authority: opts.Authority, transport: transport}
}

func (c *Core) SetAuthority(authority *ca.EphemeralAuthority) {
	c.authority = authority
}

func (c *Core) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodConnect {
		c.handleConnect(w, req)
		return
	}
	target := absoluteURL(req)
	if target == nil {
		cors.WriteGatewayError(w, req, http.StatusBadGateway, "upstream", fmt.Errorf("request target is not an absolute URL"))
		return
	}
	if cors.IsPreflight(req) {
		cors.WritePreflight(w, req)
		return
	}
	c.forward(w, req, target)
}

func (c *Core) forward(w http.ResponseWriter, req *http.Request, target *url.URL) {
	out := req.Clone(req.Context())
	out.URL = target
	out.RequestURI = ""
	out.Host = target.Host
	out.Header = req.Header.Clone()

	resp, err := c.transport.RoundTrip(out)
	if err != nil {
		status := http.StatusBadGateway
		if req.Context().Err() == context.Canceled {
			return
		}
		cors.WriteGatewayError(w, req, status, "upstream", err)
		return
	}
	defer resp.Body.Close()

	for name, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	cors.RepairResponseHeaders(w.Header(), req.Header.Get("Origin"))
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (c *Core) handleConnect(w http.ResponseWriter, req *http.Request) {
	if c.caTrusted {
		c.handleTrustedHTTPS(w, req)
		return
	}
	dst, err := net.Dial("tcp", req.Host)
	if err != nil {
		cors.WriteGatewayError(w, req, http.StatusBadGateway, "upstream", err)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		_ = dst.Close()
		cors.WriteGatewayError(w, req, http.StatusInternalServerError, "gateway", fmt.Errorf("response writer cannot tunnel"))
		return
	}
	client, _, err := hj.Hijack()
	if err != nil {
		_ = dst.Close()
		return
	}
	_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	go tunnel(dst, client)
	go tunnel(client, dst)
}

func (c *Core) handleTrustedHTTPS(w http.ResponseWriter, req *http.Request) {
	if c.authority == nil {
		cors.WriteGatewayError(w, req, http.StatusInternalServerError, "https-interception", fmt.Errorf("ephemeral CA is not available"))
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		cors.WriteGatewayError(w, req, http.StatusInternalServerError, "gateway", fmt.Errorf("response writer cannot intercept HTTPS"))
		return
	}
	client, _, err := hj.Hijack()
	if err != nil {
		return
	}
	host, _, err := net.SplitHostPort(req.Host)
	if err != nil {
		host = req.Host
	}
	leaf, err := c.authority.LeafCertificate(host)
	if err != nil {
		_ = client.Close()
		return
	}
	if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		_ = client.Close()
		return
	}
	tlsClient := tls.Server(client, &tls.Config{Certificates: []tls.Certificate{leaf}})
	if err := tlsClient.Handshake(); err != nil {
		_ = tlsClient.Close()
		return
	}
	go c.serveInterceptedHTTPS(tlsClient, req.Host)
}

func (c *Core) serveInterceptedHTTPS(conn net.Conn, upstreamHost string) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		req, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		target := *req.URL
		target.Scheme = "https"
		target.Host = upstreamHost
		req.URL = &target
		req.Host = upstreamHost
		req.RequestURI = ""

		if cors.IsPreflight(req) {
			cors.WritePreflight(rawResponseWriter{conn: conn, headers: http.Header{}}, req)
			if !shouldKeepAlive(req, nil) {
				return
			}
			continue
		}

		resp, err := c.transport.RoundTrip(req)
		if err != nil {
			cors.WriteGatewayError(rawResponseWriter{conn: conn, headers: http.Header{}}, req, http.StatusBadGateway, "upstream", err)
			return
		}
		cors.RepairResponseHeaders(resp.Header, req.Header.Get("Origin"))
		_ = resp.Write(conn)
		_ = resp.Body.Close()
		if !shouldKeepAlive(req, resp) {
			return
		}
	}
}

type rawResponseWriter struct {
	conn    net.Conn
	headers http.Header
	status  int
}

func (w rawResponseWriter) Header() http.Header {
	return w.headers
}

func (w rawResponseWriter) WriteHeader(status int) {
	w.status = status
	statusText := http.StatusText(status)
	if statusText == "" {
		statusText = "status"
	}
	_, _ = fmt.Fprintf(w.conn, "HTTP/1.1 %d %s\r\n", status, statusText)
	for name, values := range w.headers {
		for _, value := range values {
			_, _ = fmt.Fprintf(w.conn, "%s: %s\r\n", name, value)
		}
	}
	_, _ = io.WriteString(w.conn, "\r\n")
}

func (w rawResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.conn.Write(body)
}

func absoluteURL(req *http.Request) *url.URL {
	if req.URL == nil {
		return nil
	}
	if req.URL.IsAbs() {
		return req.URL
	}
	if req.Host == "" {
		return nil
	}
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	copy := *req.URL
	copy.Scheme = scheme
	copy.Host = req.Host
	return &copy
}

func shouldKeepAlive(req *http.Request, resp *http.Response) bool {
	if resp == nil {
		return !req.Close
	}
	return !req.Close && !resp.Close
}

func tunnel(dst io.WriteCloser, src io.ReadCloser) {
	defer dst.Close()
	defer src.Close()
	_, _ = io.Copy(dst, src)
}
