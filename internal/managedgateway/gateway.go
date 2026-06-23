package managedgateway

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
	"seamless-cors/internal/gatewaycoord"
	"seamless-cors/internal/gatewayfacade"
	"seamless-cors/internal/gatewayrouter"
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
	return Gateway{Stdin: stdin, Stdout: stdout, Adapter: adapter}.Start(ctx)
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
	stdout := writerOrDiscard(g.Stdout)
	adapter := adapterOrDefault(g.Adapter)
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
	owner, err := newOwner(adapter, stdout)
	if err != nil {
		return err
	}
	started := false
	return owner.run(ctx, func() error {
		result, err := owner.planAndStart(ctx, g.Stdin, stdout)
		if err != nil {
			return err
		}
		started = result.Kind == gatewayfacade.StartResultStarted || result.Kind == gatewayfacade.StartResultAlreadyRunning
		if !started {
			return fmt.Errorf("gateway start did not activate runtime: %s", result.Kind)
		}
		return nil
	})
}

func (g Gateway) ServeOwner(ctx context.Context) error {
	stdout := writerOrDiscard(g.Stdout)
	adapter := adapterOrDefault(g.Adapter)
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
	owner, err := newOwnerWithCoord(adapter, stdout, coord)
	if err != nil {
		return err
	}
	return owner.run(ctx, func() error {
		fmt.Fprintln(stdout, "gateway owner running")
		return nil
	})
}

type owner struct {
	adapter  platform.Adapter
	stdout   io.Writer
	coord    *gatewaycoord.Coordinator
	token    string
	cache    gatewaycoord.GatewayStateCache
	listener net.Listener
	facade   *gatewayfacade.Facade
	router   *gatewayrouter.Server
}

func newOwner(adapter platform.Adapter, stdout io.Writer) (*owner, error) {
	coord, err := gatewaycoord.Default()
	if err != nil {
		return nil, err
	}
	return newOwnerWithCoord(adapter, stdout, coord)
}

func newOwnerWithCoord(adapter platform.Adapter, stdout io.Writer, coord *gatewaycoord.Coordinator) (*owner, error) {
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
	return &owner{
		adapter:  adapter,
		stdout:   stdout,
		coord:    coord,
		token:    token,
		cache:    cache,
		listener: listener,
		facade:   facade,
		router:   router,
	}, nil
}

func (o *owner) run(ctx context.Context, afterPublish func() error) error {
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

func (o *owner) shutdown(ctx context.Context) error {
	_, _ = o.facade.Stop()
	return o.closeOwnerOnly(ctx)
}

func (o *owner) closeOwnerOnly(ctx context.Context) error {
	_ = o.coord.RemoveOwned(o.cache)
	return o.router.Close(ctx)
}

func (o *owner) planAndStart(ctx context.Context, stdin io.Reader, stdout io.Writer) (gatewayfacade.StartResult, error) {
	plan, err := o.facade.PlanStart()
	if err != nil {
		return gatewayfacade.StartResult{}, err
	}
	input := gatewayfacade.StartRequest{}
	if plan.Kind == gatewayfacade.StartPlanConsentRequired {
		if err := promptForLifecycleConsentDetail(stdin, stdout, plan.Consent); err != nil {
			return gatewayfacade.StartResult{}, err
		}
		input.Consent = acceptConsent(plan.Consent)
	}
	result, err := o.facade.ExecuteStart(ctx, input)
	if err != nil {
		return gatewayfacade.StartResult{}, err
	}
	renderStartResult(stdout, result)
	if result.Kind == gatewayfacade.StartResultConsentRequired {
		return result, ErrLifecycleConsentDeclined
	}
	if result.Kind == gatewayfacade.StartResultPlatformApprovalDenied {
		return result, platform.ErrTrustApprovalDenied
	}
	return result, nil
}

func Stop(stdout, _ io.Writer) error {
	return Gateway{Stdout: stdout, Adapter: platform.CurrentAdapter}.Stop()
}

func (g Gateway) Stop() error {
	stdout := writerOrDiscard(g.Stdout)
	adapter := adapterOrDefault(g.Adapter)
	coord, err := gatewaycoord.Default()
	if err != nil {
		return err
	}
	verification := coord.Verify()
	if verification.Status != gatewaycoord.Active {
		fmt.Fprintln(stdout, "seamless-cors stop requested; no running seamless-cors found")
		return cleanRuntime(adapter)
	}
	result, err := callStop(verification.Cache)
	if err != nil {
		return err
	}
	renderStopResult(stdout, result)
	if result.Kind == gatewayfacade.StopResultStopped {
		gatewaycoord.WaitForStop(verification.Cache)
		return nil
	}
	return fmt.Errorf("gateway stop failed: %s", cleanupFailureText(result.CleanupFailures))
}

func Status(stdout, _ io.Writer) error {
	return Gateway{Stdout: stdout, Adapter: platform.CurrentAdapter}.Status()
}

func (g Gateway) Status() error {
	stdout := writerOrDiscard(g.Stdout)
	adapter := adapterOrDefault(g.Adapter)
	coord, err := gatewaycoord.Default()
	if err != nil {
		return err
	}
	verification := coord.Verify()
	if verification.Status == gatewaycoord.Active {
		result, err := callStatus(verification.Cache)
		if err != nil {
			return err
		}
		renderStatus(stdout, result)
		return nil
	}
	facade, err := gatewayfacade.New(adapter, coord, "")
	if err != nil {
		return err
	}
	result, err := facade.Status(verification.Status == gatewaycoord.Stale)
	if err != nil {
		return err
	}
	renderStatus(stdout, result)
	if verification.Status == gatewaycoord.Stale {
		fmt.Fprintln(stdout, "stale Gateway State Cache detected; run start or stop to clean up")
	}
	return nil
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
	status, err := callStatus(verification.Cache)
	if err != nil {
		return false, nil
	}
	return status.Kind == gatewayfacade.GatewayStatusRunning, nil
}

func InstallCA(stdout io.Writer, adapter platform.Adapter) error {
	stdout = writerOrDiscard(stdout)
	adapter = adapterOrDefault(adapter)
	coord, err := gatewaycoord.Default()
	if err != nil {
		return err
	}
	var result gatewayfacade.InstallResult
	verification := coord.Verify()
	if verification.Status == gatewaycoord.Active {
		result, err = callInstall(verification.Cache)
	} else {
		facade, facadeErr := gatewayfacade.New(adapter, coord, "")
		if facadeErr != nil {
			return facadeErr
		}
		result, err = facade.Install()
	}
	if err != nil {
		if errors.Is(err, platform.ErrTrustApprovalDenied) {
			fmt.Fprintln(stdout, "Certificate trust was not approved.")
			fmt.Fprintln(stdout, "Run the command again and approve the system prompt.")
		}
		return err
	}
	renderInstallResult(stdout, result)
	return nil
}

func UninstallCA(stdout io.Writer, adapter platform.Adapter) error {
	stdout = writerOrDiscard(stdout)
	adapter = adapterOrDefault(adapter)
	coord, err := gatewaycoord.Default()
	if err != nil {
		return err
	}
	var result gatewayfacade.UninstallResult
	verification := coord.Verify()
	if verification.Status == gatewaycoord.Active {
		result, err = callUninstall(verification.Cache)
	} else {
		facade, facadeErr := gatewayfacade.New(adapter, coord, "")
		if facadeErr != nil {
			return facadeErr
		}
		result, err = facade.Uninstall()
	}
	if err != nil {
		return err
	}
	renderUninstallResult(stdout, result)
	if result.Kind == gatewayfacade.UninstallResultBlockedRuntimeActive {
		return fmt.Errorf("seamless-cors is running")
	}
	return nil
}

func callStatus(cache gatewaycoord.GatewayStateCache) (gatewayfacade.StatusResult, error) {
	var result gatewayfacade.StatusResult
	if err := callJSON(http.MethodGet, cache, "/status", nil, &result); err != nil {
		return result, err
	}
	return result, nil
}

func callStop(cache gatewaycoord.GatewayStateCache) (gatewayfacade.StopResult, error) {
	var result gatewayfacade.StopResult
	if err := callJSON(http.MethodPost, cache, "/stop", nil, &result); err != nil {
		return result, err
	}
	return result, nil
}

func callInstall(cache gatewaycoord.GatewayStateCache) (gatewayfacade.InstallResult, error) {
	var result gatewayfacade.InstallResult
	if err := callJSON(http.MethodPost, cache, "/install", nil, &result); err != nil {
		return result, err
	}
	return result, nil
}

func callUninstall(cache gatewaycoord.GatewayStateCache) (gatewayfacade.UninstallResult, error) {
	var result gatewayfacade.UninstallResult
	if err := callJSON(http.MethodPost, cache, "/uninstall", nil, &result); err != nil {
		return result, err
	}
	return result, nil
}

func callJSON(method string, cache gatewaycoord.GatewayStateCache, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, "http://"+cache.HTTPRouterListen+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("X-Seamless-CORS-Token", cache.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s returned %s: %s", path, resp.Status, strings.TrimSpace(string(data)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
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
	detail := &gatewayfacade.LifecycleConsentDetail{Requirements: []gatewayfacade.ConsentRequirement{{
		Kind: gatewayfacade.ConsentRequirementManagedPACReplacement,
		ManagedPACReplacement: &gatewayfacade.ManagedPACReplacementDetail{
			CurrentPACState: pacStatesForPrompt(req.CurrentPACState),
			CleanupMode:     gatewayfacade.CleanupModeNoPACRestoration,
		},
	}}}
	return promptForLifecycleConsentDetail(stdin, stdout, detail)
}

func promptForLifecycleConsentDetail(stdin io.Reader, stdout io.Writer, detail *gatewayfacade.LifecycleConsentDetail) error {
	if detail == nil || len(detail.Requirements) == 0 {
		return nil
	}
	fmt.Fprintln(stdout, "Explicit Lifecycle Consent required before seamless-cors changes current-user OS-managed state.")
	for _, req := range detail.Requirements {
		if req.Kind != gatewayfacade.ConsentRequirementManagedPACReplacement || req.ManagedPACReplacement == nil {
			continue
		}
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Managed PAC Consent:")
		fmt.Fprintln(stdout, "seamless-cors will replace existing managed PAC state for this run.")
		fmt.Fprintln(stdout, "Gateway Footprint Cleanup removes seamless-cors-owned managed PAC settings without restoring previous PAC state.")
		fmt.Fprintln(stdout, "Current managed PAC state:")
		for _, state := range req.ManagedPACReplacement.CurrentPACState {
			url := state.URL
			if url == "" {
				url = "(empty)"
			}
			fmt.Fprintf(stdout, "  %s: enabled=%t url=%s\n", state.ServiceName, state.Enabled, url)
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

func acceptConsent(detail *gatewayfacade.LifecycleConsentDetail) *gatewayfacade.LifecycleConsentInput {
	if detail == nil {
		return nil
	}
	input := &gatewayfacade.LifecycleConsentInput{}
	for _, req := range detail.Requirements {
		input.AcceptedRequirements = append(input.AcceptedRequirements, req.Kind)
	}
	return input
}

func pacStatesForPrompt(states []platform.PACServiceState) []gatewayfacade.ManagedPACServiceState {
	out := make([]gatewayfacade.ManagedPACServiceState, 0, len(states))
	for _, state := range states {
		out = append(out, gatewayfacade.ManagedPACServiceState{
			ServiceName: state.Name,
			Enabled:     state.Enabled,
			URL:         state.URL,
		})
	}
	return out
}

func renderStartResult(stdout io.Writer, result gatewayfacade.StartResult) {
	switch result.Kind {
	case gatewayfacade.StartResultStarted:
		if result.Guidance != nil {
			if result.Guidance.InstalledCAChanged {
				fmt.Fprintln(stdout, "Installed User CA added to the current user's SSL trust settings.")
			}
			fmt.Fprintln(stdout, "seamless-cors running")
			fmt.Fprintf(stdout, "config: %s\n", result.Guidance.ConfigPath)
			fmt.Fprintf(stdout, "domain-list: %s\n", result.Guidance.DomainListPath)
			if result.Guidance.ManagedPACActive {
				fmt.Fprintln(stdout, "managed-pac: active")
			}
		}
	case gatewayfacade.StartResultAlreadyRunning:
		fmt.Fprintln(stdout, "seamless-cors already running")
	case gatewayfacade.StartResultPlatformApprovalDenied:
		fmt.Fprintln(stdout, "Certificate trust was not approved.")
		fmt.Fprintln(stdout, "Run the command again and approve the system prompt.")
	}
}

func renderStopResult(stdout io.Writer, result gatewayfacade.StopResult) {
	switch result.Kind {
	case gatewayfacade.StopResultStopped:
		fmt.Fprintln(stdout, "seamless-cors stop requested")
	case gatewayfacade.StopResultCleanupFailed:
		fmt.Fprintln(stdout, "seamless-cors stop cleanup failed")
	}
}

func renderInstallResult(stdout io.Writer, result gatewayfacade.InstallResult) {
	switch result.Kind {
	case gatewayfacade.InstallResultInstalled:
		fmt.Fprintln(stdout, "Installed User CA installed.")
	case gatewayfacade.InstallResultAlreadyUsable:
		fmt.Fprintln(stdout, "Installed User CA is already usable.")
	}
	if !result.InstalledCAExpires.IsZero() {
		fmt.Fprintf(stdout, "installed-ca-expires: %s\n", result.InstalledCAExpires.Format("2006-01-02"))
	}
	for _, advisory := range result.Advisories {
		if advisory.Kind == gatewayfacade.InstallAdvisoryConfigCATrustedDisabled && advisory.ConfigCATrustedDisabled != nil {
			fmt.Fprintln(stdout, "HTTPS interception is disabled by config: ca-trusted: false.")
			fmt.Fprintln(stdout, "Set ca-trusted: true to use the Installed User CA.")
		}
	}
}

func renderUninstallResult(stdout io.Writer, result gatewayfacade.UninstallResult) {
	switch result.Kind {
	case gatewayfacade.UninstallResultUninstalled:
		fmt.Fprintln(stdout, "Installed User CA uninstalled.")
	case gatewayfacade.UninstallResultAlreadyAbsent:
		fmt.Fprintln(stdout, "Installed User CA is already absent.")
	case gatewayfacade.UninstallResultBlockedRuntimeActive:
		fmt.Fprintln(stdout, "seamless-cors is running; stop it before uninstalling the Installed User CA.")
	}
}

func renderStatus(stdout io.Writer, result gatewayfacade.StatusResult) {
	switch result.Kind {
	case gatewayfacade.GatewayStatusRunning:
		fmt.Fprintln(stdout, "seamless-cors status: running")
	case gatewayfacade.GatewayStatusRouterOnly:
		fmt.Fprintln(stdout, "seamless-cors status: owner running")
		fmt.Fprintln(stdout, "gateway-runtime: inactive")
	default:
		fmt.Fprintln(stdout, "seamless-cors status: not running")
	}
	if result.Runtime != nil {
		fmt.Fprintf(stdout, "runtime-proxy-endpoint: %s\n", result.Runtime.ProxyListen)
		fmt.Fprintf(stdout, "runtime-pac-endpoint: %s\n", result.Runtime.PACListen)
		if result.Owner != nil {
			fmt.Fprintf(stdout, "gateway-router-endpoint: %s\n", result.Owner.RouterListen)
		}
		fmt.Fprintf(stdout, "domain-list: %s\n", result.Runtime.DomainListPath)
		fmt.Fprintf(stdout, "domains: %d\n", result.Runtime.DomainCount)
		fmt.Fprintln(stdout, "managed-pac: active")
		fmt.Fprintf(stdout, "ca-trusted: %t\n", result.Runtime.CATrusted)
		if len(result.Runtime.PendingLifecycle) > 0 {
			var values []string
			for _, pending := range result.Runtime.PendingLifecycle {
				values = append(values, string(pending))
			}
			fmt.Fprintf(stdout, "pending lifecycle changes: %s\n", strings.Join(values, ", "))
		}
	}
	fmt.Fprintf(stdout, "installed-ca: %s\n", result.InstalledCA.Health)
	if !result.InstalledCA.Expires.IsZero() {
		fmt.Fprintf(stdout, "installed-ca-expires: %s\n", result.InstalledCA.Expires.Format("2006-01-02"))
	}
	if result.Cleanup.Needed {
		fmt.Fprintln(stdout, "cleanup-needed: run `seamless-cors stop` to clean seamless-cors-owned gateway footprint")
	}
}

func cleanupFailureText(failures []gatewayfacade.CleanupFailureDetail) string {
	var parts []string
	for _, failure := range failures {
		if failure.Diagnostic != "" {
			parts = append(parts, failure.Diagnostic)
		} else {
			parts = append(parts, string(failure.Subject))
		}
	}
	return strings.Join(parts, "; ")
}

func writerOrDiscard(stdout io.Writer) io.Writer {
	if stdout == nil {
		return io.Discard
	}
	return stdout
}

func adapterOrDefault(adapter platform.Adapter) platform.Adapter {
	if adapter == nil {
		return platform.CurrentAdapter
	}
	return adapter
}

type runtime struct {
	live       liveconfig.Config
	adapter    platform.Adapter
	stdout     io.Writer
	engine     *gatewayruntime.Runtime
	runtimeDir string
	coord      *gatewaycoord.Coordinator
	cache      gatewaycoord.GatewayStateCache
	token      string
	cleanupMu  sync.Mutex
	cleaned    bool
}

func newRuntimeFromLiveConfig(source *liveconfig.Source, live liveconfig.Config, adapter platform.Adapter, stdout io.Writer) (*runtime, error) {
	runtimeDir, err := config.RuntimeDir()
	if err != nil {
		return nil, err
	}
	engine, err := gatewayruntime.New(source, live)
	if err != nil {
		return nil, err
	}
	token, err := randomToken()
	if err != nil {
		_ = engine.Close()
		return nil, err
	}
	return &runtime{
		live:       live,
		adapter:    adapterOrDefault(adapter),
		stdout:     writerOrDiscard(stdout),
		engine:     engine,
		runtimeDir: runtimeDir,
		coord:      gatewaycoord.New(runtimeDir),
		token:      token,
	}, nil
}

func (r *runtime) Serve(ctx context.Context) error {
	if cleanupNeeded, err := r.coord.EnsureStartAllowed(); err != nil {
		_ = r.engine.Close()
		return err
	} else if cleanupNeeded {
		if err := cleanup.Clean(r.runtimeDir, r.adapter); err != nil {
			_ = r.engine.Close()
			return err
		}
	}
	if r.live.CATrusted() {
		caDir, err := config.CADir()
		if err != nil {
			_ = r.engine.Close()
			return err
		}
		authority, result, err := usercaEnsure(caDir, r.adapter)
		if err != nil {
			_ = r.engine.Close()
			return err
		}
		if err := r.engine.SetAuthority(authority); err != nil {
			_ = r.engine.Close()
			return err
		}
		if result.Changed {
			fmt.Fprintln(r.stdout, "Installed User CA added to the current user's SSL trust settings.")
		}
	} else if err := r.engine.SetAuthority(nil); err != nil {
		_ = r.engine.Close()
		return err
	}
	if err := r.adapter.InstallPAC(r.engine.PACURL()); err != nil {
		_ = r.engine.Close()
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = r.engine.Close()
		return err
	}
	r.cache = gatewaycoord.GatewayStateCache{HTTPRouterListen: listener.Addr().String(), Token: r.token}
	router := gatewayrouter.New(r.token, runtimeFacade{runtime: r, routerListen: listener.Addr().String()})
	errs := make(chan error, 2)
	go func() { errs <- router.Serve(listener) }()
	if err := r.coord.Claim(r.cache); err != nil {
		_ = router.Close(context.Background())
		_ = r.engine.Close()
		return err
	}
	leaseLost := watchLease(ctx, r.coord, r.cache)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { errs <- r.engine.Serve(runCtx) }()
	select {
	case <-ctx.Done():
		_ = router.Close(context.Background())
		_ = r.engine.Close()
		return r.cleanup()
	case <-leaseLost:
		_ = router.Close(context.Background())
		_ = r.engine.Close()
		return r.cleanup()
	case <-router.ShutdownRequested():
		cancel()
		return nil
	case err := <-errs:
		cancel()
		_ = router.Close(context.Background())
		if err == nil || err == http.ErrServerClosed {
			return r.cleanup()
		}
		_ = r.cleanup()
		return err
	}
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

type runtimeFacade struct {
	runtime      *runtime
	routerListen string
}

func (f runtimeFacade) PlanStart() (gatewayfacade.StartPlan, error) {
	return gatewayfacade.StartPlan{Kind: gatewayfacade.StartPlanAlreadyRunning}, nil
}

func (f runtimeFacade) ExecuteStart(context.Context, gatewayfacade.StartRequest) (gatewayfacade.StartResult, error) {
	return gatewayfacade.StartResult{Kind: gatewayfacade.StartResultAlreadyRunning}, nil
}

func (f runtimeFacade) Stop() (gatewayfacade.StopResult, error) {
	_ = f.runtime.engine.CloseTraffic()
	if err := f.runtime.cleanup(); err != nil {
		return gatewayfacade.StopResult{
			Kind:            gatewayfacade.StopResultCleanupFailed,
			CleanupFailures: []gatewayfacade.CleanupFailureDetail{{Subject: gatewayfacade.CleanupSubjectManagedPAC, Diagnostic: err.Error()}},
		}, nil
	}
	return gatewayfacade.StopResult{Kind: gatewayfacade.StopResultStopped}, nil
}

func (f runtimeFacade) Status(bool) (gatewayfacade.StatusResult, error) {
	state := f.runtime.engine.State()
	return gatewayfacade.StatusResult{
		Kind:  gatewayfacade.GatewayStatusRunning,
		Owner: &gatewayfacade.OwnerStatusDetail{RouterListen: f.routerListen},
		Runtime: &gatewayfacade.RuntimeStatusDetail{
			ProxyListen:      state.ProxyListen,
			PACListen:        state.PACListen,
			DomainListPath:   state.DomainList,
			DomainCount:      state.DomainCount,
			CATrusted:        state.CATrusted,
			PendingLifecycle: runtimePendingLifecycle(state.PendingLifecycle),
		},
		InstalledCA: gatewayfacade.InstalledCAStatusDetail{Health: gatewayfacade.CAHealthUnknown},
	}, nil
}

func (f runtimeFacade) Install() (gatewayfacade.InstallResult, error) {
	return gatewayfacade.InstallResult{}, fmt.Errorf("install unavailable")
}

func (f runtimeFacade) Uninstall() (gatewayfacade.UninstallResult, error) {
	return gatewayfacade.UninstallResult{Kind: gatewayfacade.UninstallResultBlockedRuntimeActive}, nil
}

func runtimePendingLifecycle(values []string) []gatewayfacade.PendingLifecycleChangeKind {
	var out []gatewayfacade.PendingLifecycleChangeKind
	for _, value := range values {
		if value == string(gatewayfacade.PendingLifecycleChangeCATrusted) {
			out = append(out, gatewayfacade.PendingLifecycleChangeCATrusted)
		}
	}
	return out
}

func usercaEnsure(caDir string, adapter platform.Adapter) (*userca.Authority, userca.EnsureResult, error) {
	return userca.Ensure(caDir, adapter)
}
