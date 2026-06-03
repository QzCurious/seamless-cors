package cli

import "testing"

func TestParseOverridesSupportsBooleanShorthandAndExplicitFalse(t *testing.T) {
	overrides, err := parseOverrides([]string{"--ca-trusted", "--managed-system-proxy=false"})
	if err != nil {
		t.Fatal(err)
	}
	if !overrides.CATrustedSet || !overrides.CATrusted {
		t.Fatalf("ca-trusted override = set:%t value:%t", overrides.CATrustedSet, overrides.CATrusted)
	}
	if !overrides.ManagedSystemProxySet || overrides.ManagedSystemProxy {
		t.Fatalf("managed-system-proxy override = set:%t value:%t", overrides.ManagedSystemProxySet, overrides.ManagedSystemProxy)
	}
}
