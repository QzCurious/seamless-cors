package liveconfig_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"seamless-cors/internal/liveconfig"
)

func TestWatchEmitsEffectiveConfigAndKeepsLifecycleChangesPending(t *testing.T) {
	home := t.TempDir()
	firstDomainPath := filepath.Join(home, "first-domains.txt")
	secondDomainPath := filepath.Join(home, "second-domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, firstDomainPath, "first.example.test\n")
	writeFile(t, secondDomainPath, "second.example.test\n")
	writeConfig(t, configPath, firstDomainPath, false)

	source, initial, err := liveconfig.LoadOrBootstrap(configPath, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if initial.CATrusted() {
		t.Fatal("initial CA trust = true")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := source.Watch(ctx, 10*time.Millisecond)

	writeConfig(t, configPath, secondDomainPath, true)
	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	live := event.Config
	if live.DomainListPath() != secondDomainPath {
		t.Fatalf("domain list path = %q", live.DomainListPath())
	}
	entries := live.Entries()
	if len(entries) != 1 || entries[0].Host != "second.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
	if live.CATrusted() {
		t.Fatal("restart-applied CA trust was hot-applied")
	}
	if live.Effective().CATrusted {
		t.Fatal("effective config exposed desired CA trust")
	}
	if got := strings.Join(live.PendingLifecycle(), ","); got != "ca-trusted" {
		t.Fatalf("pending lifecycle = %q", got)
	}

	cached := source.Config()
	if cached.DomainListPath() != secondDomainPath {
		t.Fatalf("cached domain list path = %q", cached.DomainListPath())
	}

	writeFile(t, secondDomainPath, "second-updated.example.test\n")
	event = waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	live = event.Config
	if live.CATrusted() {
		t.Fatal("Domain List reload hot-applied CA trust")
	}
	if live.Effective().CATrusted {
		t.Fatal("Domain List reload exposed desired CA trust")
	}
	if got := strings.Join(live.PendingLifecycle(), ","); got != "ca-trusted" {
		t.Fatalf("pending lifecycle after Domain List reload = %q", got)
	}

	writeConfig(t, configPath, secondDomainPath, false)
	event = waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	if got := strings.Join(event.Config.PendingLifecycle(), ","); got != "" {
		t.Fatalf("pending lifecycle after revert = %q", got)
	}
}

func TestWatchEmitsErrorWithoutReplacingCachedConfig(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, "first.example.test\n")
	writeConfig(t, configPath, domainPath, false)
	source, _, err := liveconfig.LoadOrBootstrap(configPath, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := source.Watch(ctx, 10*time.Millisecond)

	writeFile(t, domainPath, "https://*.bad.example.test\n")
	event := waitForEvent(t, events)
	if event.Err == nil || !strings.Contains(event.Err.Error(), "Fatal Domain List Error") {
		t.Fatalf("event error = %v", event.Err)
	}
	cached := source.Config()
	entries := cached.Entries()
	if len(entries) != 1 || entries[0].Host != "first.example.test" {
		t.Fatalf("cached entries = %#v", entries)
	}
}

func TestWatchTreatsMissingLiveDomainListAsFatal(t *testing.T) {
	home := t.TempDir()
	firstDomainPath := filepath.Join(home, "first-domains.txt")
	missingDomainPath := filepath.Join(home, "missing-domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, firstDomainPath, "first.example.test\n")
	writeConfig(t, configPath, firstDomainPath, false)
	source, _, err := liveconfig.LoadOrBootstrap(configPath, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := source.Watch(ctx, 10*time.Millisecond)

	writeConfig(t, configPath, missingDomainPath, false)
	event := waitForEvent(t, events)
	if event.Err == nil || !strings.Contains(event.Err.Error(), "Fatal Domain List Error") {
		t.Fatalf("event error = %v", event.Err)
	}
}

func TestWatchTreatsUnreadableLiveConfigAsFatal(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, "first.example.test\n")
	writeConfig(t, configPath, domainPath, false)
	source, _, err := liveconfig.LoadOrBootstrap(configPath, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := source.Watch(ctx, 10*time.Millisecond)

	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	event := waitForEvent(t, events)
	if event.Err == nil || !strings.Contains(event.Err.Error(), "Fatal Config Error") {
		t.Fatalf("event error = %v", event.Err)
	}
}

func writeConfig(t *testing.T, path, domainPath string, caTrusted bool) {
	t.Helper()
	writeFile(t, path, "domain-list: "+domainPath+"\nca-trusted: "+map[bool]string{true: "true", false: "false"}[caTrusted]+"\n")
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitForEvent(t *testing.T, events <-chan liveconfig.Event) liveconfig.Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("event channel closed")
		}
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return liveconfig.Event{}
	}
}
