package gateway

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestStopWithoutOwnerReturnsNotRunningAndRemovesStaleCache(t *testing.T) {
	_, coord := useTestGatewayEnvironment(t)
	if err := coord.Claim(stateCache{HTTPRouterListen: "127.0.0.1:1", Token: "stale"}); err != nil {
		t.Fatal(err)
	}
	settings := &lifecycleTestSystemSettings{
		services: []systemPACTestService{{
			ServiceName: "Wi-Fi", URL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: true, Observed: true,
		}},
	}

	result, err := stop(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != StopResultNotRunning {
		t.Fatalf("stop kind = %s, want %s", result.Kind, StopResultNotRunning)
	}
	if coord.Exists() {
		t.Fatal("stale Gateway State Cache was not removed")
	}
	if settings.cleared != 1 {
		t.Fatalf("System PAC cleanup calls = %d, want 1", settings.cleared)
	}
}

func TestOfflineCAWorkExcludesCompetingCommandsWithoutPublishingControl(t *testing.T) {
	for _, operation := range []string{"install", "uninstall"} {
		t.Run(operation, func(t *testing.T) {
			_, coord := useTestGatewayEnvironment(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			ca := &fakeUserCA{
				install: func(context.Context) (userCAState, error) {
					close(entered)
					<-release
					return userCAState{}, nil
				},
				uninstall: func(context.Context) error {
					close(entered)
					<-release
					return nil
				},
			}
			done := make(chan error, 1)
			go func() {
				var err error
				if operation == "install" {
					_, err = installCA(context.Background(), ca)
				} else {
					_, err = uninstallCA(context.Background(), ca, UninstallRequest{})
				}
				done <- err
			}()
			awaitSignal(t, entered)
			if coord.Exists() {
				t.Fatal("offline CA work published Gateway discovery state")
			}
			competingCA := &fakeUserCA{}
			install, err := installCA(context.Background(), competingCA)
			if err != nil || install.Kind != InstallResultOwnerTransition {
				t.Fatalf("competing install = %#v, %v", install, err)
			}
			uninstall, err := uninstallCA(context.Background(), competingCA, UninstallRequest{})
			if err != nil || uninstall.Kind != UninstallResultOwnerTransition {
				t.Fatalf("competing uninstall = %#v, %v", uninstall, err)
			}
			start, err := start(context.Background(), nil, nil, StartHooks{})
			if err != nil || start.Kind() != StartResultOwnerTransition {
				t.Fatalf("competing start = %#v, %v", start, err)
			}
			settings := &lifecycleTestSystemSettings{}
			status, err := status(context.Background(), settings, competingCA)
			if err != nil || status.Kind != StatusResultOwnerTransition {
				t.Fatalf("competing status = %#v, %v", status, err)
			}
			if _, err := stop(context.Background(), settings); err == nil || settings.cleared != 0 {
				t.Fatalf("competing stop = %v, PAC cleanup calls = %d", err, settings.cleared)
			}
			if competingCA.inspectCalls != 0 || competingCA.installCalls != 0 || competingCA.uninstallCalls != 0 {
				t.Fatal("competing command accessed UserCA without the instance lock")
			}
			unblock()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("offline CA operation did not finish")
			}
			lock, acquired, err := coord.TryAcquireOwnerLock()
			if err != nil || !acquired {
				t.Fatalf("offline CA work did not release instance lock: %t, %v", acquired, err)
			}
			defer lock.Release()
			if ca.inspectCalls != 0 {
				t.Fatalf("offline CA work performed an unrelated inspection: %d", ca.inspectCalls)
			}
		})
	}
}

func TestOwnerlessStatusReportsOwnershipTransitionInsteadOfInspectingUnlocked(t *testing.T) {
	_, coord := useTestGatewayEnvironment(t)
	lock, acquired, err := coord.TryAcquireOwnerLock()
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("test did not acquire Gateway Ownership")
	}
	defer lock.Release()
	ca := &fakeUserCA{}

	result, err := status(context.Background(), &lifecycleTestSystemSettings{}, ca)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != StatusResultOwnerTransition || result.Fulfillment() != CommandUnfulfilled {
		t.Fatalf("status result = %#v", result)
	}
	if ca.inspectCalls != 0 {
		t.Fatalf("status inspected UserCA without coherent ownership: %d calls", ca.inspectCalls)
	}
}

func TestOwnerlessStatusReportsEveryVisibleNetworkService(t *testing.T) {
	useTestGatewayEnvironment(t)
	settings := &lifecycleTestSystemSettings{services: []systemPACTestService{
		{ServiceName: "Wi-Fi", Observed: true},
		{ServiceName: "Corporate VPN", URL: "http://corp/pac", Enabled: true, Observed: true},
	}}
	result, err := status(context.Background(), settings, &fakeUserCA{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Runtime != nil || len(result.SystemPAC.Services) != 2 || result.SystemPAC.RoutesCurrentEndpoint {
		t.Fatalf("status = %#v", result.StatusReport)
	}
}

func TestStopWithoutOwnerPreservesResultWhenCleanupFails(t *testing.T) {
	_, coord := useTestGatewayEnvironment(t)
	if err := coord.Claim(stateCache{HTTPRouterListen: "127.0.0.1:1", Token: "stale"}); err != nil {
		t.Fatal(err)
	}
	settings := &lifecycleTestSystemSettings{
		clearErr: errors.New("pac denied"),
		services: []systemPACTestService{{
			ServiceName: "Wi-Fi", URL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: true, Observed: true,
		}},
	}

	result, err := stop(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != StopResultNotRunning || result.Fulfillment() != CommandFulfilled {
		t.Fatalf("stop result = %#v", result)
	}
	if len(result.CleanupFailures) != 1 || result.CleanupFailures[0].Subject != CleanupSubjectSystemPAC {
		t.Fatalf("cleanup failures = %#v", result.CleanupFailures)
	}
	if coord.Exists() {
		t.Fatal("stale Gateway State Cache was not removed after PAC cleanup failure")
	}
}

func TestStopWithoutPublishedOwnerRejectsOwnerLockContention(t *testing.T) {
	_, coord := useTestGatewayEnvironment(t)
	lock, acquired, err := coord.TryAcquireOwnerLock()
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("test did not acquire ownership lock")
	}
	defer lock.Release()
	settings := &lifecycleTestSystemSettings{}

	_, err = stop(context.Background(), settings)

	var busy *instanceBusyError
	if !errors.As(err, &busy) {
		t.Fatalf("stop error = %v, want retryable instance contention", err)
	}
	if settings.cleared != 0 {
		t.Fatalf("System PAC cleanup calls = %d, want 0", settings.cleared)
	}
}
