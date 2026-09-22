package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
)

type trafficRuntime struct {
	live           *liveTrafficProjection
	proxyTransport *http.Transport
	proxy          *http.Server
	pac            *http.Server
	listeners      []net.Listener
}

func newTrafficRuntime(transport *http.Transport) (*trafficRuntime, error) {
	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("proxy listener unavailable: %w", err)
	}
	pacListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = proxyListener.Close()
		return nil, fmt.Errorf("PAC listener unavailable: %w", err)
	}
	live := newLiveTrafficProjection()
	return &trafficRuntime{
		live: live, proxyTransport: transport,
		proxy:     &http.Server{Handler: http.HandlerFunc(live.serveProxy)},
		pac:       &http.Server{Handler: http.HandlerFunc(live.servePAC)},
		listeners: []net.Listener{proxyListener, pacListener},
	}, nil
}

func defaultProxyTransport() *http.Transport {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport.Clone()
	}
	return &http.Transport{}
}

func (r *trafficRuntime) Serve(ctx context.Context) error { return r.ServeReady(ctx, nil) }

func (r *trafficRuntime) ServeReady(ctx context.Context, ready chan<- struct{}) error {
	errs := make(chan error, 2)
	go func() { errs <- r.proxy.Serve(r.listeners[0]) }()
	go func() { errs <- r.pac.Serve(r.listeners[1]) }()
	for _, address := range []string{r.listeners[0].Addr().String(), r.listeners[1].Addr().String()} {
		if err := proveHTTPServing(ctx, address); err != nil {
			_ = r.Close()
			return fmt.Errorf("runtime readiness failed for %s: %w", address, err)
		}
	}
	if ready != nil {
		close(ready)
	}
	select {
	case <-ctx.Done():
		return r.Close()
	case serverErr := <-errs:
		_ = r.Close()
		if serverErr == http.ErrServerClosed {
			return nil
		}
		return serverErr
	}
}

func proveHTTPServing(ctx context.Context, address string) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	defer conn.Close()
	stopCancelClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancelClose()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if _, err := fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", address); err != nil {
		return err
	}
	var first [1]byte
	_, err = conn.Read(first[:])
	return err
}

func (r *trafficRuntime) Close() error { return r.CloseTraffic() }

func (r *trafficRuntime) CloseTraffic() error {
	err := errors.Join(r.proxy.Close(), r.pac.Close())
	for _, listener := range r.listeners {
		_ = listener.Close()
	}
	r.proxyTransport.CloseIdleConnections()
	return err
}

func (r *trafficRuntime) PACListen() string { return r.listeners[1].Addr().String() }

func (r *trafficRuntime) interceptionActive() bool {
	served := r.live.current.Load()
	return served != nil && (served.httpsCORSRoutes || served.httpsFacadeRoutes)
}
