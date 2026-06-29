package cleanup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"seamless-cors/internal/gatewaycoord"
	"seamless-cors/internal/platform"
)

type Inspector interface {
	CurrentPACState() ([]platform.PACServiceState, error)
}

type Cleaner interface {
	ClearOwnedPAC() error
}

type Adapter interface {
	Inspector
	Cleaner
}

type Inspection struct {
	StaleGatewayStateCache bool
	GatewayStateCache      bool
	OwnedPAC               bool
}

func Inspect(runtimeDir string, adapter Inspector, staleRuntimeState bool) Inspection {
	inspection := Inspection{StaleGatewayStateCache: staleRuntimeState}
	if _, err := os.Stat(filepath.Join(runtimeDir, gatewaycoord.StateFileName)); err == nil {
		inspection.GatewayStateCache = true
	}
	if states, err := adapter.CurrentPACState(); err == nil && platform.HasOwnedPACState(states) {
		inspection.OwnedPAC = true
	}
	return inspection
}

func (i Inspection) Needed() bool {
	return i.StaleGatewayStateCache || i.GatewayStateCache || i.OwnedPAC
}

func Clean(runtimeDir string, adapter Cleaner) error {
	return clean(runtimeDir, adapter, nil)
}

func CleanOwned(runtimeDir string, adapter Cleaner, cache gatewaycoord.GatewayStateCache) error {
	return clean(runtimeDir, adapter, &cache)
}

func clean(runtimeDir string, adapter Cleaner, ownedCache *gatewaycoord.GatewayStateCache) error {
	var errs []error
	if err := adapter.ClearOwnedPAC(); err != nil {
		errs = append(errs, fmt.Errorf("managed PAC cleanup failed: %w", err))
	}
	if ownedCache != nil && len(errs) > 0 {
		return Error{Causes: errs}
	}
	coord := gatewaycoord.New(runtimeDir)
	var err error
	if ownedCache == nil {
		err = coord.Remove()
	} else {
		err = coord.RemoveOwned(*ownedCache)
	}
	if err != nil {
		errs = append(errs, fmt.Errorf("gateway state cache cleanup failed: %w", err))
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
