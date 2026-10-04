package gateway

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
	"github.com/QzCurious/seamless-cors/internal/systempac"
)

type cleanupProbePAC struct {
	endpoint               string
	cleanupCalls           int
	reachableDuringCleanup bool
	err                    error
}

type quiescingPAC struct {
	deliverEntered chan struct{}
	releaseDeliver chan struct{}
	cleanupEntered chan struct{}
	deliverCalls   int
}

func (f *quiescingPAC) Deliver(context.Context, string) error {
	f.deliverCalls++
	close(f.deliverEntered)
	<-f.releaseDeliver
	return nil
}
func (f *quiescingPAC) Inspect(context.Context) (systempac.Observation, error) {
	return systempac.Observation{}, nil
}
func (f *quiescingPAC) Cleanup(context.Context) error {
	close(f.cleanupEntered)
	return nil
}

func (f *cleanupProbePAC) Deliver(context.Context, string) error {
	return nil
}
func (f *cleanupProbePAC) Inspect(context.Context) (systempac.Observation, error) {
	return systempac.Observation{}, nil
}
func (f *cleanupProbePAC) Cleanup(context.Context) error {
	f.cleanupCalls++
	conn, err := net.DialTimeout("tcp", f.endpoint, time.Second)
	if err == nil {
		f.reachableDuringCleanup = true
		_ = conn.Close()
	}
	return f.err
}

func TestStopCleansSystemPACWhileRuntimeServesAndRemainsFulfilledOnFailure(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Contents(nil))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- runtime.ServeReady(ctx, ready) }()
	<-ready
	pac := &cleanupProbePAC{endpoint: runtime.PACListen(), err: systempac.VerificationError{ServiceName: "Wi-Fi", Cause: errors.New("verification uncertain")}}
	lifecycle := newLifecycle(pac, emptyTestUserCA{}, newCoordinator(t.TempDir()))
	lifecycle.runtime = &activeRuntime{engine: runtime.trafficRuntime, ctx: ctx, cancel: cancel}

	result, err := lifecycle.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Fulfillment() != CommandFulfilled || result.CleanupFulfillment != CommandUnfulfilled || len(result.SystemPACCleanup.Issues) != 1 || len(result.CleanupFailures) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if pac.cleanupCalls != 1 || !pac.reachableDuringCleanup {
		t.Fatalf("cleanup calls/reachable = %d/%t", pac.cleanupCalls, pac.reachableDuringCleanup)
	}
	if conn, err := net.DialTimeout("tcp", pac.endpoint, 100*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("PAC endpoint remained reachable after Stop")
	}
}

func TestStopQuiescesAdmittedDeliveryAndRejectsLaterDelivery(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Contents(nil))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	pac := &quiescingPAC{deliverEntered: make(chan struct{}), releaseDeliver: make(chan struct{}), cleanupEntered: make(chan struct{})}
	lifecycle := newLifecycle(pac, emptyTestUserCA{}, newCoordinator(t.TempDir()))
	active := &activeRuntime{engine: runtime.trafficRuntime, ctx: ctx, cancel: cancel}
	lifecycle.runtime = active
	deliveryDone := make(chan struct{})
	go func() {
		lifecycle.changeMu.Lock()
		_, _ = lifecycle.deliverSystemPAC(ctx, active)
		lifecycle.changeMu.Unlock()
		close(deliveryDone)
	}()
	<-pac.deliverEntered
	stopDone := make(chan StopResult, 1)
	go func() { result, _ := lifecycle.Stop(context.Background()); stopDone <- result }()
	deadline := time.Now().Add(time.Second)
	for {
		lifecycle.mu.Lock()
		ending := lifecycle.ownerEnding
		lifecycle.mu.Unlock()
		if ending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Stop did not enter Owner Ending")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-pac.cleanupEntered:
		t.Fatal("cleanup began before admitted delivery settled")
	case <-time.After(100 * time.Millisecond):
	}
	close(pac.releaseDeliver)
	<-deliveryDone
	result := <-stopDone
	if result.CleanupFulfillment != CommandFulfilled {
		t.Fatalf("result = %#v", result)
	}
	if _, delivered := lifecycle.deliverSystemPAC(context.Background(), active); delivered || pac.deliverCalls != 1 {
		t.Fatalf("post-cleanup delivery admitted=%t calls=%d", delivered, pac.deliverCalls)
	}
}

func TestConcurrentStopWaitsForAdmittedCAWorkAndSharesCleanup(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Contents(nil))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	lifecycle := runtime.lifecycle
	lifecycle.coord = newCoordinator(t.TempDir())
	mutating, releaseMutation := make(chan struct{}), make(chan struct{})
	cleaning, releaseCleanup := make(chan struct{}), make(chan struct{})
	var mutationOnce, cleanupOnce sync.Once
	unblockMutation := func() { mutationOnce.Do(func() { close(releaseMutation) }) }
	unblockCleanup := func() { cleanupOnce.Do(func() { close(releaseCleanup) }) }
	defer unblockMutation()
	defer unblockCleanup()
	lifecycle.userCA = &fakeUserCA{install: func(context.Context) (userCAState, error) {
		close(mutating)
		<-releaseMutation
		return userCAState{}, nil
	}}
	var cleanups atomic.Int32
	lifecycle.systemPAC = callbackPAC{cleanup: func(context.Context) error {
		if cleanups.Add(1) == 1 {
			close(cleaning)
		}
		<-releaseCleanup
		return systempac.VerificationError{ServiceName: "Wi-Fi", Cause: errors.New("verification uncertain")}
	}}
	installed := make(chan error, 1)
	go func() { _, err := lifecycle.Install(context.Background()); installed <- err }()
	awaitSignal(t, mutating)
	results := make(chan StopResult, 2)
	for range 2 {
		go func() { result, _ := lifecycle.Stop(context.Background()); results <- result }()
	}
	// Stop must close admission before waiting for the trust operation to settle.
	deadline := time.Now().Add(3 * time.Second)
	for {
		status, err := lifecycle.Status(context.Background(), false)
		if err != nil {
			t.Fatal(err)
		}
		if status.State == GatewayStatusEnding {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Stop did not close admission")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-cleaning:
		t.Fatal("PAC cleanup began before admitted CA work settled")
	default:
	}
	unblockMutation()
	if err := <-installed; err != nil {
		t.Fatal(err)
	}
	awaitSignal(t, cleaning)
	select {
	case <-results:
		t.Fatal("Stop returned before cleanup finished")
	default:
	}
	unblockCleanup()
	for range 2 {
		select {
		case result := <-results:
			if result.Kind != StopResultStopped || result.CleanupFulfillment != CommandUnfulfilled || len(result.CleanupFailures) != 1 {
				t.Fatalf("shared Stop result = %#v", result)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("concurrent Stop did not finish")
		}
	}
	if cleanups.Load() != 1 {
		t.Fatalf("concurrent cleanup calls = %d", cleanups.Load())
	}
}
