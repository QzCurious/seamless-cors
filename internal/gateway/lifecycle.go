package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/QzCurious/seamless-cors/internal/systempac"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

// Lock order is CA admission, changeMu, then mu. changeMu keeps publication and
// delivery sequential; mu protects retained facts and is never held during I/O.
type lifecycle struct {
	mu                     sync.Mutex
	changeMu               sync.Mutex
	caAdmissionMu          sync.Mutex
	systemPAC              systempac.Module
	userCA                 userCAModule
	userCAState            userCAState
	userCAAssessmentErr    error
	userCARevision         uint64
	deadlineTimer          *time.Timer
	coord                  *coordinator
	globalUpstreamListPath string
	ownerCache             stateCache
	ownerEnding            bool
	caMutating             bool
	runtime                *activeRuntime
	userCACleanupIssue     *UserCACleanupIssue
	fatal                  chan error
	stopOnce               sync.Once
	stopResult             StopResult
}

type activeRuntime struct {
	engine            *trafficRuntime
	latestPACDelivery *SystemPACReport
	ctx               context.Context
	cancel            context.CancelFunc
	upstreamLists     []runtimeUpstreamListSource
}

func newLifecycle(pac systempac.Module, ca userCAModule, coord *coordinator) *lifecycle {
	return &lifecycle{
		systemPAC:              pac,
		userCA:                 ca,
		coord:                  coord,
		globalUpstreamListPath: defaultGlobalUpstreamListPath(),
		fatal:                  make(chan error, 1),
	}
}

// adoptUserCA publishes the retained facts and their traffic consequences. The
// caller holds changeMu through the subsequent delivery, never mu during I/O.
func (f *lifecycle) adoptUserCA(ctx context.Context, current userCAState, assessmentErr error) {
	f.mu.Lock()
	f.userCAState = current
	f.userCAAssessmentErr = assessmentErr
	f.userCARevision++
	active := f.runtime
	changed := false
	if active != nil {
		changed = f.publishTrafficLocked(active)
	}
	f.resetUserCADeadlineLocked(active)
	f.mu.Unlock()
	if changed {
		_, _ = f.deliverSystemPAC(ctx, active)
	}
}

func (f *lifecycle) resetUserCADeadlineLocked(active *activeRuntime) {
	if f.deadlineTimer != nil {
		f.deadlineTimer.Stop()
		f.deadlineTimer = nil
	}
	if active != nil && !f.ownerEnding && f.userCAAssessmentErr == nil && f.userCAState.Usable {
		revision := f.userCARevision
		f.deadlineTimer = time.AfterFunc(time.Until(f.userCAState.ExpiresAt), func() { f.handleUserCADeadline(active, revision) })
	}
}

// beginStop prevents new updates while admitted work settles.
func (f *lifecycle) beginStop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ownerEnding = true
	if f.deadlineTimer != nil {
		f.deadlineTimer.Stop()
		f.deadlineTimer = nil
	}
}

func (f *lifecycle) handleUserCADeadline(active *activeRuntime, revision uint64) {
	f.caAdmissionMu.Lock()
	defer f.caAdmissionMu.Unlock()
	f.changeMu.Lock()
	f.mu.Lock()
	valid := !f.ownerEnding && f.runtime == active && f.userCARevision == revision && f.userCAState.Usable
	f.mu.Unlock()
	if !valid {
		f.changeMu.Unlock()
		return
	}
	// Expiry withdraws HTTPS before inspecting trust again.
	f.adoptUserCA(active.ctx, userCAState{}, nil)
	current, err := f.userCA.Inspect(active.ctx)
	f.adoptUserCA(active.ctx, current, err)
	f.changeMu.Unlock()
}

func (f *lifecycle) reassessUserCA(active *activeRuntime) {
	f.caAdmissionMu.Lock()
	defer f.caAdmissionMu.Unlock()
	f.changeMu.Lock()
	defer f.changeMu.Unlock()
	f.mu.Lock()
	needed := !f.ownerEnding && f.runtime == active && (!f.userCAState.Usable || f.userCAAssessmentErr != nil)
	f.mu.Unlock()
	if !needed {
		return
	}
	current, err := f.userCA.Inspect(active.ctx)
	f.adoptUserCA(active.ctx, current, err)
}

func (f *lifecycle) finishCAMutation() {
	f.mu.Lock()
	f.caMutating = false
	f.mu.Unlock()
	f.caAdmissionMu.Unlock()
}

// Stop runs cleanup once, including when a signal races with a CLI stop.
func (f *lifecycle) Stop(ctx context.Context) (StopResult, error) {
	f.stopOnce.Do(func() { f.stopResult = f.stop(ctx) })
	return f.stopResult, nil
}

func (f *lifecycle) stop(ctx context.Context) StopResult {
	var warnings []CommandWarning
	f.beginStop()
	f.mu.Lock()
	active := f.runtime
	ownerCache := f.ownerCache
	f.mu.Unlock()
	var cleanupFailures []CleanupFailure
	// Settle admitted CA work and every projection/delivery sequence before cleanup.
	f.caAdmissionMu.Lock()
	defer f.caAdmissionMu.Unlock()
	f.changeMu.Lock()
	cleanupErr := f.systemPAC.Cleanup(ctx)
	cleanupObservation, inspectionErr := f.systemPAC.Inspect(ctx)
	cleanupErr = errors.Join(cleanupErr, inspectionErr)
	if cleanupErr != nil {
		cleanupFailures = append(cleanupFailures, CleanupFailure{Subject: CleanupSubjectSystemPAC, Diagnostic: cleanupErr.Error()})
	}
	f.changeMu.Unlock()
	if active != nil {
		active.cancel()
		for _, source := range active.upstreamLists {
			if source.observation != nil {
				source.observation.Close()
			}
		}
		if err := active.engine.CloseTraffic(); err != nil {
			warnings = append(warnings, CommandWarning{Kind: CommandWarningRuntimeCloseFailed, Diagnostic: err.Error()})
		}
	}
	f.mu.Lock()
	if f.runtime == active {
		f.runtime = nil
	}
	f.mu.Unlock()
	var ownedCache *stateCache
	if ownerCache.HTTPRouterListen != "" && ownerCache.Token != "" {
		ownedCache = &ownerCache
	}
	cleanupFailures = append(cleanupFailures, cleanGatewayStateCache(f.coord, ownedCache)...)
	cleanupFulfillment := CommandFulfilled
	if len(cleanupFailures) > 0 {
		cleanupFulfillment = CommandUnfulfilled
	}
	return StopResult{Kind: StopResultStopped, Warnings: warnings, CleanupFulfillment: cleanupFulfillment, SystemPACCleanup: systemPACReport(cleanupObservation, "", cleanupErr), CleanupFailures: cleanupFailures}
}

func (f *lifecycle) Status(ctx context.Context, stale bool) (StatusResult, error) {
	f.mu.Lock()
	active := f.runtime
	ownerCache := f.ownerCache
	ownerEnding := f.ownerEnding
	caState := f.userCAState
	caAssessmentErr := f.userCAAssessmentErr
	caMutating := f.caMutating
	caCleanupIssue := f.userCACleanupIssue
	var state runtimeState
	var latestPACDelivery *SystemPACReport
	if active != nil {
		state = f.runtimeStateLocked(active)
		latestPACDelivery = active.latestPACDelivery
	}
	f.mu.Unlock()
	var endpoint string
	if active != nil {
		endpoint = active.engine.PACListen()
	}
	pacState, pacErr := f.systemPAC.Inspect(ctx)
	currentPAC := systemPACReport(pacState, endpoint, pacErr)
	result := StatusResult{
		Kind: StatusResultReported,
		StatusReport: StatusReport{
			State:                 GatewayStatusNotRunning,
			SystemPAC:             currentPAC,
			Cleanup:               f.cleanupStatus(stale, ownerCache, currentPAC),
			InstalledCA:           installedCAStatus(caState, caAssessmentErr, caMutating, caCleanupIssue),
			UserCAAssessmentIssue: userCAAssessmentIssue(caAssessmentErr),
		},
	}
	if ownerEnding {
		result.State = GatewayStatusEnding
		if ownerCache.HTTPRouterListen != "" {
			result.Owner = &OwnerStatusDetail{RouterListen: ownerCache.HTTPRouterListen}
		}
		return result, nil
	}
	if active != nil {
		result.State = GatewayStatusRunning
		result.Owner = &OwnerStatusDetail{RouterListen: ownerCache.HTTPRouterListen}
		result.Runtime = &RuntimeStatusDetail{
			ProxyListen:             state.ProxyListen,
			PACListen:               state.PACListen,
			UpstreamLists:           state.UpstreamLists,
			UpstreamCount:           state.UpstreamCount,
			Traffic:                 trafficStatus(state, currentPAC.RoutesCurrentEndpoint),
			LatestSystemPACDelivery: latestPACDelivery,
		}
		return result, nil
	}
	if stale {
		result.State = GatewayStatusStaleCache
	}
	return result, nil
}

func upstreamListWarningDetails(kind UpstreamListSourceKind, path string, warnings []upstreamlist.Warning) []UpstreamListWarningDetail {
	details := make([]UpstreamListWarningDetail, 0, len(warnings))
	for _, warning := range warnings {
		details = append(details, UpstreamListWarningDetail{
			Source:     kind,
			Path:       path,
			Line:       warning.Line,
			Text:       warning.Text,
			Diagnostic: warning.Diagnostic,
		})
	}
	return details
}

func (f *lifecycle) Install(ctx context.Context) (InstallResult, error) {
	if err := ctx.Err(); err != nil {
		return InstallResult{}, err
	}
	if !f.caAdmissionMu.TryLock() {
		return InstallResult{Kind: InstallResultAlreadyMutating}, nil
	}
	f.mu.Lock()
	if f.ownerEnding {
		f.mu.Unlock()
		f.caAdmissionMu.Unlock()
		return InstallResult{Kind: InstallResultOwnerEnding}, nil
	}
	f.caMutating = true
	f.mu.Unlock()
	defer f.finishCAMutation()
	f.changeMu.Lock()
	defer f.changeMu.Unlock()
	// Withdraw HTTPS, mutate trust, then adopt the complete returned facts.
	ownerCtx := context.Background()
	f.adoptUserCA(ownerCtx, userCAState{}, nil)
	current, err := f.userCA.Install(ownerCtx)
	if err != nil {
		current = userCAState{}
	}
	f.adoptUserCA(ownerCtx, current, err)
	if err != nil {
		return InstallResult{}, err
	}
	f.mu.Lock()
	f.userCACleanupIssue = nil
	f.mu.Unlock()
	return InstallResult{Kind: InstallResultInstalled, InstalledCAExpires: current.ExpiresAt}, nil
}

func (f *lifecycle) Uninstall(ctx context.Context) (UninstallResult, error) {
	return f.UninstallWithConsent(ctx, "")
}

func (f *lifecycle) UninstallWithConsent(ctx context.Context, consentFingerprint string) (UninstallResult, error) {
	if err := ctx.Err(); err != nil {
		return UninstallResult{}, err
	}
	if !f.caAdmissionMu.TryLock() {
		return UninstallResult{Kind: UninstallResultAlreadyMutating}, nil
	}
	f.mu.Lock()
	if f.ownerEnding {
		f.mu.Unlock()
		f.caAdmissionMu.Unlock()
		return UninstallResult{Kind: UninstallResultOwnerEnding}, nil
	}
	f.caMutating = true
	f.mu.Unlock()
	defer f.finishCAMutation()
	f.changeMu.Lock()
	defer f.changeMu.Unlock()
	// Check consent against the projection this operation will withdraw, after
	// any preceding source publication and delivery have settled.
	f.mu.Lock()
	active := f.runtime
	if active != nil && active.engine.interceptionActive() {
		expected := f.uninstallConsentFingerprint(active)
		if consentFingerprint != expected {
			f.mu.Unlock()
			return UninstallResult{Kind: UninstallResultConsentRequired, ConsentFingerprint: expected}, nil
		}
	}
	f.mu.Unlock()
	ownerCtx := context.Background()
	f.adoptUserCA(ownerCtx, userCAState{}, nil)
	err := f.userCA.Uninstall(ownerCtx)
	f.adoptUserCA(ownerCtx, userCAState{}, err)
	var cleanupIssue *UserCACleanupIssue
	if err != nil {
		cleanupIssue = &UserCACleanupIssue{Cause: err.Error(), Action: "Run `seamless-cors uninstall` again."}
	}
	f.mu.Lock()
	f.userCACleanupIssue = cleanupIssue
	f.mu.Unlock()
	if err != nil {
		return UninstallResult{Kind: UninstallResultIncomplete, CleanupIssue: cleanupIssue}, nil
	}
	return UninstallResult{Kind: UninstallResultUninstalled}, nil
}

func (f *lifecycle) uninstallConsentFingerprint(active *activeRuntime) string {
	sum := sha256.Sum256([]byte(active.engine.listeners[0].Addr().String() + "\x00" + active.engine.PACListen() + "\x00uninstall-all-usercas"))
	return hex.EncodeToString(sum[:])
}

// deliverSystemPAC is a sequential phase of an operation holding changeMu.
func (f *lifecycle) deliverSystemPAC(ctx context.Context, active *activeRuntime) (SystemPACReport, bool) {
	f.mu.Lock()
	admitted := !f.ownerEnding && f.runtime == active
	f.mu.Unlock()
	if !admitted {
		return SystemPACReport{}, false
	}
	endpoint := active.engine.PACListen()
	deliveryErr := f.systemPAC.Deliver(ctx, endpoint)
	observation, inspectionErr := f.systemPAC.Inspect(ctx)
	report := systemPACReport(observation, endpoint, errors.Join(deliveryErr, inspectionErr))
	f.mu.Lock()
	active.latestPACDelivery = &report
	f.mu.Unlock()
	return report, true
}

func (f *lifecycle) cleanupStatus(stale bool, ownerCache stateCache, pac SystemPACReport) CleanupStatusDetail {
	return inspectGatewayFootprint(f.coord, stale, ownerCache, pac)
}

func trafficStatus(state runtimeState, routingReady bool) TrafficStatusDetail {
	httpActive := routingReady && state.ServedHTTPCORS
	httpsActive := routingReady && state.ServedHTTPSCORS && state.UserCAUsable && state.UserCAIdentityMatches
	facadeActive := routingReady && state.ServedHTTPSFacade && state.UserCAUsable && state.UserCAIdentityMatches
	return TrafficStatusDetail{
		RoutingReady: routingReady,
		HTTPCORS:     featureState(httpActive, state.HTTPDemand),
		HTTPSCORS:    featureState(httpsActive, state.HTTPSDemand),
		HTTPSFacade:  featureState(facadeActive, false),
	}
}

func featureState(active, demanded bool) TrafficFeatureState {
	if active {
		return TrafficFeatureActive
	}
	if demanded {
		return TrafficFeatureBlocked
	}
	return TrafficFeatureInactive
}

func installedCAStatus(current userCAState, assessmentErr error, mutating bool, cleanupIssue *UserCACleanupIssue) InstalledCAStatusDetail {
	if mutating {
		return InstalledCAStatusDetail{Health: CAHealthMutating, CleanupIssue: cleanupIssue}
	}
	if assessmentErr != nil || !current.Usable {
		return InstalledCAStatusDetail{Health: CAHealthNotUsable, CleanupIssue: cleanupIssue}
	}
	return InstalledCAStatusDetail{
		Health:       CAHealthUsable,
		Expires:      current.ExpiresAt,
		RenewalDue:   current.RenewalDue,
		CleanupIssue: cleanupIssue,
	}
}

func userCAAssessmentIssue(err error) *UserCAAssessmentIssue {
	if err == nil {
		return nil
	}
	return &UserCAAssessmentIssue{Cause: err.Error()}
}
