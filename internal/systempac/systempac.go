// Package systempac safely coordinates current-user System PAC settings.
package systempac

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/QzCurious/seamless-cors/internal/lib/networkservice"
)

// Module is the complete System PAC integration seam used by Gateway.
type Module interface {
	Deliver(context.Context, string) error
	Inspect(context.Context) (Observation, error)
	Cleanup(context.Context) error
}

type SystemPAC struct {
	mu           sync.Mutex
	generation   uint64
	listServices func(context.Context) ([]networkservice.Service, error)
}

func New() *SystemPAC { return &SystemPAC{listServices: networkservice.List} }

var _ Module = (*SystemPAC)(nil)

// Observation contains facts from fresh platform reads, not an atomic or live snapshot.
type Observation struct {
	Services []ServiceObservation `json:"services"`
}

// Deliver publishes a fresh version for endpoint (host:port). Success requires at least
// one eligible service and every eligible setting verified enabled with that URL.
// Foreign settings are preserved; any discovery, read, write, or verification error
// is returned without rolling back successful writes.
func (m *SystemPAC) Deliver(ctx context.Context, endpoint string) error {
	if endpoint == "" {
		return fmt.Errorf("System PAC endpoint is empty")
	}

	// Valid delivery attempts consume a generation and hold serialization through
	// their complete discover-observe-mutate-verify protocol.
	m.mu.Lock()
	defer m.mu.Unlock()

	// Discover current services and observe which settings we may replace.
	services, discoveryErr := m.listServices(ctx)
	before, observationErrs := observeServices(ctx, services)

	// Publish a fresh generation to empty or owned settings, continuing on failure.
	m.generation++
	nextURL := publicationURL(endpoint, m.generation)
	var mutationErrs []error
	eligible := 0
	for i, service := range services {
		state := before[i].State
		if !state.Manageable() {
			continue
		}
		eligible++
		if err := service.SetPAC(ctx, nextURL); err != nil {
			mutationErrs = append(mutationErrs, MutationError{ServiceName: service.Name(), Cause: err})
		}
	}

	// Verify every eligible service has the exact publication enabled.
	verified, verificationErrs := observeServices(ctx, services)
	for i, service := range verified {
		if err := verificationErrs[i]; err != nil {
			observation := err.(ObservationError)
			verificationErrs[i] = VerificationError{ServiceName: observation.ServiceName, Cause: observation.Cause}
			continue
		}
		prior := before[i].State
		if !prior.Manageable() {
			continue
		}
		if state := service.State; state != nil && (!state.Enabled || state.URL != nextURL) {
			verificationErrs[i] = DeliveryMismatchError{ServiceName: service.Name, PACURL: nextURL, Observed: *state}
		}
	}
	var deliveryErr error
	if eligible == 0 {
		deliveryErr = NoEligibleServicesError{}
	}
	return errors.Join(discoveryErr, errors.Join(observationErrs...), errors.Join(mutationErrs...), errors.Join(verificationErrs...), deliveryErr)
}

// Inspect discovers and reads current settings without changing them.
func (m *SystemPAC) Inspect(ctx context.Context) (Observation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	services, err := m.listServices(ctx)
	observed, observationErrs := observeServices(ctx, services)
	return Observation{Services: observed}, errors.Join(err, errors.Join(observationErrs...))
}

// Cleanup disables active owned settings and verifies that none remain active.
// It preserves foreign settings and retained URLs, and returns any uncertainty.
func (m *SystemPAC) Cleanup(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	services, err := m.listServices(ctx)
	var operationErrs []error
	if err != nil {
		operationErrs = append(operationErrs, err)
	}
	before, observationErrs := observeServices(ctx, services)
	operationErrs = append(operationErrs, observationErrs...)
	for i, service := range services {
		state := before[i].State
		if state == nil || !state.Enabled || !state.Owned() {
			continue
		}
		if err := service.DisablePAC(ctx); err != nil {
			operationErrs = append(operationErrs, MutationError{ServiceName: service.Name(), Cause: err})
		}
	}
	verified, verificationErrs := observeServices(ctx, services)
	for i, err := range verificationErrs {
		if err != nil {
			observation := err.(ObservationError)
			verificationErrs[i] = VerificationError{ServiceName: observation.ServiceName, Cause: observation.Cause}
		}
	}
	operationErrs = append(operationErrs, verificationErrs...)
	var residue []string
	for _, service := range verified {
		state := service.State
		if state != nil && state.Enabled && state.Owned() {
			residue = append(residue, service.Name)
		}
	}
	if len(residue) > 0 {
		operationErrs = append(operationErrs, ResidueError{Services: residue})
	}
	err = errors.Join(operationErrs...)
	return err
}

// RoutesEndpoint reports whether an enabled observed service uses this PAC endpoint.
func (s Observation) RoutesEndpoint(endpoint string) bool {
	if endpoint == "" {
		return false
	}
	for _, service := range s.Services {
		state := service.State
		if state == nil || !state.Enabled || !state.Owned() {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(state.URL))
		if err == nil && u.Host == endpoint {
			return true
		}
	}
	return false
}

// ServiceObservation retains each visible service even when its PAC read fails.
type ServiceObservation struct {
	Name  string    `json:"name"`
	State *PACState `json:"state,omitempty"` // Nil when the read failed.
}

// PACState is one successfully observed PAC setting.
type PACState struct {
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

// Manageable reports whether this observation permits delivery. A failed read
// has no state and never permits mutation, even though an empty URL does.
func (s *PACState) Manageable() bool {
	return s != nil && (strings.TrimSpace(s.URL) == "" || s.Owned())
}

// Owned identifies our PAC setting independently of whether it is enabled.
func (s *PACState) Owned() bool {
	return s != nil && ownedURL(s.URL)
}

// observeServices returns states and errors in service order. Each error is nil
// or an ObservationError; callers assign any operation-specific meaning.
func observeServices(ctx context.Context, services []networkservice.Service) ([]ServiceObservation, []error) {
	states := make([]ServiceObservation, len(services))
	errs := make([]error, len(services))
	var wg sync.WaitGroup
	for i, service := range services {
		wg.Go(func() {
			setting, err := service.PAC(ctx)
			var state *PACState
			if err == nil {
				state = &PACState{URL: setting.URL, Enabled: setting.Enabled}
			}
			states[i] = ServiceObservation{Name: service.Name(), State: state}
			if err != nil {
				errs[i] = ObservationError{ServiceName: service.Name(), Cause: err}
			}
		})
	}
	wg.Wait()
	return states, errs
}

const marker = "seamless-cors.pac"

func publicationURL(endpoint string, generation uint64) string {
	u := url.URL{Scheme: "http", Host: endpoint, Path: "/" + marker}
	q := u.Query()
	q.Set("v", strconv.FormatUint(generation, 10))
	u.RawQuery = q.Encode()
	return u.String()
}

func ownedURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "http" || path.Base(u.EscapedPath()) != marker {
		return false
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && ip.IsLoopback()
}

type ObservationError struct {
	ServiceName string
	Cause       error
}

func (e ObservationError) Error() string {
	return fmt.Sprintf("observe System PAC for %s: %v", e.ServiceName, e.Cause)
}
func (e ObservationError) Unwrap() error { return e.Cause }

type MutationError struct {
	ServiceName string
	Cause       error
}

func (e MutationError) Error() string {
	return fmt.Sprintf("update System PAC for %s: %v", e.ServiceName, e.Cause)
}
func (e MutationError) Unwrap() error { return e.Cause }

type VerificationError struct {
	ServiceName string
	Cause       error
}

func (e VerificationError) Error() string {
	return fmt.Sprintf("verify System PAC for %s: %v", e.ServiceName, e.Cause)
}
func (e VerificationError) Unwrap() error { return e.Cause }

type ResidueError struct{ Services []string }

func (e ResidueError) Error() string {
	return "active owned System PAC remains on " + strings.Join(e.Services, ", ")
}

// NoEligibleServicesError means no observed setting was safe to replace.
type NoEligibleServicesError struct{}

func (NoEligibleServicesError) Error() string { return "no eligible System PAC services" }

// DeliveryMismatchError identifies settings that did not retain the intended publication.
type DeliveryMismatchError struct {
	ServiceName string
	PACURL      string
	Observed    PACState
}

func (e DeliveryMismatchError) Error() string {
	return fmt.Sprintf("System PAC for %s: expected enabled %q, observed URL %q (enabled=%t)", e.ServiceName, e.PACURL, e.Observed.URL, e.Observed.Enabled)
}
