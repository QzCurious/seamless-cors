package managedgateway

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"seamless-cors/internal/cleanup"
	"seamless-cors/internal/config"
	"seamless-cors/internal/control"
	"seamless-cors/internal/gatewaycoord"
	"seamless-cors/internal/gatewayruntime"
	"seamless-cors/internal/liveconfig"
	"seamless-cors/internal/platform"
	"seamless-cors/internal/userca"
)

var ErrLifecycleConsentDeclined = errors.New("explicit lifecycle consent declined")

func Start(stdout, _ io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return StartWithContextAndInput(ctx, os.Stdin, stdout, platform.CurrentAdapter)
}

func StartWithContext(ctx context.Context, stdout io.Writer, adapter platform.Adapter) error {
	return StartWithContextAndInput(ctx, nil, stdout, adapter)
}

func StartWithContextAndInput(ctx context.Context, stdin io.Reader, stdout io.Writer, adapter platform.Adapter) error {
	return Gateway{
		Stdin:   stdin,
		Stdout:  stdout,
		Adapter: adapter,
	}.Start(ctx)
}

func Serve(stdout, _ io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return ServeWithContext(ctx, stdout, platform.CurrentAdapter)
}

func ServeWithContext(ctx context.Context, stdout io.Writer, adapter platform.Adapter) error {
	return Gateway{Stdout: stdout, Adapter: adapter}.ServeOwner(ctx)
}

type Gateway struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Adapter platform.Adapter
}

func (g Gateway) Start(ctx context.Context) error {
	stdout := g.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	adapter := g.Adapter
	if adapter == nil {
		adapter = platform.CurrentAdapter
	}
	if err := requireSupported(adapter.Capabilities()); err != nil {
		return err
	}
	if active, err := activeOwnerState(); err != nil {
		return err
	} else if active {
		fmt.Fprintln(stdout, "gateway owner already running")
		return nil
	}
	if err := cleanRuntime(adapter); err != nil {
		return err
	}
	source, live, err := liveconfig.LoadOrBootstrap("", stdout)
	if err != nil {
		return err
	}
	pacStates, err := adapter.CurrentPACState()
	if err != nil {
		return err
	}
	consent := lifecycleConsentRequest{
		ManagedPAC:      platform.HasForeignEnabledPACState(pacStates),
		CurrentPACState: pacStates,
	}
	if err := promptForLifecycleConsent(g.Stdin, stdout, consent); err != nil {
		return err
	}

	runtime, err := newRuntimeFromLiveConfig(source, live, adapter, stdout)
	if err != nil {
		return err
	}
	return g.serveRuntime(ctx, runtime)
}

func (g Gateway) ServeOwner(ctx context.Context) error {
	stdout := g.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	adapter := g.Adapter
	if adapter == nil {
		adapter = platform.CurrentAdapter
	}
	if err := requireSupported(adapter.Capabilities()); err != nil {
		return err
	}
	coord, err := gatewaycoord.Default()
	if err != nil {
		return err
	}
	if coord.Verify().Status == gatewaycoord.Active {
		return fmt.Errorf("gateway owner already running")
	}
	token, err := randomToken()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("router listener unavailable: %w", err)
	}
	routerListen := listener.Addr().String()
	cache := gatewaycoord.GatewayStateCache{HTTPRouterListen: routerListen, Token: token}
	router := control.NewWithStop(control.State{
		RuntimeActive: false,
		ControlListen: routerListen,
	}, token, func() error {
		return coord.RemoveOwned(cache)
	})
	errs := make(chan error, 1)
	go func() { errs <- router.Serve(listener) }()

	if err := coord.Claim(cache); err != nil {
		_ = router.Close(context.Background())
		return err
	}
	fmt.Fprintln(stdout, "gateway owner running")
	defer coord.RemoveOwned(cache)
	leaseLost := watchLease(ctx, coord, cache)

	select {
	case <-ctx.Done():
		_ = router.Close(context.Background())
		return nil
	case <-leaseLost:
		_ = router.Close(context.Background())
		return nil
	case err := <-errs:
		if err == nil || err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func writeStartGuidance(stdout io.Writer, live liveconfig.Config) {
	fmt.Fprintf(stdout, "seamless-cors running\n")
	fmt.Fprintf(stdout, "config: %s\n", live.ConfigPath())
	fmt.Fprintf(stdout, "domain-list: %s\n", live.DomainListPath())
	fmt.Fprintln(stdout, "managed-pac: active")
}

func requireSupported(report platform.CapabilityReport) error {
	if report.Supported &&
		report.PACManagement == platform.CapabilitySupported &&
		report.CATrustManagement == platform.CapabilitySupported &&
		report.RuntimeCleanup == platform.CapabilitySupported {
		return nil
	}
	return fmt.Errorf("platform unsupported: run `seamless-cors check` for details")
}

type lifecycleConsentRequest struct {
	ManagedPAC      bool
	CurrentPACState []platform.PACServiceState
}

func (r lifecycleConsentRequest) needed() bool {
	return r.ManagedPAC
}

func promptForLifecycleConsent(stdin io.Reader, stdout io.Writer, req lifecycleConsentRequest) error {
	if !req.needed() {
		return nil
	}
	fmt.Fprintln(stdout, "Explicit Lifecycle Consent required before seamless-cors changes current-user OS-managed state.")
	if req.ManagedPAC {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Managed PAC Consent:")
		fmt.Fprintln(stdout, "seamless-cors will replace existing managed PAC state for this run.")
		fmt.Fprintln(stdout, "Gateway Footprint Cleanup removes seamless-cors-owned managed PAC settings without restoring previous PAC state.")
		fmt.Fprintln(stdout, "Current managed PAC state:")
		for _, state := range req.CurrentPACState {
			if state.URL == "" {
				state.URL = "(empty)"
			}
			fmt.Fprintf(stdout, "  %s: enabled=%t url=%s\n", state.Name, state.Enabled, state.URL)
		}
	}
	fmt.Fprintln(stdout)
	fmt.Fprint(stdout, "Proceed? [y/N] ")
	ok, err := readYes(stdin)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(stdout, "Startup canceled; no lifecycle changes were applied.")
		return ErrLifecycleConsentDeclined
	}
	fmt.Fprintln(stdout)
	return nil
}

func readYes(stdin io.Reader) (bool, error) {
	if stdin == nil {
		return true, nil
	}
	answer, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes", nil
}

type runtime struct {
	live       liveconfig.Config
	adapter    platform.Adapter
	stdout     io.Writer
	engine     *gatewayruntime.Runtime
	runtimeDir string
	coord      *gatewaycoord.Coordinator
	cache      gatewaycoord.GatewayStateCache
	cleanupMu  sync.Mutex
	cleaned    bool
}

func newRuntimeFromLiveConfig(source *liveconfig.Source, live liveconfig.Config, adapter platform.Adapter, stdout io.Writer) (*runtime, error) {
	runtimeDir, err := config.RuntimeDir()
	if err != nil {
		return nil, err
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	r := &runtime{
		live:       live,
		adapter:    adapter,
		stdout:     stdout,
		runtimeDir: runtimeDir,
		coord:      gatewaycoord.New(runtimeDir),
	}
	engine, err := gatewayruntime.NewWithStop(source, live, token, r.stopFromRouter)
	if err != nil {
		return nil, err
	}
	r.engine = engine
	return r, nil
}

func (r *runtime) Serve(ctx context.Context) error {
	return Gateway{Adapter: r.adapter, Stdout: r.stdout}.serveRuntime(ctx, r)
}

func (g Gateway) serveRuntime(ctx context.Context, r *runtime) error {
	if err := r.ensureSingleInstance(); err != nil {
		r.Close()
		return err
	}

	if r.live.CATrusted() {
		caDir, err := config.CADir()
		if err != nil {
			r.Close()
			return err
		}
		authority, result, err := userca.Ensure(caDir, r.adapter)
		if err != nil {
			cleanupErr := r.Close()
			if errors.Is(err, platform.ErrTrustApprovalDenied) {
				fmt.Fprintln(r.stdout, "Certificate trust was not approved.")
				fmt.Fprintln(r.stdout, "Run the command again and approve the system prompt.")
			}
			if cleanupErr != nil {
				return fmt.Errorf("%w; %v", err, cleanupErr)
			}
			return err
		}
		if err := r.engine.SetAuthority(authority); err != nil {
			r.Close()
			return err
		}
		if result.Changed {
			fmt.Fprintln(r.stdout, "Installed User CA added to the current user's SSL trust settings.")
		}
	} else if err := r.engine.SetAuthority(nil); err != nil {
		r.Close()
		return err
	}

	if err := r.adapter.InstallPAC(r.engine.PACURL()); err != nil {
		r.Close()
		return err
	}
	cache := gatewaycoord.GatewayStateCache{HTTPRouterListen: r.engine.RouterListen(), Token: r.engine.Token()}
	if err := r.writeGatewayStateCache(cache); err != nil {
		r.Close()
		return err
	}

	writeStartGuidance(r.stdout, r.live)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-watchLease(runCtx, r.coord, cache):
			cancel()
		case <-runCtx.Done():
		}
	}()
	if err := r.engine.Serve(runCtx); err != nil {
		_ = r.cleanup()
		return err
	}
	return r.cleanup()
}

func (r *runtime) Close() error {
	return r.engine.Close()
}

func (r *runtime) cleanup() error {
	r.cleanupMu.Lock()
	defer r.cleanupMu.Unlock()
	if r.cleaned {
		return nil
	}
	err := cleanup.CleanOwned(r.runtimeDir, r.adapter, r.cache)
	if err == nil {
		r.cleaned = true
	}
	return err
}

func (r *runtime) stopFromRouter() error {
	_ = r.engine.CloseTraffic()
	return r.cleanup()
}

func Stop(stdout, _ io.Writer) error {
	return Gateway{Stdout: stdout, Adapter: platform.CurrentAdapter}.Stop()
}

func (g Gateway) Stop() error {
	stdout := g.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	adapter := g.Adapter
	if adapter == nil {
		adapter = platform.CurrentAdapter
	}
	coord, err := gatewaycoord.Default()
	if err != nil {
		return err
	}
	verification := coord.Verify()
	if verification.Status != gatewaycoord.Active {
		fmt.Fprintln(stdout, "seamless-cors stop requested; no running seamless-cors found")
		return cleanRuntime(adapter)
	}
	stopped, err := control.CallStop("http://"+verification.Cache.HTTPRouterListen, verification.Cache.Token, stdout)
	if err != nil {
		return err
	}
	if stopped {
		gatewaycoord.WaitForStop(verification.Cache)
	}
	return cleanRuntime(adapter)
}

func Status(stdout, _ io.Writer) error {
	return Gateway{Stdout: stdout, Adapter: platform.CurrentAdapter}.Status()
}

func (g Gateway) Status() error {
	stdout := g.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	adapter := g.Adapter
	if adapter == nil {
		adapter = platform.CurrentAdapter
	}
	coord, err := gatewaycoord.Default()
	if err != nil {
		return err
	}
	verification := coord.Verify()
	if verification.Status == gatewaycoord.Missing {
		fmt.Fprintln(stdout, "seamless-cors status: not running")
		reportCAHealth(stdout, adapter)
		reportCleanupNeeded(stdout, adapter, false)
		return nil
	}
	if verification.Status == gatewaycoord.Stale {
		fmt.Fprintln(stdout, "seamless-cors status: not running")
		reportCAHealth(stdout, adapter)
		fmt.Fprintln(stdout, "stale Gateway State Cache detected; run start or stop to clean up")
		reportCleanupNeeded(stdout, adapter, true)
		return nil
	}
	if err := control.CallStatus("http://"+verification.Cache.HTTPRouterListen, verification.Cache.Token, stdout); err != nil {
		return err
	}
	reportCAHealth(stdout, adapter)
	return nil
}

func (r *runtime) ensureSingleInstance() error {
	cleanupNeeded, err := r.coord.EnsureStartAllowed()
	if err != nil {
		return err
	}
	if cleanupNeeded {
		return cleanup.Clean(r.runtimeDir, r.adapter)
	}
	return nil
}

func (r *runtime) writeGatewayStateCache(cache gatewaycoord.GatewayStateCache) error {
	if err := r.coord.Claim(cache); err != nil {
		return err
	}
	r.cache = cache
	return nil
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

func cleanRuntime(adapter cleanup.Adapter) error {
	runtimeDir, err := config.RuntimeDir()
	if err != nil {
		return err
	}
	return cleanup.Clean(runtimeDir, adapter)
}

func activeOwnerState() (bool, error) {
	coord, err := gatewaycoord.Default()
	if err != nil {
		return false, err
	}
	return coord.Verify().Status == gatewaycoord.Active, nil
}

func Running() (bool, error) {
	coord, err := gatewaycoord.Default()
	if err != nil {
		return false, err
	}
	verification := coord.Verify()
	if verification.Status != gatewaycoord.Active {
		return false, nil
	}
	state, err := control.FetchStatus("http://"+verification.Cache.HTTPRouterListen, verification.Cache.Token)
	if err != nil {
		return false, nil
	}
	return state.RuntimeActive, nil
}

func reportCleanupNeeded(stdout io.Writer, adapter platform.Adapter, staleState bool) {
	runtimeDir, err := config.RuntimeDir()
	if err != nil {
		return
	}
	if cleanup.Inspect(runtimeDir, adapter, staleState).Needed() {
		fmt.Fprintln(stdout, "cleanup-needed: run `seamless-cors stop` to clean seamless-cors-owned gateway footprint")
	}
}

func reportCAHealth(stdout io.Writer, adapter platform.Adapter) {
	if adapter.Capabilities().CATrustManagement != platform.CapabilitySupported {
		fmt.Fprintln(stdout, "installed-ca: unsupported")
		return
	}
	caDir, err := config.CADir()
	if err != nil {
		fmt.Fprintln(stdout, "installed-ca: unknown")
		return
	}
	report, err := userca.Inspect(caDir, adapter)
	if err != nil {
		fmt.Fprintln(stdout, "installed-ca: unknown")
		return
	}
	fmt.Fprintf(stdout, "installed-ca: %s\n", report.Health)
	if !report.Expires.IsZero() {
		fmt.Fprintf(stdout, "installed-ca-expires: %s\n", report.Expires.Format("2006-01-02"))
	}
}
