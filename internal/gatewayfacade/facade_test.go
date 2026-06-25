package gatewayfacade

import (
	"testing"

	"seamless-cors/internal/cleanup"
	"seamless-cors/internal/platform"
)

func TestManagedPACLeaseAllowsMissingSelectedService(t *testing.T) {
	scope := cleanup.ManagedPACScope{
		URL:      "http://127.0.0.1:49152/seamless-cors.pac?v=1",
		Services: []string{"Wi-Fi", "Ethernet"},
	}
	states := []platform.PACServiceState{{
		Name:    "Wi-Fi",
		URL:     scope.URL,
		Enabled: true,
	}}

	if !managedPACLeaseHeld(states, scope) {
		t.Fatal("missing selected service should not lose the managed PAC lease")
	}
}

func TestManagedPACLeaseRejectsVisibleChangedSelectedService(t *testing.T) {
	scope := cleanup.ManagedPACScope{
		URL:      "http://127.0.0.1:49152/seamless-cors.pac?v=1",
		Services: []string{"Wi-Fi", "Ethernet"},
	}
	states := []platform.PACServiceState{
		{Name: "Wi-Fi", URL: scope.URL, Enabled: true},
		{Name: "Ethernet", URL: "http://corp.example/proxy.pac", Enabled: true},
	}

	if managedPACLeaseHeld(states, scope) {
		t.Fatal("visible selected service with replaced PAC should lose the managed PAC lease")
	}
}
