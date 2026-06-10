package corsproxy

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/elazarl/goproxy"

	"seamless-cors/internal/ca"
)

type Options struct {
	CATrusted bool
	Authority *ca.EphemeralAuthority
	Transport *http.Transport
}

type Core struct {
	proxy *goproxy.ProxyHttpServer
}

func New(opts Options) (*Core, error) {
	proxy := goproxy.NewProxyHttpServer()
	configureProxyLogging(proxy)
	proxy.Tr = opts.Transport
	if proxy.Tr == nil {
		proxy.Tr = defaultTransport()
	}
	proxy.CertStore = newMemoryCertStore()

	if opts.CATrusted {
		if opts.Authority == nil {
			return nil, fmt.Errorf("trusted HTTPS interception requires an ephemeral CA")
		}
		cert, err := opts.Authority.TLSCertificate()
		if err != nil {
			return nil, err
		}
		proxy.OnRequest().HandleConnectFunc(func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
			return &goproxy.ConnectAction{
				Action:    goproxy.ConnectMitm,
				TLSConfig: goproxy.TLSConfigFromCA(&cert),
			}, host
		})
	}

	proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if !isPreflight(req) {
			return req, nil
		}
		ctx.UserData = localPreflight{}
		return nil, preflightResponse(req)
	})
	proxy.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		if resp == nil {
			return nil
		}
		if _, ok := ctx.UserData.(localPreflight); ok {
			return resp
		}
		if ctx.Req != nil {
			repairResponseHeaders(resp.Header, ctx.Req.Header.Get("Origin"))
		}
		return resp
	})

	return &Core{proxy: proxy}, nil
}

func configureProxyLogging(proxy *goproxy.ProxyHttpServer) {
	if proxyDebugEnabled() {
		proxy.Verbose = true
		return
	}
	proxy.Verbose = false
	proxy.Logger = log.New(io.Discard, "", 0)
}

func proxyDebugEnabled() bool {
	switch strings.ToLower(os.Getenv("SEAMLESS_CORS_DEBUG_PROXY")) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func (c *Core) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	c.proxy.ServeHTTP(w, req)
}

func defaultTransport() *http.Transport {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport.Clone()
	}
	return &http.Transport{}
}

type localPreflight struct{}

type memoryCertStore struct {
	mu    sync.Mutex
	certs map[string]*tls.Certificate
}

func newMemoryCertStore() *memoryCertStore {
	return &memoryCertStore{certs: map[string]*tls.Certificate{}}
}

func (s *memoryCertStore) Fetch(hostname string, gen func() (*tls.Certificate, error)) (*tls.Certificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cert, ok := s.certs[hostname]; ok {
		return cert, nil
	}
	cert, err := gen()
	if err != nil {
		return nil, err
	}
	s.certs[hostname] = cert
	return cert, nil
}
