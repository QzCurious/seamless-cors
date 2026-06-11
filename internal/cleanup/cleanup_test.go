package cleanup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"seamless-cors/internal/platform"
)

type fakeAdapter struct {
	pacStates []platform.PACServiceState
	ca        bool
	clearErr  error
	cleared   int
}

func (f *fakeAdapter) CurrentPACState() ([]platform.PACServiceState, error) {
	return f.pacStates, nil
}

func (f *fakeAdapter) ClearOwnedPAC() error {
	f.cleared++
	return f.clearErr
}

func TestInspectReportsOwnedFootprintWithoutMutating(t *testing.T) {
	runtimeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(runtimeDir, "control-state.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter := &fakeAdapter{
		pacStates: []platform.PACServiceState{{
			Name:    "Wi-Fi",
			URL:     "http://127.0.0.1:8079/seamless-cors.pac",
			Enabled: true,
		}},
		ca: true,
	}

	inspection := Inspect(runtimeDir, adapter, true)

	if !inspection.Needed() {
		t.Fatal("owned footprint should require cleanup")
	}
	if !inspection.StaleRuntimeState || !inspection.OwnedPAC {
		t.Fatalf("inspection = %#v", inspection)
	}
	if got := strings.Join(inspection.RuntimeFiles, ","); got != "control-state.json" {
		t.Fatalf("runtime files = %q", got)
	}
	if adapter.cleared != 0 {
		t.Fatalf("inspect mutated adapter: PAC=%d", adapter.cleared)
	}
}

func TestCleanRemovesOwnedRuntimeFilesAndCallsAdapters(t *testing.T) {
	runtimeDir := t.TempDir()
	for _, name := range []string{"control-state.json"} {
		if err := os.WriteFile(filepath.Join(runtimeDir, name), []byte("owned"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	adapter := &fakeAdapter{}

	if err := Clean(runtimeDir, adapter); err != nil {
		t.Fatal(err)
	}

	if adapter.cleared != 1 {
		t.Fatalf("cleanup calls: PAC=%d", adapter.cleared)
	}
	for _, name := range ownedRuntimeFiles {
		if _, err := os.Stat(filepath.Join(runtimeDir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s still exists: %v", name, err)
		}
	}
}

func TestCleanGroupsFailuresWithRetryGuidance(t *testing.T) {
	adapter := &fakeAdapter{
		clearErr: errors.New("pac denied"),
	}

	err := Clean(t.TempDir(), adapter)
	if err == nil {
		t.Fatal("expected cleanup error")
	}
	var cleanupErr Error
	if !errors.As(err, &cleanupErr) {
		t.Fatalf("error type = %T", err)
	}
	got := err.Error()
	for _, want := range []string{
		"managed PAC cleanup failed: pac denied",
		"Cleanup failed; resolve the OS or permission problem, then run `seamless-cors stop` again.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("cleanup error missing %q:\n%s", want, got)
		}
	}
}
