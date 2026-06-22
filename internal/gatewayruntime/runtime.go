package gatewayruntime

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"seamless-cors/internal/config"
	"seamless-cors/internal/control"
	"seamless-cors/internal/corsproxy"
	"seamless-cors/internal/liveconfig"
	"seamless-cors/internal/pacrouting"
	"seamless-cors/internal/platform"
	"seamless-cors/internal/userca"
)

type Runtime struct {
	mu         sync.RWMutex
	live       liveconfig.Config
	authority  *userca.Authority
	proxy      *http.Server
	pacHandler *pacrouting.DynamicHandler
	pac        *http.Server
	router     *control.Server
	listeners  []net.Listener
	token      string
	liveConfig *liveconfig.Source
}

type serverError struct {
	source string
	err    error
}

func New(source *liveconfig.Source, live liveconfig.Config, token string) (*Runtime, error) {
	return NewWithStop(source, live, token, nil)
}

func NewWithStop(source *liveconfig.Source, live liveconfig.Config, token string, onStop func() error) (*Runtime, error) {
	cfg := live.Effective()
	entries := live.Entries()
	if err := config.Validate(cfg); err != nil {
		return nil, err
	}
	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("proxy listener unavailable: %w", err)
	}
	pacListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		proxyListener.Close()
		return nil, fmt.Errorf("PAC listener unavailable: %w", err)
	}
	routerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		proxyListener.Close()
		pacListener.Close()
		return nil, fmt.Errorf("router listener unavailable: %w", err)
	}

	proxyListen := proxyListener.Addr().String()
	pacListen := pacListener.Addr().String()
	routerListen := routerListener.Addr().String()

	pacBody := pacrouting.Generate(pacrouting.Options{ProxyListen: proxyListen, CATrusted: live.CATrusted(), Entries: entries})
	pacHandler := pacrouting.NewDynamicHandler(pacBody)
	routerServer := control.NewWithStop(control.State{
		RuntimeActive:    true,
		ProxyListen:      proxyListen,
		PACListen:        pacListen,
		ControlListen:    routerListen,
		DomainList:       live.DomainListPath(),
		CATrusted:        live.CATrusted(),
		DomainCount:      len(entries),
		PendingLifecycle: live.PendingLifecycle(),
	}, token, onStop)
	return &Runtime{
		live:       live,
		liveConfig: source,
		pacHandler: pacHandler,
		proxy:      &http.Server{},
		pac:        &http.Server{Handler: pacHandler},
		router:     routerServer,
		listeners:  []net.Listener{proxyListener, pacListener, routerListener},
		token:      token,
	}, nil
}

func (r *Runtime) SetAuthority(authority *userca.Authority) error {
	r.authority = authority
	proxyHandler, err := corsproxy.New(corsproxy.Options{
		CATrusted: r.live.CATrusted(),
		Authority: authority,
	})
	if err != nil {
		return err
	}
	r.proxy.Handler = proxyHandler
	return nil
}

func (r *Runtime) Serve(ctx context.Context) error {
	if r.proxy.Handler == nil {
		if err := r.SetAuthority(nil); err != nil {
			return err
		}
	}
	errs := make(chan serverError, 4)
	go r.watchLiveConfig(ctx, errs)
	go func() { errs <- serverError{source: "router", err: r.router.Serve(r.listeners[2])} }()
	go func() { errs <- serverError{source: "proxy", err: r.proxy.Serve(r.listeners[0])} }()
	go func() { errs <- serverError{source: "pac", err: r.pac.Serve(r.listeners[1])} }()

	select {
	case <-ctx.Done():
		return r.Close()
	case serverErr := <-errs:
		if serverErr.err == http.ErrServerClosed && serverErr.source != "router" {
			return r.waitForRouterOrFatal(ctx, errs)
		}
		_ = r.Close()
		if serverErr.err == http.ErrServerClosed {
			return nil
		}
		return serverErr.err
	}
}

func (r *Runtime) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	_ = r.CloseTraffic()
	return r.router.Close(ctx)
}

func (r *Runtime) CloseTraffic() error {
	_ = r.proxy.Close()
	return r.pac.Close()
}

func (r *Runtime) Token() string {
	return r.token
}

func (r *Runtime) RouterListen() string {
	return r.listeners[2].Addr().String()
}

func (r *Runtime) PACURL() string {
	return "http://" + r.listeners[1].Addr().String() + "/" + platform.PACFootprintFileName
}

func (r *Runtime) PACListen() string {
	return r.listeners[1].Addr().String()
}

func (r *Runtime) State() control.State {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stateLocked()
}

func (r *Runtime) waitForRouterOrFatal(ctx context.Context, errs <-chan serverError) error {
	for {
		select {
		case <-ctx.Done():
			return r.Close()
		case serverErr := <-errs:
			if serverErr.err == http.ErrServerClosed {
				if serverErr.source == "router" {
					return nil
				}
				continue
			}
			_ = r.Close()
			return serverErr.err
		}
	}
}

func (r *Runtime) watchLiveConfig(ctx context.Context, errs chan<- serverError) {
	events := r.liveConfig.Watch(ctx, 100*time.Millisecond)
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if event.Err != nil {
				select {
				case errs <- serverError{source: "live-config", err: event.Err}:
				case <-ctx.Done():
				}
				return
			}
			r.applyLiveConfig(event.Config)
		}
	}
}

func (r *Runtime) applyLiveConfig(live liveconfig.Config) {
	r.mu.Lock()
	r.live = live
	state := r.stateLocked()
	r.mu.Unlock()
	r.pacHandler.Set(pacrouting.Generate(pacrouting.Options{
		ProxyListen: r.listeners[0].Addr().String(),
		CATrusted:   live.CATrusted(),
		Entries:     live.Entries(),
	}))
	r.router.SetState(state)
}

func (r *Runtime) stateLocked() control.State {
	entries := r.live.Entries()
	return control.State{
		RuntimeActive:    true,
		ProxyListen:      r.listeners[0].Addr().String(),
		PACListen:        r.listeners[1].Addr().String(),
		ControlListen:    r.listeners[2].Addr().String(),
		DomainList:       r.live.DomainListPath(),
		CATrusted:        r.live.CATrusted(),
		DomainCount:      len(entries),
		PendingLifecycle: r.live.PendingLifecycle(),
	}
}
