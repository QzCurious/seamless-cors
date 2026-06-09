package cleanup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"seamless-cors/internal/platform"
)

var ownedRuntimeFiles = []string{"ephemeral-ca.pem", "ephemeral-ca-key.pem", "control-state.json"}

type Inspector interface {
	CurrentPACState() ([]platform.PACServiceState, error)
	HasCAFootprint() (bool, error)
}

type Cleaner interface {
	ClearOwnedPAC() error
	CleanupCAFootprint() error
}

type Adapter interface {
	Inspector
	Cleaner
}

type Inspection struct {
	StaleRuntimeState bool
	RuntimeFiles      []string
	OwnedPAC          bool
	OwnedCA           bool
}

func Inspect(runtimeDir string, adapter Inspector, staleRuntimeState bool) Inspection {
	inspection := Inspection{StaleRuntimeState: staleRuntimeState}
	for _, name := range ownedRuntimeFiles {
		if _, err := os.Stat(filepath.Join(runtimeDir, name)); err == nil {
			inspection.RuntimeFiles = append(inspection.RuntimeFiles, name)
		}
	}
	if states, err := adapter.CurrentPACState(); err == nil && platform.HasOwnedPACState(states) {
		inspection.OwnedPAC = true
	}
	if hasCA, err := adapter.HasCAFootprint(); err == nil && hasCA {
		inspection.OwnedCA = true
	}
	return inspection
}

func (i Inspection) Needed() bool {
	return i.StaleRuntimeState || len(i.RuntimeFiles) > 0 || i.OwnedPAC || i.OwnedCA
}

func Clean(runtimeDir string, adapter Cleaner) error {
	var errs []error
	if err := adapter.ClearOwnedPAC(); err != nil {
		errs = append(errs, fmt.Errorf("managed PAC cleanup failed: %w", err))
	}
	if err := adapter.CleanupCAFootprint(); err != nil {
		errs = append(errs, fmt.Errorf("CA trust cleanup failed: %w", err))
	}
	for _, name := range ownedRuntimeFiles {
		err := os.Remove(filepath.Join(runtimeDir, name))
		if err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("runtime file cleanup failed for %s: %w", name, err))
		}
	}
	if len(errs) > 0 {
		return Error{Causes: errs}
	}
	return nil
}

type Error struct {
	Causes []error
}

func (e Error) Error() string {
	var parts []string
	for _, err := range e.Causes {
		parts = append(parts, err.Error())
	}
	return strings.Join(parts, "; ") + "\nCleanup failed; resolve the OS or permission problem, then run `seamless-cors stop` again."
}
