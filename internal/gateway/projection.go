package gateway

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/QzCurious/seamless-cors/internal/httpsfacade"
	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
	"github.com/QzCurious/seamless-cors/internal/pacrouting"
	"github.com/QzCurious/seamless-cors/internal/proxy"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

type runtimeUpstreamListSource struct {
	kind            UpstreamListSourceKind
	path            string
	optional        bool
	observation     *fileobservation.Observation
	projection      upstreamlist.Projection
	fileSyncIssue   *FileSyncIssue
	projectionIssue *UpstreamListProjectionIssue
}

type runtimeUpstreamListInput struct {
	kind        UpstreamListSourceKind
	path        string
	optional    bool
	observation *fileobservation.Observation
	initial     fileobservation.Outcome
}

type trafficProjectionSemantics struct {
	pacContent        string
	httpCORSRoutes    bool
	httpsCORSRoutes   bool
	httpsFacadeRoutes bool
	userCAIdentity    string
}

func initialRuntimeUpstreamListSource(input runtimeUpstreamListInput) (runtimeUpstreamListSource, error) {
	source := runtimeUpstreamListSource{kind: input.kind, path: input.path, optional: input.optional, observation: input.observation}
	_, err := source.adopt(input.initial)
	return source, err
}

// adopt retains the previous projection on observation failure and fails closed
// on rejected contents. The result says whether new contents were adopted.
func (source *runtimeUpstreamListSource) adopt(outcome fileobservation.Outcome) (bool, error) {
	switch outcome := optionalMissingAsEmpty(source.optional, outcome).(type) {
	case fileobservation.Contents:
		candidate, err := upstreamlist.Project(outcome)
		source.fileSyncIssue = nil
		source.projectionIssue = nil
		if err != nil {
			source.projection = upstreamlist.Projection{}
			source.projectionIssue = &UpstreamListProjectionIssue{Cause: err.Error()}
		} else {
			source.projection = candidate
		}
		return true, nil
	case fileobservation.ReadError:
		source.fileSyncIssue = &FileSyncIssue{Kind: FileSyncIssueFileUnreadable, Cause: outcome.Error()}
	case fileobservation.ObservationStoppedError:
		source.fileSyncIssue = &FileSyncIssue{Kind: FileSyncIssueObservationStopped, Cause: outcome.Error()}
	default:
		return false, fmt.Errorf("unsupported file observation outcome %T", outcome)
	}
	return false, nil
}

func optionalMissingAsEmpty(optional bool, outcome fileobservation.Outcome) fileobservation.Outcome {
	if optional {
		if readErr, ok := outcome.(fileobservation.ReadError); ok && errors.Is(readErr.Cause, os.ErrNotExist) {
			return fileobservation.Contents(nil)
		}
	}
	return outcome
}

func (f *lifecycle) watchUpstreamList(active *activeRuntime, index int) {
	source := &active.upstreamLists[index]
	if source.observation == nil {
		return
	}
	for {
		select {
		case <-active.ctx.Done():
			return
		case outcome, ok := <-source.observation.Outcomes():
			if !ok {
				return
			}
			if err := f.applyUpstreamListOutcome(active, index, outcome); err != nil {
				select {
				case f.fatal <- err:
				default:
				}
				return
			}
		}
	}
}

func mergedUpstreamList(active *activeRuntime) upstreamlist.Projection {
	projections := make([]upstreamlist.Projection, 0, len(active.upstreamLists))
	for _, source := range active.upstreamLists {
		projections = append(projections, source.projection)
	}
	return upstreamlist.Merge(projections...)
}

func (f *lifecycle) applyUpstreamListOutcome(active *activeRuntime, sourceIndex int, outcome fileobservation.Outcome) error {
	f.changeMu.Lock()
	f.mu.Lock()
	if f.ownerEnding || f.runtime != active {
		f.mu.Unlock()
		f.changeMu.Unlock()
		return nil
	}
	// Adopt source facts and atomically publish their traffic consequences.
	adopted, err := active.upstreamLists[sourceIndex].adopt(outcome)
	changed := adopted && f.publishTrafficLocked(active)
	f.mu.Unlock()
	if changed {
		_, _ = f.deliverSystemPAC(active.ctx, active)
	}
	f.changeMu.Unlock()
	// CA admission always precedes changeMu, including on reassessment.
	if adopted {
		f.reassessUserCA(active)
	}
	return err
}

func (f *lifecycle) publishTrafficLocked(active *activeRuntime) bool {
	r := active.engine
	projection := mergedUpstreamList(active)
	hasHost, hasHTTPOrigin, hasHTTPSOrigin := selectorFacts(projection)
	httpDemand := hasHost || hasHTTPOrigin
	httpsDemand := hasHTTPSOrigin || (hasHost && f.userCAState.Usable)
	trustedHTTPS := f.userCAAssessmentErr == nil && f.userCAState.Usable && f.userCAState.Identity != "" && f.userCAState.SigningMaterial() != nil &&
		(httpsDemand || hasHTTPOrigin)

	canonical := canonicalUpstreams(projection)
	facades := httpsfacade.Projection{}
	if trustedHTTPS {
		facades = httpsfacade.Project(canonical.OriginSelectors)
	}
	pacContent := pacrouting.Project(
		canonical,
		facades,
		trustedHTTPS,
		r.listeners[0].Addr().String(),
	)
	identity := ""
	if trustedHTTPS {
		identity = f.userCAState.Identity
	}
	semantics := trafficProjectionSemantics{
		pacContent:        pacContent,
		httpCORSRoutes:    httpDemand,
		httpsCORSRoutes:   trustedHTTPS && httpsDemand,
		httpsFacadeRoutes: trustedHTTPS && len(facades.Routes()) > 0,
		userCAIdentity:    identity,
	}
	if served := r.live.current.Load(); served != nil && served.trafficProjectionSemantics == semantics {
		return false
	}
	next := &servedTrafficProjection{
		trafficProjectionSemantics: semantics,
		proxy:                      proxy.New(r.proxyTransport, f.userCAState.SigningMaterialIf(trustedHTTPS), facades),
	}
	r.live.Store(next)
	return true
}

func canonicalUpstreams(projection upstreamlist.Projection) upstreamlist.Projection {
	hosts := append([]upstreamlist.HostSelector(nil), projection.HostSelectors...)
	origins := append([]upstreamlist.OriginSelector(nil), projection.OriginSelectors...)
	sort.Slice(hosts, func(i, j int) bool {
		if hosts[i].Hostname != hosts[j].Hostname {
			return hosts[i].Hostname < hosts[j].Hostname
		}
		return !hosts[i].Wildcard && hosts[j].Wildcard
	})
	sort.Slice(origins, func(i, j int) bool {
		if origins[i].Scheme != origins[j].Scheme {
			return origins[i].Scheme < origins[j].Scheme
		}
		if origins[i].Hostname != origins[j].Hostname {
			return origins[i].Hostname < origins[j].Hostname
		}
		return origins[i].Port < origins[j].Port
	})
	return upstreamlist.Projection{HostSelectors: hosts, OriginSelectors: origins}
}

func selectorFacts(projection upstreamlist.Projection) (hasHost, hasHTTPOrigin, hasHTTPSOrigin bool) {
	hasHost = len(projection.HostSelectors) > 0
	for _, selector := range projection.OriginSelectors {
		hasHTTPOrigin = hasHTTPOrigin || selector.Scheme == "http"
		hasHTTPSOrigin = hasHTTPSOrigin || selector.Scheme == "https"
	}
	return hasHost, hasHTTPOrigin, hasHTTPSOrigin
}

type runtimeState struct {
	ProxyListen           string
	PACListen             string
	UpstreamLists         []UpstreamListSourceDetail
	UpstreamCount         int
	HTTPDemand            bool
	HTTPSDemand           bool
	ServedHTTPCORS        bool
	ServedHTTPSCORS       bool
	ServedHTTPSFacade     bool
	UserCAUsable          bool
	UserCAIdentityMatches bool
}

func (f *lifecycle) runtimeStateLocked(active *activeRuntime) runtimeState {
	r := active.engine
	served := r.live.current.Load().trafficProjectionSemantics
	projection := mergedUpstreamList(active)
	sources := make([]UpstreamListSourceDetail, 0, len(active.upstreamLists))
	for _, source := range active.upstreamLists {
		sources = append(sources, UpstreamListSourceDetail{
			Kind:            source.kind,
			Path:            source.path,
			Warnings:        upstreamListWarningDetails(source.kind, source.path, source.projection.Warnings),
			FileSyncIssue:   source.fileSyncIssue,
			ProjectionIssue: source.projectionIssue,
		})
	}
	hasHost, hasHTTPOrigin, hasHTTPSOrigin := selectorFacts(projection)
	return runtimeState{
		ProxyListen:           r.listeners[0].Addr().String(),
		PACListen:             r.listeners[1].Addr().String(),
		UpstreamLists:         sources,
		UpstreamCount:         len(projection.HostSelectors) + len(projection.OriginSelectors),
		HTTPDemand:            hasHost || hasHTTPOrigin,
		HTTPSDemand:           hasHTTPSOrigin || (hasHost && f.userCAState.Usable),
		ServedHTTPCORS:        served.httpCORSRoutes,
		ServedHTTPSCORS:       served.httpsCORSRoutes,
		ServedHTTPSFacade:     served.httpsFacadeRoutes,
		UserCAUsable:          f.userCAState.Usable,
		UserCAIdentityMatches: f.userCAState.Usable && f.userCAState.Identity != "" && f.userCAState.Identity == served.userCAIdentity,
	}
}
