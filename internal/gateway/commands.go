package gateway

import (
	"context"

	"github.com/QzCurious/seamless-cors/internal/systempac"
)

// Stop discovers and stops the live owner, or cleans the ownerless Gateway
// Footprint locally when no owner can be reached.
func Stop(ctx context.Context) (StopResult, error) {
	return stop(ctx, openSystemPAC())
}

func stop(ctx context.Context, pac systempac.Module) (StopResult, error) {
	if err := ctx.Err(); err != nil {
		return StopResult{}, err
	}
	target, err := discover()
	if err != nil {
		return StopResult{}, err
	}
	// Forward to the initialized foreground process.
	if target.kind == targetActive {
		result, err := target.client.Stop(ctx)
		if err != nil {
			return StopResult{}, err
		}
		if result.Kind == StopResultStopped {
			waitForStop(target.cache)
		}
		return result, nil
	}

	// Offline cleanup holds the same lock as startup and CA work.
	coord, err := openCoordinator()
	if err != nil {
		return StopResult{}, err
	}
	lock, acquired, err := coord.TryAcquireOwnerLock()
	if err != nil {
		return StopResult{}, err
	}
	if !acquired {
		return StopResult{}, &instanceBusyError{}
	}
	defer lock.Release()
	report, failures := cleanGatewayFootprint(ctx, pac, coord, nil)
	cleanupFulfillment := CommandFulfilled
	if len(failures) > 0 {
		cleanupFulfillment = CommandUnfulfilled
	}
	return StopResult{Kind: StopResultNotRunning, CleanupFulfillment: cleanupFulfillment, SystemPACCleanup: report, CleanupFailures: failures}, nil
}

// Status returns live owner status when available and otherwise
// inspects local Gateway coordination and OS-managed state.
func Status(ctx context.Context) (StatusResult, error) {
	return status(ctx, openSystemPAC(), nil)
}

func status(ctx context.Context, pac systempac.Module, ca userCAModule) (StatusResult, error) {
	if err := ctx.Err(); err != nil {
		return StatusResult{}, err
	}
	target, err := discover()
	if err != nil {
		return StatusResult{}, err
	}
	if target.kind == targetActive {
		return target.client.Status(ctx)
	}
	if ca == nil {
		ca, err = openSystemUserCA()
		if err != nil {
			return StatusResult{}, err
		}
	}
	coord, err := openCoordinator()
	if err != nil {
		return StatusResult{}, err
	}
	lock, acquired, err := coord.TryAcquireOwnerLock()
	if err != nil {
		return StatusResult{}, err
	}
	if !acquired {
		retry, rediscoverErr := discover()
		if rediscoverErr != nil {
			return StatusResult{}, rediscoverErr
		}
		if retry.kind == targetActive {
			return retry.client.Status(ctx)
		}
		return StatusResult{Kind: StatusResultOwnerTransition}, nil
	}
	defer lock.Release()
	lifecycle := newLifecycle(pac, ca, coord)
	lifecycle.userCAState, lifecycle.userCAAssessmentErr = ca.Inspect(ctx)
	return lifecycle.Status(ctx, target.kind == targetStale)
}

// InstallCA ensures the Installed User CA through the live owner when one is
// available, and locally otherwise.
func InstallCA(ctx context.Context) (InstallResult, error) {
	return installCA(ctx, nil)
}

func installCA(ctx context.Context, ca userCAModule) (InstallResult, error) {
	if err := ctx.Err(); err != nil {
		return InstallResult{}, err
	}
	target, err := discover()
	if err != nil {
		return InstallResult{}, err
	}
	if target.kind == targetActive {
		return target.client.Install(ctx)
	}
	lock, routed, err := lockForLocalCommand()
	if routed != nil {
		return routed.Install(ctx)
	}
	if _, busy := err.(*instanceBusyError); busy {
		return InstallResult{Kind: InstallResultOwnerTransition}, nil
	}
	if err != nil {
		return InstallResult{}, err
	}
	defer lock.Release()
	if ca == nil {
		ca, err = openSystemUserCA()
		if err != nil {
			return InstallResult{}, err
		}
	}
	current, err := ca.Install(ctx)
	if err != nil {
		return InstallResult{}, err
	}
	return InstallResult{Kind: InstallResultInstalled, InstalledCAExpires: current.ExpiresAt}, nil
}

// UninstallCA removes the Installed User CA through the live owner when one is
// available, and locally otherwise.
func UninstallCA(ctx context.Context, request UninstallRequest) (UninstallResult, error) {
	return uninstallCA(ctx, nil, request)
}

func uninstallCA(ctx context.Context, ca userCAModule, request UninstallRequest) (UninstallResult, error) {
	if err := ctx.Err(); err != nil {
		return UninstallResult{}, err
	}
	target, err := discover()
	if err != nil {
		return UninstallResult{}, err
	}
	if target.kind == targetActive {
		return target.client.Uninstall(ctx, request)
	}
	lock, routed, err := lockForLocalCommand()
	if routed != nil {
		return routed.Uninstall(ctx, request)
	}
	if _, busy := err.(*instanceBusyError); busy {
		return UninstallResult{Kind: UninstallResultOwnerTransition}, nil
	}
	if err != nil {
		return UninstallResult{}, err
	}
	defer lock.Release()
	if ca == nil {
		ca, err = openSystemUserCA()
		if err != nil {
			return UninstallResult{}, err
		}
	}
	if err := ca.Uninstall(ctx); err != nil {
		return UninstallResult{Kind: UninstallResultIncomplete, CleanupIssue: &UserCACleanupIssue{
			Cause: err.Error(), Action: "Run `seamless-cors uninstall` again.",
		}}, nil
	}
	return UninstallResult{Kind: UninstallResultUninstalled}, nil
}

// lockForLocalCommand excludes startup and other offline work. If a Gateway
// won the launch race, rediscover it once so the command can be forwarded.
func lockForLocalCommand() (*ownerLock, *client, error) {
	coord, err := openCoordinator()
	if err != nil {
		return nil, nil, err
	}
	lock, acquired, err := coord.TryAcquireOwnerLock()
	if err != nil {
		return nil, nil, err
	}
	if acquired {
		return lock, nil, nil
	}
	target, err := discover()
	if err != nil {
		return nil, nil, err
	}
	if target.kind == targetActive {
		return nil, target.client, nil
	}
	return nil, nil, &instanceBusyError{}
}

// instanceBusyError means the instance lock is held without reachable control.
type instanceBusyError struct{}

func (*instanceBusyError) Error() string {
	return "gateway instance is busy; retry after the current operation finishes"
}
