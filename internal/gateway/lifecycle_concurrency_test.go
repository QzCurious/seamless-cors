package gateway

import (
	"context"
	"io"
	"net/http"
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
