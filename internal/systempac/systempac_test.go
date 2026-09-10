package systempac

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/lib/networkservice"
)

type fakeServiceState struct {
	Name    string
	URL     string
	Enabled bool
}

type fakeSystem struct {
	mu           sync.Mutex
	states       []fakeServiceState
	discoveryErr error
	observeErrs  map[string]error
	setErrs      map[string]error
	setResults   map[string]PACState
	disableErrs  map[string]error
	writes       []string
	discoveries  int
}

func (f *fakeSystem) list(context.Context) ([]networkservice.Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.discoveries++
	services := make([]networkservice.Service, 0, len(f.states))
	for _, state := range f.states {
		services = append(services, fakeService{system: f, name: state.Name})
	}
	return services, f.discoveryErr
}

type fakeService struct {
	system *fakeSystem
	name   string
}

type concurrentObservationService struct {
	networkservice.Service
	started chan<- string
	release <-chan struct{}
}

func (s concurrentObservationService) PAC(ctx context.Context) (networkservice.PACSetting, error) {
	s.started <- s.Name()
	select {
	case <-s.release:
		return s.Service.PAC(ctx)
	case <-ctx.Done():
		return networkservice.PACSetting{}, ctx.Err()
	}
}

func TestObserveReadsServicesConcurrentlyAndPreservesOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	failure := errors.New("query failed")
	system := &fakeSystem{
		states:      []fakeServiceState{{Name: "Wi-Fi"}, {Name: "VPN"}},
		observeErrs: map[string]error{"VPN": failure},
	}
	started := make(chan string, 2)
	release := make(chan struct{})
	module := &SystemPAC{listServices: func(ctx context.Context) ([]networkservice.Service, error) {
		services, err := system.list(ctx)
		for i, service := range services {
			services[i] = concurrentObservationService{Service: service, started: started, release: release}
		}
		return services, err
	}}
	done := make(chan struct{})
	var state Observation
	var err error
	go func() {
		defer close(done)
		state, err = module.Inspect(ctx)
	}()
	defer func() { cancel(); <-done }()

	// Both reads must start before either is allowed to finish.
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("service observations did not start concurrently")
		}
	}
	close(release)
	<-done
	if len(state.Services) != 2 || state.Services[0].Name != "Wi-Fi" || state.Services[1].Name != "VPN" {
		t.Fatalf("services = %#v", state.Services)
	}
	if state.Services[0].State == nil || !state.Services[0].State.Manageable() || state.Services[1].State != nil {
		t.Fatalf("successful empty read and failed read must have distinct observations: %#v", state.Services)
	}
	var observation ObservationError
	if !errors.Is(err, failure) || !errors.As(err, &observation) || observation.ServiceName != "VPN" {
		t.Fatalf("error = %v", err)
	}
}

func (f fakeService) Name() string { return f.name }
func (f fakeService) PAC(context.Context) (networkservice.PACSetting, error) {
	f.system.mu.Lock()
	defer f.system.mu.Unlock()
	if err := f.system.observeErrs[f.name]; err != nil {
		return networkservice.PACSetting{}, err
	}
	for _, state := range f.system.states {
		if state.Name == f.name {
			return networkservice.PACSetting{URL: state.URL, Enabled: state.Enabled}, nil
		}
	}
	return networkservice.PACSetting{}, errors.New("missing")
}
func (f fakeService) SetPAC(_ context.Context, raw string) error {
	f.system.mu.Lock()
	defer f.system.mu.Unlock()
	if err := f.system.setErrs[f.name]; err != nil {
		return err
	}
	for i := range f.system.states {
		if f.system.states[i].Name == f.name {
			f.system.states[i].URL = raw
			f.system.states[i].Enabled = true
			if result, ok := f.system.setResults[f.name]; ok {
				f.system.states[i].URL = result.URL
				f.system.states[i].Enabled = result.Enabled
			}
			f.system.writes = append(f.system.writes, f.name+"="+raw)
			return nil
		}
	}
	return errors.New("missing")
}
func (f fakeService) DisablePAC(context.Context) error {
	f.system.mu.Lock()
	defer f.system.mu.Unlock()
	if err := f.system.disableErrs[f.name]; err != nil {
		return err
	}
	for i := range f.system.states {
		if f.system.states[i].Name == f.name {
			f.system.states[i].Enabled = false
			return nil
		}
	}
	return errors.New("missing")
}

func TestDeliverFreshlyDiscoversSafeServicesAndConsumesGenerations(t *testing.T) {
	system := &fakeSystem{states: []fakeServiceState{{Name: "Wi-Fi"}}}
	module := &SystemPAC{listServices: system.list}
	err := module.Deliver(context.Background(), "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	system.mu.Lock()
	system.states = append(system.states, fakeServiceState{Name: "USB"})
	system.mu.Unlock()
	err = module.Deliver(context.Background(), "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(system.writes[0], "v=1") {
		t.Fatalf("first publication = %q", system.writes[0])
	}
	if len(system.writes) != 3 {
		t.Fatalf("writes = %#v", system.writes)
	}
	if !strings.Contains(system.writes[2], "v=2") {
		t.Fatalf("second delivery URL = %q", system.writes[2])
	}
}

// A service can become foreign between deliveries; previous manageability must
// not authorize later writes, while disabled owned settings remain adoptable.
func TestDeliveryReassessesManageability(t *testing.T) {
	system := &fakeSystem{states: []fakeServiceState{
		{Name: "Wi-Fi", URL: "http://127.0.0.1/seamless-cors.pac"},
		{Name: "Corporate", URL: "http://corp/pac"},
	}}
	module := &SystemPAC{listServices: system.list}
	before, err := module.Inspect(context.Background())
	if err != nil || !before.Services[0].State.Manageable() || before.Services[1].State.Manageable() {
		t.Fatalf("observation = %#v, error = %v", before, err)
	}
	if err := module.Deliver(context.Background(), "127.0.0.1:8080"); err != nil {
		t.Fatal(err)
	}
	if !system.states[0].Enabled || len(system.writes) != 1 {
		t.Fatalf("disabled owned setting was not adopted: %#v, writes = %v", system.states, system.writes)
	}
	system.states[0].URL = "http://corp/pac"
	var noEligible NoEligibleServicesError
	if err := module.Deliver(context.Background(), "127.0.0.1:8080"); !errors.As(err, &noEligible) {
		t.Fatalf("error = %v", err)
	}
	if len(system.writes) != 1 || system.states[0].URL != "http://corp/pac" || system.states[1].Enabled {
		t.Fatalf("foreign settings changed: %#v, writes = %v", system.states, system.writes)
	}
	after, err := module.Inspect(context.Background())
	if err != nil || after.Services[0].State.Manageable() || !before.Services[0].State.Manageable() {
		t.Fatalf("manageability did not remain observation-local: before = %#v, after = %#v, error = %v", before, after, err)
	}
}

func TestDeliverPreservesForeignAndSuccessfulWritesOnPartialFailure(t *testing.T) {
	system := &fakeSystem{states: []fakeServiceState{
		{Name: "Corporate", URL: "http://corp/pac", Enabled: true},
		{Name: "Wi-Fi"},
		{Name: "VPN"},
	}, setErrs: map[string]error{"VPN": errors.New("denied")}}
	module := &SystemPAC{listServices: system.list}
	err := module.Deliver(context.Background(), "127.0.0.1:8080")
	var mutation MutationError
	if !errors.As(err, &mutation) || mutation.ServiceName != "VPN" {
		t.Fatalf("error = %v", err)
	}
	state, _ := module.Inspect(context.Background())
	if len(state.Services) != 3 {
		t.Fatalf("state = %#v", state)
	}
	if state.Services[0].State.Manageable() {
		t.Fatalf("foreign state = %#v", state.Services[0])
	}
	if !slices.Equal(system.writes, []string{"Wi-Fi=http://127.0.0.1:8080/seamless-cors.pac?v=1"}) {
		t.Fatalf("writes = %#v", system.writes)
	}
}

func TestObserveIsFreshAndOwnerlessNeverClaimsCurrentEndpoint(t *testing.T) {
	system := &fakeSystem{states: []fakeServiceState{{Name: "Wi-Fi", URL: "http://127.0.0.1:8080/seamless-cors.pac?v=8", Enabled: true}}}
	module := &SystemPAC{listServices: system.list}
	ownerless, err := module.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	current, err := module.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ownerless.RoutesEndpoint("") || !current.RoutesEndpoint("127.0.0.1:8080") {
		t.Fatalf("ownerless/current = %t/%t", ownerless.RoutesEndpoint(""), current.RoutesEndpoint("127.0.0.1:8080"))
	}
}

func TestCleanupPreservesForeignAndDisabledOwnedAndReportsUncertainty(t *testing.T) {
	system := &fakeSystem{states: []fakeServiceState{
		{Name: "Wi-Fi", URL: "http://127.0.0.1/seamless-cors.pac", Enabled: true},
		{Name: "Disabled", URL: "http://127.0.0.1/seamless-cors.pac", Enabled: false},
		{Name: "Corporate", URL: "http://corp/pac", Enabled: true},
		{Name: "VPN", URL: "http://127.0.0.1/seamless-cors.pac", Enabled: true},
	}, observeErrs: map[string]error{"VPN": errors.New("query failed")}}
	err := (&SystemPAC{listServices: system.list}).Cleanup(context.Background())
	var observation ObservationError
	if !errors.As(err, &observation) {
		t.Fatalf("error = %v", err)
	}
	if system.states[0].Enabled || !system.states[2].Enabled || !system.states[3].Enabled {
		t.Fatalf("states = %#v", system.states)
	}
}

func TestFailedDiscoveryStillConsumesDeliveryGeneration(t *testing.T) {
	system := &fakeSystem{discoveryErr: networkservice.ListError{Cause: errors.New("unavailable")}}
	module := &SystemPAC{listServices: system.list}
	err := module.Deliver(context.Background(), "127.0.0.1:8080")
	var discovery networkservice.ListError
	if !errors.As(err, &discovery) {
		t.Fatalf("first error = %v", err)
	}
	system.discoveryErr = nil
	system.states = []fakeServiceState{{Name: "Wi-Fi"}}
	err = module.Deliver(context.Background(), "127.0.0.1:8080")
	if err != nil || !strings.Contains(system.writes[0], "v=2") {
		t.Fatalf("second writes/error = %#v, %v", system.writes, err)
	}
}

func TestInvalidEndpointIsNotDeliveryAttempt(t *testing.T) {
	system := &fakeSystem{states: []fakeServiceState{{Name: "Wi-Fi"}}}
	module := &SystemPAC{listServices: system.list}
	err := module.Deliver(context.Background(), "")
	if err == nil || system.discoveries != 0 {
		t.Fatalf("error/discoveries = %v / %d", err, system.discoveries)
	}
	err = module.Deliver(context.Background(), "127.0.0.1:8080")
	if err != nil || !strings.Contains(system.writes[0], "v=1") {
		t.Fatalf("valid delivery writes/error = %#v / %v", system.writes, err)
	}
}

func TestPartialDiscoveryRetainsAndProcessesAvailableServices(t *testing.T) {
	system := &fakeSystem{states: []fakeServiceState{{Name: "Wi-Fi"}}, discoveryErr: networkservice.ListError{Cause: errors.New("one adapter failed")}}
	module := &SystemPAC{listServices: system.list}
	err := module.Deliver(context.Background(), "127.0.0.1:8080")
	state, _ := module.Inspect(context.Background())
	var discovery networkservice.ListError
	if !errors.As(err, &discovery) || len(state.Services) != 1 || !state.Services[0].State.Owned() {
		t.Fatalf("state/error = %#v / %v", state, err)
	}
}

func TestMutationOperationsRetainPerServiceObservationAndVerificationFailures(t *testing.T) {
	for _, operation := range []string{"deliver", "cleanup"} {
		t.Run(operation, func(t *testing.T) {
			wifiFailure := errors.New("Wi-Fi query failed")
			vpnFailure := errors.New("VPN query failed")
			system := &fakeSystem{
				states:      []fakeServiceState{{Name: "Wi-Fi"}, {Name: "VPN"}},
				observeErrs: map[string]error{"Wi-Fi": wifiFailure, "VPN": vpnFailure},
			}
			module := &SystemPAC{listServices: system.list}
			var err error
			if operation == "deliver" {
				err = module.Deliver(context.Background(), "127.0.0.1:8080")
			} else {
				err = module.Cleanup(context.Background())
			}
			var observations, verifications []string
			var visit func(error)
			visit = func(err error) {
				if joined, ok := err.(interface{ Unwrap() []error }); ok {
					for _, child := range joined.Unwrap() {
						visit(child)
					}
					return
				}
				switch err := err.(type) {
				case ObservationError:
					observations = append(observations, err.ServiceName)
				case VerificationError:
					verifications = append(verifications, err.ServiceName)
					if err.Cause != system.observeErrs[err.ServiceName] {
						t.Fatalf("verification cause = %v", err.Cause)
					}
				case NoEligibleServicesError:
				default:
					t.Fatalf("unexpected error: %v", err)
				}
			}
			visit(err)
			want := []string{"Wi-Fi", "VPN"}
			if !slices.Equal(observations, want) || !slices.Equal(verifications, want) || !errors.Is(err, wifiFailure) || !errors.Is(err, vpnFailure) {
				t.Fatalf("observations/ verifications/ error = %v / %v / %v", observations, verifications, err)
			}
		})
	}
}

func TestObservationRoutesEndpointRequiresAnEnabledOwnedMatchingObservation(t *testing.T) {
	for _, test := range []struct {
		name  string
		state *PACState
		want  bool
	}{
		{name: "unobservable"},
		{name: "empty", state: &PACState{}},
		{name: "disabled", state: &PACState{URL: "http://127.0.0.1:8080/seamless-cors.pac?v=1"}},
		{name: "foreign", state: &PACState{URL: "http://127.0.0.1:8080/other.pac", Enabled: true}},
		{name: "old endpoint", state: &PACState{URL: "http://127.0.0.1:8081/seamless-cors.pac?v=1", Enabled: true}},
		{name: "current endpoint", state: &PACState{URL: "http://127.0.0.1:8080/seamless-cors.pac?v=2", Enabled: true}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := Observation{Services: []ServiceObservation{{Name: "Unavailable"}, {Name: "Wi-Fi", State: test.state}}}
			if got := state.RoutesEndpoint("127.0.0.1:8080"); got != test.want {
				t.Fatalf("RoutesEndpoint = %t, want %t", got, test.want)
			}
		})
	}
}

func TestDeliverRejectsUnverifiedPublicationDespiteSuccessfulWrite(t *testing.T) {
	const endpoint = "127.0.0.1:8080"
	const intended = "http://127.0.0.1:8080/seamless-cors.pac?v=1"
	for _, observed := range []PACState{
		{URL: "http://127.0.0.1:8080/seamless-cors.pac?v=0", Enabled: true},
		{URL: intended, Enabled: false},
		{URL: "http://corp/pac", Enabled: true},
	} {
		t.Run(fmt.Sprintf("%s/enabled=%t", observed.URL, observed.Enabled), func(t *testing.T) {
			system := &fakeSystem{
				states:     []fakeServiceState{{Name: "Wi-Fi"}, {Name: "USB"}},
				setResults: map[string]PACState{"Wi-Fi": observed},
			}
			module := &SystemPAC{listServices: system.list}
			err := module.Deliver(context.Background(), endpoint)
			var mismatch DeliveryMismatchError
			if !errors.As(err, &mismatch) || mismatch.ServiceName != "Wi-Fi" || mismatch.PACURL != intended || mismatch.Observed != observed {
				t.Fatalf("delivery error = %#v / %v", mismatch, err)
			}
			facts, err := module.Inspect(context.Background())
			if err != nil || *facts.Services[1].State != (PACState{URL: intended, Enabled: true}) {
				t.Fatalf("successful service was rolled back: %#v / %v", facts, err)
			}
		})
	}
}

func TestDeliverRequiresAnEligibleService(t *testing.T) {
	for _, states := range [][]fakeServiceState{
		nil,
		{{Name: "Corporate", URL: "http://corp/pac", Enabled: true}},
	} {
		system := &fakeSystem{states: states}
		err := (&SystemPAC{listServices: system.list}).Deliver(context.Background(), "127.0.0.1:8080")
		var excluded NoEligibleServicesError
		if !errors.As(err, &excluded) || len(system.writes) != 0 {
			t.Fatalf("error/writes = %v / %#v", err, system.writes)
		}
	}
}

func TestCleanupReportsOwnedResidueWithoutChangingForeignSettings(t *testing.T) {
	failure := errors.New("denied")
	system := &fakeSystem{
		states: []fakeServiceState{
			{Name: "Wi-Fi", URL: "http://127.0.0.1/seamless-cors.pac", Enabled: true},
			{Name: "Corporate", URL: "http://corp/pac", Enabled: true},
		},
		disableErrs: map[string]error{"Wi-Fi": failure},
	}
	err := (&SystemPAC{listServices: system.list}).Cleanup(context.Background())
	var residue ResidueError
	if !errors.Is(err, failure) || !errors.As(err, &residue) || !slices.Equal(residue.Services, []string{"Wi-Fi"}) || !system.states[1].Enabled {
		t.Fatalf("cleanup error/states = %v / %#v", err, system.states)
	}
}
