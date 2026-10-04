package gateway

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStartReturnsOwnerTransitionWhenInstanceLockIsHeldWithoutDiscovery(t *testing.T) {
	_, coord := useTestGatewayEnvironment(t)
	lock, acquired, err := coord.TryAcquireOwnerLock()
	if err != nil || !acquired {
		t.Fatalf("acquire instance lock = %t, %v", acquired, err)
	}
	defer lock.Release()
	result, err := start(context.Background(), nil, nil, StartHooks{})
	if err != nil || result.Kind() != StartResultOwnerTransition {
		t.Fatalf("start = %#v, %v", result, err)
	}
}

func TestForegroundStartRetainsTrafficAndSupportsLiveCommands(t *testing.T) {
	_, coord := useTestGatewayEnvironment(t)
	directory := t.TempDir()
	t.Chdir(directory)
	writeTrafficTestFile(t, filepath.Join(directory, upstreamListFileName), "http://plain.example.test\nhttps://secure.example.test\n")
	settings := &lifecycleTestSystemSettings{
		services:      []systemPACTestService{{ServiceName: "Wi-Fi", Observed: true}},
		deliverRoutes: true,
	}
	ca := &fakeUserCA{installState: testUserCAState(t, time.Now().Add(time.Hour), false)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	done := make(chan error, 1)
	consentCalls := 0
	go func() {
		_, err := start(ctx, settings, ca, StartHooks{
			ConfirmUpstreamListCreation: func(context.Context, UpstreamListCreationConsent) (bool, error) {
				consentCalls++
				return true, nil
			},
			Started: func(StartResult) { close(ready) },
		})
		done <- err
	}()
	awaitSignal(t, ready)
	select {
	case err := <-done:
		t.Fatalf("start returned while Gateway was running: %v", err)
	default:
	}
	if consentCalls != 1 {
		t.Fatalf("creation prompts = %d", consentCalls)
	}
	if _, err := os.Stat(defaultGlobalUpstreamListPath()); err != nil {
		t.Fatal(err)
	}
	before, err := Status(context.Background())
	if err != nil || before.State != GatewayStatusRunning || before.Runtime.UpstreamCount != 2 {
		t.Fatalf("live status = %#v, %v", before, err)
	}
	deliveries := settings.applied
	second, err := start(context.Background(), settings, ca, StartHooks{
		ConfirmUpstreamListCreation: func(context.Context, UpstreamListCreationConsent) (bool, error) {
			t.Error("second start prompted for input")
			return false, nil
		},
	})
	if err != nil || second.Kind() != StartResultAlreadyRunning || settings.applied != deliveries {
		t.Fatalf("second start = %#v, %v, deliveries = %d -> %d", second, err, deliveries, settings.applied)
	}
	// Discovery loss must not weaken single-instance ownership or clean live PAC.
	cache := coord.Verify().Cache
	if err := coord.Remove(); err != nil {
		t.Fatal(err)
	}
	missing, err := start(context.Background(), nil, nil, StartHooks{})
	if err != nil || missing.Kind() != StartResultOwnerTransition || settings.cleanupCalls != 0 {
		t.Fatalf("start with missing discovery = %#v, %v, cleanup calls = %d", missing, err, settings.cleanupCalls)
	}
	if err := coord.Claim(cache); err != nil {
		t.Fatal(err)
	}
	installed, err := installCA(context.Background(), ca)
	if err != nil || installed.Kind != InstallResultInstalled || ca.installCalls != 1 {
		t.Fatalf("live install = %#v, %v", installed, err)
	}
	after, err := Status(context.Background())
	if err != nil || after.Runtime.Traffic.HTTPSCORS != TrafficFeatureActive || before.Runtime.ProxyListen != after.Runtime.ProxyListen {
		t.Fatalf("live HTTPS adoption = %#v, %v", after, err)
	}
	consent, err := uninstallCA(context.Background(), ca, UninstallRequest{})
	if err != nil || consent.Kind != UninstallResultConsentRequired || ca.uninstallCalls != 0 {
		t.Fatalf("live uninstall consent = %#v, %v", consent, err)
	}
	removed, err := uninstallCA(context.Background(), ca, UninstallRequest{ConsentFingerprint: consent.ConsentFingerprint})
	if err != nil || removed.Kind != UninstallResultUninstalled || ca.uninstallCalls != 1 {
		t.Fatalf("live uninstall = %#v, %v", removed, err)
	}
	after, err = Status(context.Background())
	if err != nil || after.Runtime.Traffic.HTTPSCORS != TrafficFeatureBlocked || after.Runtime.Traffic.HTTPCORS != TrafficFeatureActive {
		t.Fatalf("traffic after uninstall = %#v, %v", after, err)
	}
	stopped, err := Stop(context.Background())
	if err != nil || stopped.Kind != StopResultStopped {
		t.Fatalf("live stop = %#v, %v", stopped, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("foreground process did not exit after stop")
	}
	if coord.Exists() || settings.cleanupCalls != 1 {
		t.Fatalf("stop left discovery or repeated cleanup: %t, %d", coord.Exists(), settings.cleanupCalls)
	}
	lock, acquired, err := coord.TryAcquireOwnerLock()
	if err != nil || !acquired {
		t.Fatalf("instance lock after stop = %t, %v", acquired, err)
	}
	defer lock.Release()
}

func TestStartupCancellationCleansPACBeforeClosingTraffic(t *testing.T) {
	_, coord := useTestGatewayEnvironment(t)
	if err := createUpstreamList(defaultGlobalUpstreamListPath()); err != nil {
		t.Fatal(err)
	}
	writeTrafficTestFile(t, defaultGlobalUpstreamListPath(), "api.example.test\n")
	delivering := make(chan struct{})
	var endpoint string
	cleanedWhileServing := false
	pac := callbackPAC{
		deliver: func(ctx context.Context, current string) error {
			endpoint = current
			close(delivering)
			<-ctx.Done()
			return ctx.Err()
		},
		cleanup: func(context.Context) error {
			response, err := (&http.Client{Timeout: time.Second}).Get("http://" + endpoint + "/seamless-cors.pac")
			if err != nil {
				return err
			}
			defer response.Body.Close()
			cleanedWhileServing = response.StatusCode == http.StatusOK
			return nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var result StartResult
	go func() {
		var err error
		result, err = start(ctx, pac, emptyTestUserCA{}, StartHooks{})
		done <- err
	}()
	awaitSignal(t, delivering)
	if coord.Exists() {
		t.Fatal("startup published control discovery before initialization finished")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("startup did not honor foreground cancellation")
	}
	if result.Kind() != StartResultCancelled || !cleanedWhileServing || coord.Exists() {
		t.Fatalf("cancelled start = %#v, PAC serving during cleanup = %t", result, cleanedWhileServing)
	}
	lock, acquired, err := coord.TryAcquireOwnerLock()
	if err != nil || !acquired {
		t.Fatalf("instance lock after cancellation = %t, %v", acquired, err)
	}
	defer lock.Release()
}

func TestCancelledStartupDoesNotCreateOrInspect(t *testing.T) {
	useTestGatewayEnvironment(t)
	ca := &fakeUserCA{}
	ctx, cancel := context.WithCancel(context.Background())
	result, err := start(ctx, &lifecycleTestSystemSettings{}, ca, StartHooks{
		ConfirmUpstreamListCreation: func(context.Context, UpstreamListCreationConsent) (bool, error) {
			cancel()
			return true, nil
		},
	})
	if !errors.Is(err, context.Canceled) || result != nil || ca.inspectCalls != 0 {
		t.Fatalf("cancelled startup = %#v, %v, CA inspections = %d", result, err, ca.inspectCalls)
	}
	if _, err := os.Stat(defaultGlobalUpstreamListPath()); !os.IsNotExist(err) {
		t.Fatalf("cancelled startup created an Upstream List: %v", err)
	}
}
