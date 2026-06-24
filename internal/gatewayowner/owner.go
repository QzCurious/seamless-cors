package gatewayowner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"time"

	"seamless-cors/internal/gatewaycoord"
	"seamless-cors/internal/gatewayfacade"
	"seamless-cors/internal/gatewayrouter"
	"seamless-cors/internal/platform"
)

type Owner struct {
	coord    *gatewaycoord.Coordinator
	cache    gatewaycoord.GatewayStateCache
	listener net.Listener
	facade   *gatewayfacade.Facade
	router   *gatewayrouter.Server
}

func New(adapter platform.Adapter) (*Owner, error) {
	coord, err := gatewaycoord.Default()
	if err != nil {
		return nil, err
	}
	return NewWithCoord(adapter, coord)
}

func NewWithCoord(adapter platform.Adapter, coord *gatewaycoord.Coordinator) (*Owner, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("router listener unavailable: %w", err)
	}
	routerListen := listener.Addr().String()
	facade, err := gatewayfacade.New(adapter, coord, routerListen)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	cache := gatewaycoord.GatewayStateCache{HTTPRouterListen: routerListen, Token: token}
	facade.SetOwnerCache(cache)
	router := gatewayrouter.New(token, facade)
	return &Owner{
		coord:    coord,
		cache:    cache,
		listener: listener,
		facade:   facade,
		router:   router,
	}, nil
}

func (o *Owner) Facade() *gatewayfacade.Facade {
	return o.facade
}

func (o *Owner) Run(ctx context.Context, afterPublish func() error) error {
	errs := make(chan error, 1)
	go func() { errs <- o.router.Serve(o.listener) }()
	if err := o.coord.Claim(o.cache); err != nil {
		_ = o.router.Close(context.Background())
		return err
	}
	defer o.coord.RemoveOwned(o.cache)
	leaseLost := watchLease(ctx, o.coord, o.cache)
	if afterPublish != nil {
		if err := afterPublish(); err != nil {
			_ = o.closeOwnerOnly(context.Background())
			return err
		}
	}
	select {
	case <-ctx.Done():
		_, _ = o.facade.Stop()
		_ = o.router.Close(context.Background())
		return nil
	case <-leaseLost:
		_, _ = o.facade.Stop()
		_ = o.router.Close(context.Background())
		return nil
	case <-o.router.ShutdownRequested():
		_ = o.router.Close(context.Background())
		return nil
	case err := <-o.facade.FatalRuntimeErrors():
		_, _ = o.facade.Stop()
		_ = o.router.Close(context.Background())
		return err
	case err := <-errs:
		if err == nil || err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (o *Owner) Shutdown(ctx context.Context) error {
	_, _ = o.facade.Stop()
	return o.closeOwnerOnly(ctx)
}

func (o *Owner) closeOwnerOnly(ctx context.Context) error {
	_ = o.coord.RemoveOwned(o.cache)
	return o.router.Close(ctx)
}

func watchLease(ctx context.Context, coord *gatewaycoord.Coordinator, cache gatewaycoord.GatewayStateCache) <-chan struct{} {
	lost := make(chan struct{})
	go func() {
		defer close(lost)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !coord.Owns(cache) {
					return
				}
			}
		}
	}()
	return lost
}

func randomToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
