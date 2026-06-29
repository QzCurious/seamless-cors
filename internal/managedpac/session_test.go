package managedpac

import (
	"errors"
	"strings"
	"testing"

	"seamless-cors/internal/platform"
)

type fakeAdapter struct {
	pacStates    []platform.PACServiceState
	installedURL string
	installed    []string
	installOut   []string
	refreshes    []string
	installErr   error
	refreshErr   error
	currentErr   error
}

func (f *fakeAdapter) InstallPAC(url string, services []string) ([]string, error) {
	f.installedURL = url
	f.installed = append([]string(nil), services...)
	if f.installOut != nil {
		return append([]string(nil), f.installOut...), f.installErr
	}
	return f.installed, f.installErr
}

func (f *fakeAdapter) RefreshPAC(url string, services []string) error {
	f.refreshes = append(f.refreshes, url)
	for idx := range f.pacStates {
		for _, service := range services {
			if f.pacStates[idx].Name == service {
				f.pacStates[idx].URL = url
				f.pacStates[idx].Enabled = true
			}
		}
	}
	return f.refreshErr
}

func (f *fakeAdapter) CurrentPACState() ([]platform.PACServiceState, error) {
	if f.currentErr != nil {
		return nil, f.currentErr
	}
	return append([]platform.PACServiceState(nil), f.pacStates...), nil
}

func TestAssessPropagatesCurrentPACStateError(t *testing.T) {
	wantErr := errors.New("inspection denied")

	_, err := Assess(&fakeAdapter{currentErr: wantErr})

	if !errors.Is(err, wantErr) {
		t.Fatalf("assessment error = %v", err)
	}
}

func TestAssessSelectsServicesAndReportsReplacement(t *testing.T) {
	assessment, err := Assess(&fakeAdapter{
		pacStates: []platform.PACServiceState{
			{Name: "USB", URL: "", Enabled: false},
			{Name: "Wi-Fi", URL: "http://corp.example/proxy.pac", Enabled: true},
			{Name: "Ethernet", URL: "http://127.0.0.1:49152/seamless-cors.pac?v=1", Enabled: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if !assessment.ReplacementRequired {
		t.Fatal("foreign PAC state should require replacement")
	}
	wantServices := "Ethernet,USB,Wi-Fi"
	if got := strings.Join(assessment.ServiceSet, ","); got != wantServices {
		t.Fatalf("service set = %s, want %s", got, wantServices)
	}
	if assessment.States[0].ServiceName != "Ethernet" || assessment.States[0].Ownership != OwnershipOwned {
		t.Fatalf("states not sorted/classified: %#v", assessment.States)
	}
}

func TestStartInstallsInitialURLAndKeepsSelectedServices(t *testing.T) {
	adapter := &fakeAdapter{}

	session, result, err := Start(adapter, []string{"Wi-Fi", "Ethernet"}, "http://127.0.0.1:1/seamless-cors.pac?v=1")
	if err != nil {
		t.Fatal(err)
	}

	if session.CurrentURL() != "http://127.0.0.1:1/seamless-cors.pac?v=1" {
		t.Fatalf("current URL = %q", session.CurrentURL())
	}
	if got := strings.Join(session.Services(), ","); got != "Ethernet,Wi-Fi" {
		t.Fatalf("services = %s", got)
	}
	if got := strings.Join(result.InstalledServices, ","); got != "Ethernet,Wi-Fi" {
		t.Fatalf("installed services = %s", got)
	}
}

func TestStartRejectsEmptyServiceSet(t *testing.T) {
	_, _, err := Start(&fakeAdapter{}, nil, "http://127.0.0.1:1/seamless-cors.pac?v=1")

	if err == nil || !strings.Contains(err.Error(), "managed PAC service set is empty") {
		t.Fatalf("start error = %v", err)
	}
}

func TestStartRejectsZeroInstalledServices(t *testing.T) {
	adapter := &fakeAdapter{installOut: []string{}}

	_, _, err := Start(adapter, []string{"Wi-Fi"}, "http://127.0.0.1:1/seamless-cors.pac?v=1")

	if err == nil || !strings.Contains(err.Error(), "managed PAC install updated no services") {
		t.Fatalf("start error = %v", err)
	}
}

func TestRefreshCommitsCurrentURLOnlyAfterSuccess(t *testing.T) {
	adapter := &fakeAdapter{refreshErr: errors.New("refresh denied")}
	session, _, err := Start(adapter, []string{"Wi-Fi"}, "http://127.0.0.1:1/seamless-cors.pac?v=1")
	if err != nil {
		t.Fatal(err)
	}

	err = session.Refresh("http://127.0.0.1:1/seamless-cors.pac?v=2")
	if err == nil {
		t.Fatal("expected refresh error")
	}
	var refreshErr RefreshError
	if !errors.As(err, &refreshErr) {
		t.Fatalf("error type = %T", err)
	}
	if session.CurrentURL() != "http://127.0.0.1:1/seamless-cors.pac?v=1" {
		t.Fatalf("current URL changed after failed refresh: %q", session.CurrentURL())
	}
	if session.AttemptedURL() != "http://127.0.0.1:1/seamless-cors.pac?v=2" {
		t.Fatalf("attempted URL = %q", session.AttemptedURL())
	}

	adapter.refreshErr = nil
	if err := session.Refresh("http://127.0.0.1:1/seamless-cors.pac?v=3"); err != nil {
		t.Fatal(err)
	}
	if session.CurrentURL() != "http://127.0.0.1:1/seamless-cors.pac?v=3" || session.AttemptedURL() != "" {
		t.Fatalf("refresh state current=%q attempted=%q", session.CurrentURL(), session.AttemptedURL())
	}
}

func TestRequireLeaseAllowsMissingSelectedService(t *testing.T) {
	url := "http://127.0.0.1:49152/seamless-cors.pac?v=1"
	adapter := &fakeAdapter{
		pacStates: []platform.PACServiceState{{Name: "Wi-Fi", URL: url, Enabled: true}},
	}
	session, _, err := Start(adapter, []string{"Wi-Fi", "Ethernet"}, url)
	if err != nil {
		t.Fatal(err)
	}

	if err := session.RequireLease(); err != nil {
		t.Fatalf("missing selected service should not lose the managed PAC lease: %v", err)
	}
}

func TestRequireLeaseRejectsVisibleChangedSelectedService(t *testing.T) {
	url := "http://127.0.0.1:49152/seamless-cors.pac?v=1"
	adapter := &fakeAdapter{
		pacStates: []platform.PACServiceState{
			{Name: "Wi-Fi", URL: url, Enabled: true},
			{Name: "Ethernet", URL: "http://corp.example/proxy.pac", Enabled: true},
		},
	}
	session, _, err := Start(adapter, []string{"Wi-Fi", "Ethernet"}, url)
	if err != nil {
		t.Fatal(err)
	}

	if !errors.Is(session.RequireLease(), ErrManagedPACLeaseLost) {
		t.Fatal("visible selected service with replaced PAC should lose the managed PAC lease")
	}
}

func TestRequireLeaseWrapsInspectionFailure(t *testing.T) {
	wantErr := errors.New("inspection denied")
	session, _, err := Start(&fakeAdapter{}, []string{"Wi-Fi"}, "http://127.0.0.1:49152/seamless-cors.pac?v=1")
	if err != nil {
		t.Fatal(err)
	}
	session.adapter = &fakeAdapter{currentErr: wantErr}

	err = session.RequireLease()

	if !errors.Is(err, wantErr) {
		t.Fatalf("lease error = %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "managed PAC lease inspection failed") {
		t.Fatalf("lease error missing context: %v", err)
	}
}
