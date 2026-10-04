package gateway

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestForegroundServingFailureCleansPACAndDiscovery(t *testing.T) {
	for _, server := range []string{"control", "PAC"} {
		t.Run(server, func(t *testing.T) {
			coord := newCoordinator(t.TempDir())
			lock, acquired, err := coord.TryAcquireOwnerLock()
			if err != nil || !acquired {
				t.Fatalf("acquire instance lock = %t, %v", acquired, err)
			}
			defer lock.Release()
			settings := &lifecycleTestSystemSettings{}
			owner, err := newOwner(settings, emptyTestUserCA{}, coord)
			if err != nil {
				t.Fatal(err)
			}
			owner.lifecycle.globalUpstreamListPath = filepath.Join(t.TempDir(), upstreamListFileName)
			writeTrafficTestFile(t, owner.lifecycle.globalUpstreamListPath, "api.example.test\n")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ready := make(chan struct{})
			done := make(chan error, 1)
			directory := filepath.Join(t.TempDir(), upstreamListFileName)
			go func() {
				_, err := owner.Run(ctx, directory, false, func(StartResult) { close(ready) })
				done <- err
			}()
			awaitSignal(t, ready)
			address := owner.listener.Addr().(*net.TCPAddr)
			if !address.IP.IsLoopback() || address.Port == 0 || coord.Verify().Status != stateActive {
				t.Fatalf("control endpoint was not discoverable on loopback: %s", address)
			}
			// Listener failure exercises actual serving supervision, not a synthetic event.
			if server == "control" {
				err = owner.listener.Close()
			} else {
				err = owner.lifecycle.runtime.engine.listeners[1].Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("serving failure was hidden")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("foreground process did not end after serving failure")
			}
			if settings.cleanupCalls != 1 || coord.Exists() {
				t.Fatalf("cleanup calls = %d, discovery remains = %t", settings.cleanupCalls, coord.Exists())
			}
		})
	}
}
