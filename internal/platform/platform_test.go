package platform

import "testing"

func TestNoopAdapterFailsManagedPACInstallationClearly(t *testing.T) {
	if err := (NoopAdapter{}).InstallPAC("http://127.0.0.1:8079/proxy.pac"); err == nil {
		t.Fatal("expected unsupported managed PAC installation to fail")
	}
}
