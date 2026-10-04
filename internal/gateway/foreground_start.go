package gateway

import (
	"context"
	"os"

	"github.com/QzCurious/seamless-cors/internal/systempac"
)

type StartHooks struct {
	ConfirmUpstreamListCreation func(context.Context, UpstreamListCreationConsent) (bool, error)
	Started                     func(StartResult)
}

// Start owns one foreground process from initialization through cleanup.
// A second invocation reports an existing instance without changing it.
func Start(ctx context.Context, hooks StartHooks) (StartResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return start(ctx, openSystemPAC(), nil, hooks)
}

func start(ctx context.Context, pac systempac.Module, ca userCAModule, hooks StartHooks) (StartResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	coord, err := defaultCoordinator()
	if err != nil {
		return nil, err
	}
	lock, acquired, err := coord.TryAcquireOwnerLock()
	if err != nil {
		return nil, err
	}
	if !acquired {
		if coord.Verify().Status == stateActive {
			result := AlreadyRunning{}
			if hooks.Started != nil {
				hooks.Started(result)
			}
			return result, nil
		}
		return StartOwnerTransition{}, nil
	}
	defer lock.Release()

	// Resolve startup input and ask for creation consent in the launching CLI.
	workingDirectory, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	directoryPath, err := directoryUpstreamListPath(workingDirectory)
	if err != nil {
		return nil, err
	}
	create := false
	if consent := assessUpstreamListCreation(defaultGlobalUpstreamListPath()); consent != nil && hooks.ConfirmUpstreamListCreation != nil {
		create, err = hooks.ConfirmUpstreamListCreation(ctx, *consent)
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if failures := cleanGatewayStateCache(coord, nil); len(failures) > 0 {
		return StartCleanupFailed{Failures: failures}, nil
	}
	if ca == nil {
		ca, err = openSystemUserCA()
		if err != nil {
			return nil, err
		}
	}
	owner, err := newOwner(pac, ca, coord)
	if err != nil {
		return nil, err
	}
	return owner.Run(ctx, directoryPath, create, hooks.Started)
}
