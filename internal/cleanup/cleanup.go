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
	ClearPACForServices(url string, services []string) error
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
	return clean(runtimeDir, adapter, nil, nil)
}

func CleanOwned(runtimeDir string, adapter Cleaner, cache gatewaycoord.GatewayStateCache) error {
	return clean(runtimeDir, adapter, &cache, nil)
}

type ManagedPACScope struct {
	URL      string
	URLs     []string
	Services []string
}

func CleanOwnedScoped(runtimeDir string, adapter Cleaner, cache gatewaycoord.GatewayStateCache, scope ManagedPACScope) error {
	return clean(runtimeDir, adapter, &cache, &scope)
}

func clean(runtimeDir string, adapter Cleaner, ownedCache *gatewaycoord.GatewayStateCache, pacScope *ManagedPACScope) error {
	var errs []error
	if pacScope != nil {
		for _, url := range pacScope.urls() {
			if err := adapter.ClearPACForServices(url, pacScope.Services); err != nil {
				errs = append(errs, fmt.Errorf("managed PAC cleanup failed: %w", err))
			}
		}
	} else {
		if err := adapter.ClearOwnedPAC(); err != nil {
			errs = append(errs, fmt.Errorf("managed PAC cleanup failed: %w", err))
		}
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

func (s ManagedPACScope) urls() []string {
	seen := map[string]struct{}{}
	var urls []string
	for _, url := range append([]string{s.URL}, s.URLs...) {
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		urls = append(urls, url)
	}
	return urls
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
