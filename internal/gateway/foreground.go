package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/QzCurious/seamless-cors/internal/systempac"
)

type owner struct {
	coord     *coordinator
	cache     stateCache
	listener  net.Listener
	lifecycle *lifecycle
	router    *routerServer
}

func newOwner(pac systempac.Module, ca userCAModule, coord *coordinator) (*owner, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("router listener unavailable: %w", err)
	}
	cache := stateCache{HTTPRouterListen: listener.Addr().String(), Token: token}
	lifecycle := newLifecycle(pac, ca, coord)
	lifecycle.ownerCache = cache
	return &owner{
		coord:     coord,
		cache:     cache,
		listener:  listener,
		lifecycle: lifecycle,
		router:    newRouter(token, lifecycle),
	}, nil
}

// Run initializes traffic, publishes control discovery, and waits for shutdown.
// The caller holds the instance lock for this entire lifetime.
func (o *owner) Run(ctx context.Context, directoryPath string, create bool, started func(StartResult)) (result StartResult, err error) {
	defer o.listener.Close()
	defer func() {
		// Cleanup keeps traffic serving until owned system PAC settings are removed.
		cleanup, stopErr := o.lifecycle.Stop(context.Background())
		closeErr := o.router.Close(context.Background())
		if cleanup.CleanupFulfillment == CommandUnfulfilled {
			stopErr = errors.Join(stopErr, ownerStopCleanupError{failures: cleanup.CleanupFailures})
		}
		err = errors.Join(err, stopErr, closeErr)
	}()

	result, err = o.lifecycle.activate(ctx, directoryPath, create)
	if err != nil || result.Kind() != StartResultStarted {
		return result, err
	}
	if ctx.Err() != nil {
		return StartCancelled{}, nil
	}

	// Only an initialized Gateway accepts commands from another CLI process.
	routerErrors := make(chan error, 1)
	go func() { routerErrors <- o.router.Serve(o.listener) }()
	if err := o.coord.Claim(o.cache); err != nil {
		return result, err
	}
	if started != nil {
		started(result)
	}

	select {
	case <-ctx.Done():
	case <-o.router.ShutdownRequested():
	case err = <-o.lifecycle.fatal:
	case err = <-routerErrors:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}
	return result, err
}

type ownerStopCleanupError struct{ failures []CleanupFailure }

func (ownerStopCleanupError) Error() string { return "owner stop left gateway cleanup residue" }

func randomToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
