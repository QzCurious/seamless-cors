package proxy

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"

	"cors-vpn/internal/cors"
	"cors-vpn/internal/domain"
)

type Options struct {
	Entries   []domain.Entry
	CATrusted bool
	Logger    io.Writer
	Transport http.RoundTripper
}

type Core struct {
	entries   []domain.Entry
	caTrusted bool
	logger    *log.Logger
	transport http.RoundTripper
}

func New(opts Options) *Core {
	transport := opts.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	var logger *log.Logger
	if opts.Logger != nil {
		logger = log.New(opts.Logger, "", 0)
	}
	return &Core{entries: opts.Entries, caTrusted: opts.CATrusted, logger: logger, transport: transport}
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
	if !c.matches(target) {
		cors.WriteGatewayError(w, req, http.StatusForbidden, "unmatched", fmt.Errorf("host is not in Domain List"))
		return
	}
	if c.logger != nil {
		c.logger.Printf("matched %s %s", req.Method, target.String())
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
	if c.caTrusted && c.matchesHostPort("https", req.Host) {
		cors.WriteGatewayError(w, req, http.StatusNotImplemented, "https-interception", fmt.Errorf("trusted HTTPS interception is not available in this build"))
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

func (c *Core) matches(target *url.URL) bool {
	host := target.Hostname()
	port := target.Port()
	if port == "" {
		port = defaultPort(target.Scheme)
	}
	for _, entry := range c.entries {
		if entry.Matches(target.Scheme, host, port) {
			return true
		}
	}
	return false
}

func (c *Core) matchesHostPort(scheme, hostPort string) bool {
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		host = hostPort
		port = defaultPort(scheme)
	}
	for _, entry := range c.entries {
		if entry.Matches(scheme, host, port) {
			return true
		}
	}
	return false
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

func defaultPort(scheme string) string {
	switch strings.ToLower(scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func tunnel(dst io.WriteCloser, src io.ReadCloser) {
	defer dst.Close()
	defer src.Close()
	_, _ = io.Copy(dst, src)
}
