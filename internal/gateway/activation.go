package gateway

import (
	"context"
	"fmt"

	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
)

// activate completes an accepted Start on the owner's cancellation context.
func (f *lifecycle) activate(ctx context.Context, directoryPath string, create bool) (result StartResult, resultErr error) {
	var creationErr error
	if create {
		creationErr = createUpstreamList(f.globalUpstreamListPath)
	}
	defer func() {
		if creationErr != nil && result != nil {
			result = withUpstreamListCreationWarning(result, creationErr)
		}
	}()

	// Establish source and CA facts before publishing any traffic.
	inputs := []runtimeUpstreamListInput{
		{kind: UpstreamListSourceGlobal, path: f.globalUpstreamListPath},
		{kind: UpstreamListSourceDirectory, path: directoryPath, optional: true},
	}
	sources := make([]runtimeUpstreamListSource, 0, len(inputs))
	observationsOwned := false
	defer func() {
		if !observationsOwned {
			for _, input := range inputs {
				if input.observation != nil {
					input.observation.Close()
				}
			}
		}
	}()
	for index := range inputs {
		input := &inputs[index]
		input.observation = fileobservation.Open(input.path)
		select {
		case input.initial = <-input.observation.Outcomes():
		case <-ctx.Done():
			return StartStopCancelled{}, nil
		}
		source, err := initialRuntimeUpstreamListSource(*input)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	f.caAdmissionMu.Lock()
	current, assessmentErr := f.userCA.Inspect(ctx)
	f.caAdmissionMu.Unlock()
	if ctx.Err() != nil {
		return StartStopCancelled{}, nil
	}
	engine, err := newTrafficRuntime(defaultProxyTransport())
	if err != nil {
		return nil, fmt.Errorf("start runtime: %w", err)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	active := &activeRuntime{engine: engine, ctx: runCtx, cancel: cancel, phase: runtimePhaseStarting, upstreamLists: sources}

	f.mu.Lock()
	if f.ownerEnding || ctx.Err() != nil {
		f.mu.Unlock()
		cancel()
		_ = engine.Close()
		return StartStopCancelled{}, nil
	}
	f.userCAState = current
	f.userCAAssessmentErr = assessmentErr
	f.userCARevision++
	f.runtime = active
	f.publishTrafficLocked(active)
	f.mu.Unlock()
	observationsOwned = true
	started := false
	defer func() {
		if started {
			return
		}
		f.mu.Lock()
		// Stop owns cleanup once ending begins, including traffic that is already
		// serving during a cancelled initial PAC delivery.
		ending := f.ownerEnding
		if !ending && f.runtime == active {
			f.runtime = nil
			if f.deadlineTimer != nil {
				f.deadlineTimer.Stop()
				f.deadlineTimer = nil
			}
		}
		f.mu.Unlock()
		if !ending {
			cancel()
			_ = engine.Close()
			for _, source := range sources {
				source.observation.Close()
			}
		}
	}()

	// Traffic must serve before OS PAC settings can point at it.
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		err := engine.ServeReady(runCtx, ready)
		done <- err
		if err != nil {
			select {
			case f.fatal <- err:
			default:
			}
		}
	}()
	select {
	case <-ready:
	case err := <-done:
		return nil, fmt.Errorf("gateway runtime failed before readiness: %w", err)
	case <-ctx.Done():
		return StartStopCancelled{}, nil
	}
	select {
	case err := <-done:
		return nil, fmt.Errorf("gateway runtime failed before System PAC Delivery: %w", err)
	default:
	}

	f.changeMu.Lock()
	report, delivered := f.deliverSystemPAC(ctx, active)
	f.changeMu.Unlock()
	f.mu.Lock()
	if !delivered || f.ownerEnding || ctx.Err() != nil {
		f.mu.Unlock()
		return StartStopCancelled{}, nil
	}
	f.resetUserCADeadlineLocked(active)
	active.phase = runtimePhaseRunning
	state := f.runtimeStateLocked(active)
	installedCA := installedCAStatus(f.userCAState, f.userCAAssessmentErr, false, f.userCACleanupIssue)
	issue := userCAAssessmentIssue(f.userCAAssessmentErr)
	f.mu.Unlock()
	started = true
	for index := range sources {
		go f.watchUpstreamList(active, index)
	}
	return Started{Guidance: StartGuidance{
		UpstreamLists: state.UpstreamLists, SystemPAC: report,
		Traffic:     trafficStatus(state, report.RoutesCurrentEndpoint),
		InstalledCA: installedCA, UserCAIssue: issue,
	}}, nil
}

func authorizeUpstreamListCreation(path string, request StartRequest) (bool, StartResult, error) {
	consent := assessUpstreamListCreation(path)
	if consent == nil {
		return false, nil, nil
	}
	if request.UpstreamListCreationConsent == nil {
		return false, StartUpstreamListCreationConsentRequired{Consent: *consent}, nil
	}
	input := request.UpstreamListCreationConsent
	switch input.Decision {
	case UpstreamListCreationDeclined:
		return false, nil, nil
	case UpstreamListCreationAccepted:
		if input.Fingerprint != consent.Fingerprint {
			return false, nil, fmt.Errorf("Upstream List creation consent does not match the current creation assessment")
		}
		return true, nil, nil
	default:
		return false, nil, fmt.Errorf("invalid Upstream List creation decision %q", input.Decision)
	}
}

func withUpstreamListCreationWarning(result StartResult, err error) StartResult {
	warning := &UpstreamListCreationWarningDetail{Cause: err.Error()}
	switch typed := result.(type) {
	case Started:
		typed.UpstreamListCreationWarning = warning
		return typed
	case StartAlreadyMutating:
		typed.UpstreamListCreationWarning = warning
		return typed
	case StartStopCancelled:
		typed.UpstreamListCreationWarning = warning
		return typed
	case StartCleanupFailed:
		typed.UpstreamListCreationWarning = warning
		return typed
	default:
		return result
	}
}
