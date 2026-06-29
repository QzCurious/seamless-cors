package managedpac

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"seamless-cors/internal/platform"
)

var ErrManagedPACLeaseLost = errors.New("managed PAC lease lost")

type Adapter interface {
	InstallPAC(url string, services []string) ([]string, error)
	RefreshPAC(url string, services []string) error
	CurrentPACState() ([]platform.PACServiceState, error)
}

type Ownership string

const (
	OwnershipEmpty   Ownership = "empty"
	OwnershipOwned   Ownership = "owned"
	OwnershipForeign Ownership = "foreign"
)

type ServiceState struct {
	ServiceName string
	Enabled     bool
	URL         string
	Ownership   Ownership
}

type Assessment struct {
	ServiceSet          []string
	States              []ServiceState
	ReplacementRequired bool
}

func Assess(adapter Adapter) (Assessment, error) {
	states, err := adapter.CurrentPACState()
	if err != nil {
		return Assessment{}, err
	}
	managedStates := serviceStates(states)
	serviceSet := make([]string, 0, len(managedStates))
	replacementRequired := false
	for _, state := range managedStates {
		serviceSet = append(serviceSet, state.ServiceName)
		if state.Ownership == OwnershipForeign {
			replacementRequired = true
		}
	}
	sort.Strings(serviceSet)
	return Assessment{
		ServiceSet:          serviceSet,
		States:              managedStates,
		ReplacementRequired: replacementRequired,
	}, nil
}

type Session struct {
	mu           sync.RWMutex
	adapter      Adapter
	services     []string
	currentURL   string
	attemptedURL string
}

type StartResult struct {
	InstalledServices []string
}

func Start(adapter Adapter, services []string, pacURL string) (*Session, StartResult, error) {
	selected := sortedStrings(services)
	if len(selected) == 0 {
		return nil, StartResult{}, fmt.Errorf("managed PAC service set is empty")
	}
	installed, err := adapter.InstallPAC(pacURL, selected)
	if err != nil {
		return nil, StartResult{}, err
	}
	if len(installed) == 0 {
		return nil, StartResult{}, fmt.Errorf("managed PAC install updated no services")
	}
	installed = sortedStrings(installed)
	return &Session{
		adapter:    adapter,
		services:   selected,
		currentURL: pacURL,
	}, StartResult{InstalledServices: installed}, nil
}

func (s *Session) Refresh(nextURL string) error {
	s.mu.Lock()
	s.attemptedURL = nextURL
	services := append([]string(nil), s.services...)
	s.mu.Unlock()
	if err := s.adapter.RefreshPAC(nextURL, services); err != nil {
		return RefreshError{
			FromURL: s.CurrentURL(),
			ToURL:   nextURL,
			Err:     err,
		}
	}
	s.mu.Lock()
	s.currentURL = nextURL
	s.attemptedURL = ""
	s.mu.Unlock()
	return nil
}

func (s *Session) RequireLease() error {
	states, err := s.adapter.CurrentPACState()
	if err != nil {
		return fmt.Errorf("managed PAC lease inspection failed: %w", err)
	}
	if !leaseHeld(states, s.CurrentURL(), s.Services()) {
		return ErrManagedPACLeaseLost
	}
	return nil
}

func (s *Session) CurrentURL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentURL
}

func (s *Session) AttemptedURL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.attemptedURL
}

func (s *Session) Services() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.services...)
}

type RefreshError struct {
	FromURL string
	ToURL   string
	Err     error
}

func (e RefreshError) Error() string {
	return fmt.Sprintf("managed PAC refresh failed from %q to %q: %v", e.FromURL, e.ToURL, e.Err)
}

func (e RefreshError) Unwrap() error {
	return e.Err
}

func serviceStates(states []platform.PACServiceState) []ServiceState {
	out := make([]ServiceState, 0, len(states))
	for _, state := range states {
		out = append(out, ServiceState{
			ServiceName: state.Name,
			Enabled:     state.Enabled,
			URL:         state.URL,
			Ownership:   ownership(state.URL),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ServiceName < out[j].ServiceName
	})
	return out
}

func ownership(raw string) Ownership {
	if raw == "" || raw == "(null)" {
		return OwnershipEmpty
	}
	if platform.IsManagedPACFootprint(raw) {
		return OwnershipOwned
	}
	return OwnershipForeign
}

func leaseHeld(states []platform.PACServiceState, currentURL string, services []string) bool {
	if currentURL == "" || len(services) == 0 {
		return false
	}
	managed := stringSet(services)
	for _, state := range states {
		if _, ok := managed[state.Name]; !ok {
			continue
		}
		if !state.Enabled || state.URL != currentURL {
			return false
		}
	}
	return true
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
