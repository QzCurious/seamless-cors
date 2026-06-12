package app

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

	"seamless-cors/internal/ca"
	"seamless-cors/internal/cleanup"
	"seamless-cors/internal/config"
	"seamless-cors/internal/control"
	"seamless-cors/internal/corsproxy"
	"seamless-cors/internal/liveconfig"
	"seamless-cors/internal/pacrouting"
	"seamless-cors/internal/platform"
	"seamless-cors/internal/runtimecoord"
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

func Check(stdout, _ io.Writer) error {
	writeCapabilityReport(stdout, platform.CurrentAdapter.Capabilities())
	return nil
}

func Install(stdout, _ io.Writer) error {
	adapter := platform.CurrentAdapter
	if err := requireSupported(adapter.Capabilities()); err != nil {
		return err
	}
	caDir, err := config.CADir()
	if err != nil {
		return err
	}
	_, result, err := ca.Ensure(caDir, adapter)
	if err != nil {
		if errors.Is(err, platform.ErrTrustApprovalDenied) {
			fmt.Fprintln(stdout, "Certificate trust was not approved.")
			fmt.Fprintln(stdout, "Run the command again and approve the system prompt.")
		}
		return err
	}
	if result.Changed {
		fmt.Fprintln(stdout, "Installed User CA installed.")
	} else {
		fmt.Fprintln(stdout, "Installed User CA is already usable.")
	}
	if !result.Expires.IsZero() {
		fmt.Fprintf(stdout, "installed-ca-expires: %s\n", result.Expires.Format("2006-01-02"))
	}
	if loaded, err := config.LoadExisting(""); err == nil && !loaded.Config.CATrusted {
		fmt.Fprintln(stdout, "HTTPS interception is disabled by config: ca-trusted: false.")
		fmt.Fprintln(stdout, "Set ca-trusted: true to use the Installed User CA.")
	}
	return nil
}

func Uninstall(stdout, _ io.Writer) error {
	adapter := platform.CurrentAdapter
	report := adapter.Capabilities()
	if report.CATrustManagement != platform.CapabilitySupported {
		return fmt.Errorf("CA trust management is unsupported on this platform")
	}
	if active, err := activeRuntimeState(); err != nil {
		return err
	} else if active {
		fmt.Fprintln(stdout, "seamless-cors is running; stop it before uninstalling the Installed User CA.")
		return fmt.Errorf("seamless-cors is running")
	}
	caDir, err := config.CADir()
	if err != nil {
		return err
	}
	if err := ca.Uninstall(caDir, adapter); err != nil {
		return err
	}
	if report, err := ca.Inspect(caDir, adapter); err == nil && report.Health != ca.HealthMissing {
		return fmt.Errorf("Installed User CA uninstall incomplete: installed-ca: %s", report.Health)
	}
	fmt.Fprintln(stdout, "Installed User CA uninstalled.")
	return nil
}

func StartWithContextAndInput(ctx context.Context, stdin io.Reader, stdout io.Writer, adapter platform.Adapter) error {
	return Activation{
		Stdin:   stdin,
		Stdout:  stdout,
		Adapter: adapter,
	}.Start(ctx)
}

type Activation struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Adapter platform.Adapter
}

func (a Activation) Start(ctx context.Context) error {
	stdout := a.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	adapter := a.Adapter
	if adapter == nil {
		adapter = platform.CurrentAdapter
	}
	if err := requireSupported(adapter.Capabilities()); err != nil {
		return err
	}
	if active, err := activeRuntimeState(); err != nil {
		return err
	} else if active {
		fmt.Fprintln(stdout, "seamless-cors is already running")
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
	if err := promptForLifecycleConsent(a.Stdin, stdout, consent); err != nil {
		return err
	}

	runtime, err := NewRuntimeFromLiveConfig(source, live, adapter, stdout)
	if err != nil {
		return err
	}
	return a.serveRuntime(ctx, runtime)
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

func writeCapabilityReport(stdout io.Writer, report platform.CapabilityReport) {
	fmt.Fprintf(stdout, "platform: %s\n", report.Platform)
	fmt.Fprintf(stdout, "supported: %t\n", report.Supported)
	fmt.Fprintf(stdout, "pac-management: %s\n", report.PACManagement)
	fmt.Fprintf(stdout, "ca-trust-management: %s\n", report.CATrustManagement)
	fmt.Fprintf(stdout, "runtime-cleanup: %s\n", report.RuntimeCleanup)
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
		fmt.Fprintln(stdout, "Runtime Cleanup removes seamless-cors PAC settings without restoring previous PAC state.")
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

type Runtime struct {
	mu         sync.RWMutex
	live       liveconfig.Config
	adapter    platform.Adapter
	stdout     io.Writer
	authority  *ca.Authority
	proxy      *http.Server
	pacHandler *pacrouting.DynamicHandler
	pac        *http.Server
	control    *control.Server
	listeners  []net.Listener
	token      string
	runtimeDir string
	coord      *runtimecoord.Coordinator
	liveConfig *liveconfig.Source
}

func NewRuntimeFromLiveConfig(source *liveconfig.Source, live liveconfig.Config, adapter platform.Adapter, stdout io.Writer) (*Runtime, error) {
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
	controlListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		proxyListener.Close()
		pacListener.Close()
		return nil, fmt.Errorf("control listener unavailable: %w", err)
	}

	runtimeDir, err := config.RuntimeDir()
	if err != nil {
		proxyListener.Close()
		pacListener.Close()
		controlListener.Close()
		return nil, err
	}
	token, err := randomToken()
	if err != nil {
		proxyListener.Close()
		pacListener.Close()
		controlListener.Close()
		return nil, err
	}
	proxyListen := proxyListener.Addr().String()
	pacListen := pacListener.Addr().String()
	controlListen := controlListener.Addr().String()

	pacBody := pacrouting.Generate(pacrouting.Options{ProxyListen: proxyListen, CATrusted: live.CATrusted(), Entries: entries})
	pacHandler := pacrouting.NewDynamicHandler(pacBody)
	controlServer := control.New(control.State{
		ProxyListen:      proxyListen,
		PACListen:        pacListen,
		ControlListen:    controlListen,
		DomainList:       live.DomainListPath(),
		CATrusted:        live.CATrusted(),
		DomainCount:      len(entries),
		PendingLifecycle: live.PendingLifecycle(),
	}, token)
	return &Runtime{
		live:       live,
		adapter:    adapter,
		stdout:     stdout,
		liveConfig: source,
		pacHandler: pacHandler,
		proxy:      &http.Server{},
		pac:        &http.Server{Handler: pacHandler},
		control:    controlServer,
		listeners:  []net.Listener{proxyListener, pacListener, controlListener},
		token:      token,
		runtimeDir: runtimeDir,
		coord:      runtimecoord.New(runtimeDir),
	}, nil
}

func (r *Runtime) Serve(ctx context.Context) error {
	return Activation{Adapter: r.adapter, Stdout: r.stdout}.serveRuntime(ctx, r)
}

func (a Activation) serveRuntime(ctx context.Context, r *Runtime) error {
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
		authority, result, err := ca.Ensure(caDir, r.adapter)
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
		r.authority = authority
		if result.Changed {
			fmt.Fprintln(r.stdout, "Installed User CA added to the current user's SSL trust settings.")
		}
	}
	proxyHandler, err := corsproxy.New(corsproxy.Options{
		CATrusted: r.live.CATrusted(),
		Authority: r.authority,
	})
	if err != nil {
		r.Close()
		return err
	}
	r.proxy.Handler = proxyHandler

	errs := make(chan error, 4)
	go func() { errs <- r.control.Serve(r.listeners[2]) }()

	if err := r.adapter.InstallPAC("http://" + r.listeners[1].Addr().String() + "/" + platform.PACFootprintFileName); err != nil {
		r.Close()
		return err
	}
	state := runtimecoord.RuntimeState{State: r.controlState(), Token: r.token}
	if err := r.writeRuntimeState(state); err != nil {
		r.Close()
		return err
	}

	go r.watchLiveConfig(ctx, errs)
	go func() { errs <- r.proxy.Serve(r.listeners[0]) }()
	go func() { errs <- r.pac.Serve(r.listeners[1]) }()
	writeStartGuidance(r.stdout, r.live)

	select {
	case <-ctx.Done():
		return r.Close()
	case err := <-errs:
		if err == http.ErrServerClosed {
			return r.Close()
		}
		r.Close()
		return err
	}
}

func (r *Runtime) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	_ = r.proxy.Close()
	_ = r.pac.Close()
	_ = r.control.Close(ctx)
	return cleanup.Clean(r.runtimeDir, r.adapter)
}

func Stop(stdout, _ io.Writer) error {
	coord, err := runtimecoord.Default()
	if err != nil {
		return err
	}
	verification := coord.Verify()
	if verification.Status != runtimecoord.Active {
		fmt.Fprintln(stdout, "seamless-cors stop requested; no running seamless-cors found")
		return cleanRuntime(platform.CurrentAdapter)
	}
	stopped, err := control.CallStop("http://"+verification.State.ControlListen, verification.State.Token, stdout)
	if err != nil {
		return err
	}
	if stopped {
		runtimecoord.WaitForStop(verification.State)
	}
	return cleanRuntime(platform.CurrentAdapter)
}

func Status(stdout, _ io.Writer) error {
	coord, err := runtimecoord.Default()
	if err != nil {
		return err
	}
	verification := coord.Verify()
	if verification.Status == runtimecoord.Missing {
		fmt.Fprintln(stdout, "seamless-cors status: not running")
		reportCAHealth(stdout, platform.CurrentAdapter)
		reportCleanupNeeded(stdout, platform.CurrentAdapter, false)
		return nil
	}
	if verification.Status == runtimecoord.Stale {
		fmt.Fprintln(stdout, "seamless-cors status: not running")
		reportCAHealth(stdout, platform.CurrentAdapter)
		fmt.Fprintln(stdout, "stale runtime state detected; run start or stop to clean up")
		reportCleanupNeeded(stdout, platform.CurrentAdapter, true)
		return nil
	}
	if err := control.CallStatus("http://"+verification.State.ControlListen, verification.State.Token, stdout); err != nil {
		return err
	}
	reportCAHealth(stdout, platform.CurrentAdapter)
	return nil
}

func (r *Runtime) ensureSingleInstance() error {
	cleanupNeeded, err := r.coord.EnsureStartAllowed()
	if err != nil {
		return err
	}
	if cleanupNeeded {
		return cleanup.Clean(r.runtimeDir, r.adapter)
	}
	return nil
}

func (r *Runtime) writeRuntimeState(state runtimecoord.RuntimeState) error {
	if err := r.coord.Write(state); err == nil {
		return nil
	} else if !os.IsExist(err) {
		return err
	}
	if r.coord.Verify().Status == runtimecoord.Active {
		return fmt.Errorf("seamless-cors is already running")
	}
	if err := cleanup.Clean(r.runtimeDir, r.adapter); err != nil {
		return err
	}
	return r.coord.Write(state)
}

func (r *Runtime) watchLiveConfig(ctx context.Context, errs chan<- error) {
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
				case errs <- event.Err:
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
	state := r.controlStateLocked()
	r.mu.Unlock()
	r.pacHandler.Set(pacrouting.Generate(pacrouting.Options{
		ProxyListen: r.listeners[0].Addr().String(),
		CATrusted:   live.CATrusted(),
		Entries:     live.Entries(),
	}))
	r.control.SetState(state)
}

func (r *Runtime) controlState() control.State {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.controlStateLocked()
}

func (r *Runtime) controlStateLocked() control.State {
	entries := r.live.Entries()
	return control.State{
		ProxyListen:      r.listeners[0].Addr().String(),
		PACListen:        r.listeners[1].Addr().String(),
		ControlListen:    r.listeners[2].Addr().String(),
		DomainList:       r.live.DomainListPath(),
		CATrusted:        r.live.CATrusted(),
		DomainCount:      len(entries),
		PendingLifecycle: r.live.PendingLifecycle(),
	}
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

func activeRuntimeState() (bool, error) {
	coord, err := runtimecoord.Default()
	if err != nil {
		return false, err
	}
	return coord.Verify().Status == runtimecoord.Active, nil
}

func reportCleanupNeeded(stdout io.Writer, adapter platform.Adapter, staleState bool) {
	runtimeDir, err := config.RuntimeDir()
	if err != nil {
		return
	}
	if cleanup.Inspect(runtimeDir, adapter, staleState).Needed() {
		fmt.Fprintln(stdout, "cleanup-needed: run `seamless-cors stop` to clean seamless-cors-owned runtime state")
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
	report, err := ca.Inspect(caDir, adapter)
	if err != nil {
		fmt.Fprintln(stdout, "installed-ca: unknown")
		return
	}
	fmt.Fprintf(stdout, "installed-ca: %s\n", report.Health)
	if !report.Expires.IsZero() {
		fmt.Fprintf(stdout, "installed-ca-expires: %s\n", report.Expires.Format("2006-01-02"))
	}
}
