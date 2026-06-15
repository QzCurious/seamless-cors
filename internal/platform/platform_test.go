package platform

import "testing"

func TestManagedPACFootprintMatchesOnlyLoopbackHTTPPACFilename(t *testing.T) {
	tests := map[string]bool{
		"http://127.0.0.1:8079/seamless-cors.pac":               true,
		"http://localhost:8079/nested/seamless-cors.pac":        true,
		"http://[::1]:8079/seamless-cors.pac":                   true,
		"http://127.0.0.1:8079/not-seamless-cors.pac":           false,
		"http://127.0.0.1:8079/seamless-cors.pac.backup":        false,
		"https://127.0.0.1:8079/seamless-cors.pac":              false,
		"http://proxy.example.test/seamless-cors.pac":           false,
		"http://proxy.example.test/corporate-seamless-cors.pac": false,
	}
	for raw, want := range tests {
		if got := IsManagedPACFootprint(raw); got != want {
			t.Fatalf("IsManagedPACFootprint(%q) = %t, want %t", raw, got, want)
		}
	}
}

func TestOwnedPACStateIgnoresDisabledFootprints(t *testing.T) {
	states := []PACServiceState{{
		Name:    "Wi-Fi",
		URL:     "http://127.0.0.1:8079/seamless-cors.pac",
		Enabled: false,
	}}
	if HasOwnedPACState(states) {
		t.Fatal("disabled owned PAC footprint should not require cleanup")
	}
}
