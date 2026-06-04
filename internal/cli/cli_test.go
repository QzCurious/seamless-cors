package cli

import (
	"strings"
	"testing"
)

func TestParseOverridesSupportsCurrentFlags(t *testing.T) {
	overrides, err := parseOverrides([]string{"--ca-trusted", "--domain-list=domains.txt", "--log-level=debug"})
	if err != nil {
		t.Fatal(err)
	}
	if !overrides.CATrustedSet || !overrides.CATrusted {
		t.Fatalf("ca-trusted override = set:%t value:%t", overrides.CATrustedSet, overrides.CATrusted)
	}
	if overrides.DomainList != "domains.txt" {
		t.Fatalf("domain-list override = %q", overrides.DomainList)
	}
	if overrides.LogLevel != "debug" {
		t.Fatalf("log-level override = %q", overrides.LogLevel)
	}
}

func TestParseOverridesRejectsRemovedFlagsAsUnknown(t *testing.T) {
	_, err := parseOverrides([]string{"--managed-system-proxy=false"})
	if err == nil {
		t.Fatal("expected removed flag to be unknown")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("error = %v", err)
	}
}
