package gatewayfacade

import (
	"context"
	"errors"
	"fmt"

	"seamless-cors/internal/cleanup"
	"seamless-cors/internal/config"
	"seamless-cors/internal/gatewayruntime"
	"seamless-cors/internal/platform"
	"seamless-cors/internal/userca"
)

type gatewayActivation struct {
	facade *Facade
}

func (a gatewayActivation) Start(ctx context.Context, request StartRequest) (StartResult, error) {
	assessment, err := a.facade.assessPACReplacement()
	if err != nil {
		return StartResult{}, err
	}
	if assessment.required && (request.PACReplacementConsent == nil || !request.PACReplacementConsent.Accepted) {
		return StartResult{
			Kind:                  StartResultConsentRequired,
			PACReplacementConsent: assessment.detail,
		}, nil
	}
	if len(assessment.serviceSet) == 0 {
		return StartResult{}, fmt.Errorf("managed PAC service set is empty")
	}

	installedCAChanged := false
	var authority *userca.Authority
	source, live, err := liveconfigLoadOrBootstrap()
	if err != nil {
		return StartResult{}, err
	}
	if live.CATrusted() {
		caDir, err := config.CADir()
		if err != nil {
			return StartResult{}, err
		}
		var ensureResult userca.EnsureResult
		authority, ensureResult, err = userca.Ensure(caDir, a.facade.adapter)
		if err != nil {
			if errors.Is(err, platform.ErrTrustApprovalDenied) {
				return StartResult{Kind: StartResultPlatformApprovalDenied}, nil
			}
			return StartResult{}, err
		}
		installedCAChanged = ensureResult.Changed
	}

	engine, err := gatewayruntime.New(source, live)
	if err != nil {
		return StartResult{}, err
	}
	cleanupEngine := true
	defer func() {
		if cleanupEngine {
			_ = engine.Close()
		}
	}()

	if err := engine.SetAuthority(authority); err != nil {
		return StartResult{}, err
	}

	pacURL := engine.PACURL()
	managedServices, err := a.facade.adapter.InstallPAC(pacURL, assessment.serviceSet)
	if err != nil {
		return StartResult{}, errors.Join(err, a.cleanupFailedPACInstall())
	}
	if len(managedServices) == 0 {
		return StartResult{}, errors.Join(
			fmt.Errorf("managed PAC install updated no services"),
			a.cleanupFailedPACInstall(),
		)
	}
	managedServices = sortedStrings(managedServices)
	managedServiceSet := sortedStrings(assessment.serviceSet)

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	active := &activeRuntime{
		engine:             engine,
		live:               live,
		cancel:             cancel,
		done:               done,
		managedPACURL:      pacURL,
		managedPACServices: managedServiceSet,
	}
	a.facade.mu.Lock()
	a.facade.runtime = active
	a.facade.mu.Unlock()
	cleanupEngine = false

	go a.facade.watchManagedPACLease(runCtx, active)
	go func() {
		err := engine.Serve(runCtx)
		done <- err
		if err != nil {
			select {
			case a.facade.fatal <- err:
			default:
			}
		}
	}()

	return StartResult{
		Kind: StartResultStarted,
		Guidance: &StartGuidanceDetail{
			ConfigPath:         live.ConfigPath(),
			DomainListPath:     live.DomainListPath(),
			ManagedPACActive:   true,
			ManagedPACServices: managedServices,
			CATrusted:          live.CATrusted(),
			InstalledCAChanged: installedCAChanged,
		},
	}, nil
}

func (a gatewayActivation) cleanupFailedPACInstall() error {
	if err := cleanup.Clean(a.facade.runtimeDir, a.facade.adapter); err != nil {
		return fmt.Errorf("cleanup after failed managed PAC install failed: %w", err)
	}
	return nil
}
