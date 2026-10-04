package gateway

import (
	"context"
	"fmt"

	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
)

// activate initializes traffic on the foreground process context.
// Its caller always executes Stop after activation returns, including on failure.
func (f *lifecycle) activate(ctx context.Context, directoryPath string, create bool) (result StartResult, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
			return StartCancelled{}, nil
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
		return StartCancelled{}, nil
	}
	engine, err := newTrafficRuntime(defaultProxyTransport())
	if err != nil {
		return nil, fmt.Errorf("start runtime: %w", err)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	active := &activeRuntime{engine: engine, ctx: runCtx, cancel: cancel, upstreamLists: sources}

	f.mu.Lock()
	if f.ownerEnding || ctx.Err() != nil {
		f.mu.Unlock()
		cancel()
		_ = engine.Close()
		return StartCancelled{}, nil
	}
	f.userCAState = current
	f.userCAAssessmentErr = assessmentErr
	f.userCARevision++
	f.runtime = active
	f.publishTrafficLocked(active)
	f.mu.Unlock()
	observationsOwned = true
	// Traffic must serve before OS PAC settings can point at it.
	ready := make(chan struct{})
	go func() {
		err := engine.ServeReady(runCtx, ready)
		select {
		case f.fatal <- err:
		default:
		}
	}()
	select {
	case <-ready:
	case err := <-f.fatal:
		return nil, fmt.Errorf("gateway runtime failed before readiness: %w", err)
	case <-ctx.Done():
		return StartCancelled{}, nil
	}
	select {
	case err := <-f.fatal:
		return nil, fmt.Errorf("gateway runtime failed before System PAC Delivery: %w", err)
	default:
	}

	f.changeMu.Lock()
	report, delivered := f.deliverSystemPAC(ctx, active)
	f.changeMu.Unlock()
	f.mu.Lock()
	if !delivered || f.ownerEnding || ctx.Err() != nil {
		f.mu.Unlock()
		return StartCancelled{}, nil
	}
	f.resetUserCADeadlineLocked(active)
	state := f.runtimeStateLocked(active)
	installedCA := installedCAStatus(f.userCAState, f.userCAAssessmentErr, false, f.userCACleanupIssue)
	issue := userCAAssessmentIssue(f.userCAAssessmentErr)
	f.mu.Unlock()
	for index := range sources {
		go f.watchUpstreamList(active, index)
	}
	return Started{Guidance: StartGuidance{
		UpstreamLists: state.UpstreamLists, SystemPAC: report,
		Traffic:     trafficStatus(state, report.RoutesCurrentEndpoint),
		InstalledCA: installedCA, UserCAIssue: issue,
	}}, nil
}

func withUpstreamListCreationWarning(result StartResult, err error) StartResult {
	warning := &UpstreamListCreationWarningDetail{Cause: err.Error()}
	switch typed := result.(type) {
	case Started:
		typed.UpstreamListCreationWarning = warning
		return typed
	case StartCancelled:
		typed.UpstreamListCreationWarning = warning
		return typed
	default:
		return result
	}
}
