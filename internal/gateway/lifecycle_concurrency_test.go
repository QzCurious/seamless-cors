package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
	"github.com/QzCurious/seamless-cors/internal/systempac"
)

type callbackPAC struct {
	deliver func(context.Context, string) error
	cleanup func(context.Context) error
}

func (p callbackPAC) Deliver(ctx context.Context, endpoint string) error {
	if p.deliver != nil {
		return p.deliver(ctx, endpoint)
	}
	return nil
}
func (p callbackPAC) Inspect(context.Context) (systempac.Observation, error) {
	return systempac.Observation{}, nil
}
func (p callbackPAC) Cleanup(ctx context.Context) error {
	if p.cleanup != nil {
		return p.cleanup(ctx)
	}
	return nil
}

func awaitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for operation")
	}
}

func assertPACServing(t *testing.T, endpoint string) string {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + endpoint + "/seamless-cors.pac")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "FindProxyForURL") {
		t.Fatalf("PAC response: %d %s", response.StatusCode, body)
	}
	return string(body)
}

func TestHTTPStartOutlivesItsRequest(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		name := "successful response"
		if disconnect {
			name = "disconnect after acceptance"
		}
		t.Run(name, func(t *testing.T) {
			inspecting := make(chan struct{})
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			ca := &fakeUserCA{inspect: func(ctx context.Context) (userCAState, error) {
				close(inspecting)
				select {
				case <-release:
					return userCAState{}, nil
				case <-ctx.Done():
					return userCAState{}, ctx.Err()
				}
			}}
			owner, err := newLifecycleUninspected(callbackPAC{}, ca, newCoordinator(t.TempDir()), "")
			if err != nil {
				t.Fatal(err)
			}
			owner.globalUpstreamListPath = filepath.Join(t.TempDir(), "upstreams.txt")
			writeTrafficTestFile(t, owner.globalUpstreamListPath, "api.example.test\n")
			router := newRouter("token", owner)
			requestCtx := make(chan context.Context, 1)
			finished := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCtx <- r.Context()
				defer close(finished)
				router.server.Handler.ServeHTTP(w, r)
			}))
			defer server.Close()
			defer func() { unblock(); _, _ = owner.Stop(context.Background()) }()
			client := newClient(stateCache{HTTPRouterListen: server.Listener.Addr().String(), Token: "token"})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			completed := make(chan error, 1)
			directory := t.TempDir()
			go func() {
				result, err := client.Start(ctx, StartRequest{WorkingDirectory: directory})
				if err == nil && result.Kind() != StartResultStarted {
					err = errors.New("Start did not activate")
				}
				completed <- err
			}()
			awaitSignal(t, inspecting)
			httpContext := <-requestCtx
			if disconnect {
				cancel()
				awaitSignal(t, httpContext.Done())
			}
			unblock()
			select {
			case err := <-completed:
				if disconnect && !errors.Is(err, context.Canceled) {
					t.Fatalf("disconnected client: %v", err)
				}
				if !disconnect && err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("Start did not complete")
			}
			awaitSignal(t, finished)
			awaitSignal(t, httpContext.Done())
			status, err := owner.Status(context.Background(), false)
			if err != nil || status.State != GatewayStatusRunning {
				t.Fatalf("status after request ended: %#v, %v", status, err)
			}
			assertPACServing(t, status.Runtime.PACListen)
		})
	}
}

func TestCancelledStartDoesNotCreateOrActivate(t *testing.T) {
	ca := &fakeUserCA{}
	owner, err := newLifecycleUninspected(callbackPAC{}, ca, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	owner.globalUpstreamListPath = filepath.Join(t.TempDir(), "upstreams.txt")
	consent := assessUpstreamListCreation(owner.globalUpstreamListPath)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := owner.ExecuteStart(ctx, StartRequest{WorkingDirectory: t.TempDir(), UpstreamListCreationConsent: &UpstreamListCreationConsentInput{Decision: UpstreamListCreationAccepted, Fingerprint: consent.Fingerprint}})
	if !errors.Is(err, context.Canceled) || result != nil || owner.RuntimeActive() || ca.inspectCalls != 0 {
		t.Fatalf("cancelled Start: %#v, %v", result, err)
	}
	if assessUpstreamListCreation(owner.globalUpstreamListPath) == nil {
		t.Fatal("cancelled Start created the Upstream List")
	}
}

func TestOwnerCancellationDuringStartCleansPACBeforeClosingTraffic(t *testing.T) {
	coord := newCoordinator(t.TempDir())
	lock, acquired, err := coord.TryAcquireOwnerLock()
	if err != nil || !acquired {
		t.Fatalf("owner lock: %t, %v", acquired, err)
	}
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
			client := &http.Client{Timeout: time.Second}
			response, err := client.Get("http://" + endpoint + "/seamless-cors.pac")
			if err == nil {
				cleanedWhileServing = response.StatusCode == http.StatusOK
				response.Body.Close()
			}
			return err
		},
	}
	owner, err := newOwnerWithCoordinator(pac, emptyTestUserCA{}, coord)
	if err != nil {
		t.Fatal(err)
	}
	owner.lock = lock
	owner.lifecycle.globalUpstreamListPath = filepath.Join(t.TempDir(), "upstreams.txt")
	writeTrafficTestFile(t, owner.lifecycle.globalUpstreamListPath, "api.example.test\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	directory := t.TempDir()
	done := make(chan error, 1)
	var result StartResult
	go func() {
		done <- owner.Run(ctx, func(ctx context.Context) error {
			var err error
			result, err = owner.lifecycle.ExecuteStart(ctx, StartRequest{WorkingDirectory: directory})
			return err
		})
	}()
	awaitSignal(t, delivering)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("owner shutdown blocked on Start")
	}
	if result == nil || result.Kind() != StartResultStopCancelled || !cleanedWhileServing {
		t.Fatalf("Start = %#v, serving during cleanup = %t", result, cleanedWhileServing)
	}
	client := &http.Client{Timeout: time.Second}
	if response, err := client.Get("http://" + endpoint); err == nil {
		response.Body.Close()
		t.Fatal("traffic remained open after Stop")
	}
}

func TestSynchronousDeliveryKeepsStateReadableAndPreservesEachChange(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Contents(nil))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	runtime.lifecycle.coord = newCoordinator(t.TempDir())
	ready := make(chan struct{})
	go runtime.ServeReady(runtime.active.ctx, ready)
	awaitSignal(t, ready)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	deliveries := 0
	runtime.lifecycle.systemPAC = callbackPAC{deliver: func(context.Context, string) error {
		deliveries++
		if deliveries == 1 {
			close(entered)
			<-release
		}
		return nil
	}}
	first := make(chan error, 1)
	go func() { first <- runtime.applyUpstreamListOutcome(fileobservation.Contents("first.example.test\n")) }()
	awaitSignal(t, entered)
	statusDone := make(chan struct{})
	var status StatusResult
	go func() { status, _ = runtime.lifecycle.Status(context.Background(), false); close(statusDone) }()
	awaitSignal(t, statusDone)
	if status.Runtime.UpstreamCount != 1 {
		t.Fatalf("status lost adopted facts: %#v", status)
	}
	if !strings.Contains(assertPACServing(t, runtime.PACListen()), "first.example.test") {
		t.Fatal("PAC was not published before delivery")
	}
	second := make(chan error, 1)
	go func() { second <- runtime.applyUpstreamListOutcome(fileobservation.Contents("second.example.test\n")) }()
	select {
	case <-first:
		t.Fatal("source update returned before delivery settled")
	default:
	}
	once.Do(func() { close(release) })
	for _, done := range []<-chan error{first, second} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("source update deadlocked")
		}
	}
	if deliveries != 2 {
		t.Fatalf("delivery calls = %d, want one per effective change", deliveries)
	}
	if !strings.Contains(assertPACServing(t, runtime.PACListen()), "second.example.test") {
		t.Fatal("second projection was lost")
	}
}

func TestCAMutationPublishesFactsBeforeTrustWorkAndIgnoresStaleExpiry(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Contents("https://secure.example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	runtime.lifecycle.coord = newCoordinator(t.TempDir())
	current := testUserCAState(t, time.Now().Add(time.Hour), false)
	runtime.AdoptUserCA(current, nil)
	oldRevision := runtime.lifecycle.userCARevision
	replacement := current
	replacement.Identity = "replacement-userca"
	mutating := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	runtime.lifecycle.userCA = &fakeUserCA{install: func(context.Context) (userCAState, error) {
		close(mutating)
		<-release
		return replacement, nil
	}}
	done := make(chan error, 1)
	requestCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, err := runtime.lifecycle.Install(requestCtx); done <- err }()
	awaitSignal(t, mutating)
	cancel()
	status, err := runtime.lifecycle.Status(context.Background(), false)
	if err != nil || status.InstalledCA.Health != CAHealthMutating || runtime.snapshot().ServedHTTPSCORS {
		t.Fatalf("mutation exposed usable HTTPS: %#v, %v", status, err)
	}
	competing, err := runtime.lifecycle.Uninstall(context.Background())
	if err != nil || competing.Kind != UninstallResultAlreadyMutating {
		t.Fatalf("competing CA operation: %#v, %v", competing, err)
	}
	once.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("admitted install did not complete")
	}
	runtime.lifecycle.handleUserCADeadline(runtime.active, oldRevision)
	state := runtime.snapshot()
	if !state.ServedHTTPSCORS || !state.UserCAIdentityMatches || runtime.lifecycle.userCAState.Identity != replacement.Identity {
		t.Fatalf("stale expiry invalidated replacement: %#v", state)
	}
}
