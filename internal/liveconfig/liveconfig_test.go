package liveconfig

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"seamless-cors/internal/config"
	"seamless-cors/internal/domain"
)

func TestWatchEmitsParsedSnapshotAndKeepsLifecycleChangesPending(t *testing.T) {
	home := t.TempDir()
	firstDomainPath := filepath.Join(home, "first-domains.txt")
	secondDomainPath := filepath.Join(home, "second-domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, firstDomainPath, "first.example.test\n")
	writeFile(t, secondDomainPath, "second.example.test\n")
	writeConfig(t, configPath, firstDomainPath, false)

	entries, errs := domain.ParseList("first.example.test\n")
	if len(errs) > 0 {
		t.Fatalf("domain errors = %v", errs)
	}
	source := NewSource(Snapshot{
		Config: config.Config{
			DomainList: firstDomainPath,
			CATrusted:  false,
			SourcePath: configPath,
		},
		ConfigPath:     configPath,
		DomainListPath: firstDomainPath,
		Entries:        entries,
		CATrusted:      false,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := source.Watch(ctx, 10*time.Millisecond)

	writeConfig(t, configPath, secondDomainPath, true)
	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	if event.Snapshot.DomainListPath != secondDomainPath {
		t.Fatalf("domain list path = %q", event.Snapshot.DomainListPath)
	}
	if len(event.Snapshot.Entries) != 1 || event.Snapshot.Entries[0].Host != "second.example.test" {
		t.Fatalf("entries = %#v", event.Snapshot.Entries)
	}
	if event.Snapshot.CATrusted {
		t.Fatal("restart-applied CA trust was hot-applied")
	}
	if !event.Snapshot.Config.CATrusted {
		t.Fatal("desired CA trust was not retained in Explicit Configuration")
	}
	if got := strings.Join(event.Snapshot.PendingLifecycle, ","); got != "ca-trusted" {
		t.Fatalf("pending lifecycle = %q", got)
	}

	cached := source.Snapshot()
	if cached.DomainListPath != secondDomainPath {
		t.Fatalf("cached domain list path = %q", cached.DomainListPath)
	}

	writeFile(t, secondDomainPath, "second-updated.example.test\n")
	event = waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	if event.Snapshot.CATrusted {
		t.Fatal("Domain List reload hot-applied CA trust")
	}
	if !event.Snapshot.Config.CATrusted {
		t.Fatal("Domain List reload lost desired CA trust")
	}
	if got := strings.Join(event.Snapshot.PendingLifecycle, ","); got != "ca-trusted" {
		t.Fatalf("pending lifecycle after Domain List reload = %q", got)
	}
}

func TestWatchEmitsErrorWithoutReplacingCachedSnapshot(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	writeFile(t, domainPath, "first.example.test\n")
	entries, errs := domain.ParseList("first.example.test\n")
	if len(errs) > 0 {
		t.Fatalf("domain errors = %v", errs)
	}
	source := NewSource(Snapshot{
		Config: config.Config{
			DomainList: domainPath,
			CATrusted:  false,
		},
		DomainListPath: domainPath,
		Entries:        entries,
		CATrusted:      false,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := source.Watch(ctx, 10*time.Millisecond)

	writeFile(t, domainPath, "https://*.bad.example.test\n")
	event := waitForEvent(t, events)
	if event.Err == nil || !strings.Contains(event.Err.Error(), "Fatal Domain List Error") {
		t.Fatalf("event error = %v", event.Err)
	}
	cached := source.Snapshot()
	if len(cached.Entries) != 1 || cached.Entries[0].Host != "first.example.test" {
		t.Fatalf("cached entries = %#v", cached.Entries)
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

func waitForEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("event channel closed")
		}
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}
