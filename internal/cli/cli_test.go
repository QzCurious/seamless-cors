package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"seamless-cors/internal/config"
)

func TestParseOverridesSupportsCurrentFlags(t *testing.T) {
	overrides, err := parseOverrides([]string{"--ca-trusted", "--domain-list=domains.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !overrides.CATrustedSet || !overrides.CATrusted {
		t.Fatalf("ca-trusted override = set:%t value:%t", overrides.CATrustedSet, overrides.CATrusted)
	}
	if overrides.DomainList != "domains.txt" {
		t.Fatalf("domain-list override = %q", overrides.DomainList)
	}
}

func TestParseOverridesRejectsUnknownFlags(t *testing.T) {
	_, err := parseOverrides([]string{"--log-level=debug"})
	if err == nil {
		t.Fatal("expected removed flag to be unknown")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunPrintsStartCommandErrors(t *testing.T) {
	wantErr := errors.New("start failed")
	var stderr bytes.Buffer

	err := run([]string{"start"}, io.Discard, &stderr, commandHandlers{
		start: func(io.Writer, io.Writer, config.Overrides) error {
			return wantErr
		},
	})

	if !errors.Is(err, wantErr) {
		t.Fatalf("run error = %v", err)
	}
	if got := stderr.String(); !strings.Contains(got, wantErr.Error()) {
		t.Fatalf("stderr = %q", got)
	}
}
