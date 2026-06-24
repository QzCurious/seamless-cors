package managedgateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"seamless-cors/internal/cleanup"
	"seamless-cors/internal/config"
	"seamless-cors/internal/gatewaycoord"
	"seamless-cors/internal/gatewayfacade"
	"seamless-cors/internal/gatewayowner"
	"seamless-cors/internal/platform"
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
	owner, err := gatewayowner.New(adapter)
	if err != nil {
		return err
	}
	started := false
	return owner.Run(ctx, func() error {
		result, err := planAndStart(ctx, owner.Facade(), g.Stdin, stdout)
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
	owner, err := gatewayowner.NewWithCoord(adapter, coord)
	if err != nil {
		return err
	}
	return owner.Run(ctx, func() error {
		fmt.Fprintln(stdout, "gateway owner running")
		return nil
	})
}

func planAndStart(ctx context.Context, facade *gatewayfacade.Facade, stdin io.Reader, stdout io.Writer) (gatewayfacade.StartResult, error) {
	plan, err := facade.PlanStart()
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
	result, err := facade.ExecuteStart(ctx, input)
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
