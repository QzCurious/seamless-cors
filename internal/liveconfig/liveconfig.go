package liveconfig

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"seamless-cors/internal/config"
	"seamless-cors/internal/domain"
)

type Snapshot struct {
	Config           config.Config
	ConfigPath       string
	DomainListPath   string
	Entries          []domain.Entry
	CATrusted        bool
	PendingLifecycle []string
}

type Event struct {
	Snapshot Snapshot
	Err      error
}

type Source struct {
	mu                sync.RWMutex
	snapshot          Snapshot
	baselineCATrusted bool
	lastConfigText    string
	lastDomainText    string
}

func LoadOrBootstrap(configPath string, stdout io.Writer) (*Source, Snapshot, error) {
	loaded, err := config.LoadOrBootstrap(configPath, stdout)
	if err != nil {
		return nil, Snapshot{}, err
	}
	entries, domainText, err := loadDomainList(loaded.DomainPath)
	if err != nil {
		return nil, Snapshot{}, err
	}
	snapshot := snapshotFromLoadResult(loaded, entries, nil, loaded.Config.CATrusted)
	source := NewSource(snapshot)
	source.lastConfigText, _ = readText(loaded.ConfigPath)
	source.lastDomainText = domainText
	return source, snapshot, nil
}

func NewSource(snapshot Snapshot) *Source {
	snapshot = cloneSnapshot(snapshot)
	snapshot.CATrusted = snapshot.Config.CATrusted
	snapshot.DomainListPath = snapshot.Config.DomainList
	return &Source{
		snapshot:          snapshot,
		baselineCATrusted: snapshot.Config.CATrusted,
	}
}

func (s *Source) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshot(s.snapshot)
}

func (s *Source) Watch(ctx context.Context, interval time.Duration) <-chan Event {
	events := make(chan Event, 1)
	snapshot := s.Snapshot()
	if snapshot.ConfigPath == "" && snapshot.DomainListPath == "" {
		close(events)
		return events
	}
	go s.watch(ctx, interval, events)
	return events
}

func (s *Source) watch(ctx context.Context, interval time.Duration, events chan<- Event) {
	defer close(events)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			event, changed := s.poll()
			if !changed {
				continue
			}
			select {
			case events <- event:
			case <-ctx.Done():
				return
			}
			if event.Err != nil {
				return
			}
		}
	}
}

func (s *Source) poll() (Event, bool) {
	current := s.Snapshot()
	var configText string
	var configChanged bool
	if current.ConfigPath != "" {
		var configErr error
		configText, configErr = readText(current.ConfigPath)
		if configErr != nil {
			return Event{}, false
		}
		configChanged = configText != s.lastConfigText
	}
	domainPath := current.DomainListPath
	var loaded config.LoadResult
	if configChanged {
		var err error
		loaded, err = config.LoadExisting(current.ConfigPath)
		if err != nil {
			return Event{Err: fmt.Errorf("Fatal Config Error: %w", err)}, true
		}
		domainPath = loaded.DomainPath
	} else {
		loaded = config.LoadResult{
			Config:     current.Config,
			ConfigPath: current.ConfigPath,
			DomainPath: current.DomainListPath,
		}
	}
	domainText, domainErr := readText(domainPath)
	if domainErr != nil {
		return Event{}, false
	}
	domainChanged := domainText != s.lastDomainText || domainPath != current.DomainListPath
	if !configChanged && !domainChanged {
		return Event{}, false
	}
	entries, errs := domain.ParseList(domainText)
	if len(errs) > 0 {
		return Event{Err: fmt.Errorf("Fatal Domain List Error: invalid Domain List:\n%s", formatDomainErrors(errs))}, true
	}
	next := snapshotFromLoadResult(loaded, entries, lifecycleChanges(loaded.Config.CATrusted, s.baselineCATrusted), s.baselineCATrusted)
	s.mu.Lock()
	s.snapshot = next
	s.lastConfigText = configText
	s.lastDomainText = domainText
	s.mu.Unlock()
	return Event{Snapshot: cloneSnapshot(next)}, true
}

func snapshotFromLoadResult(loaded config.LoadResult, entries []domain.Entry, pending []string, activeCATrusted bool) Snapshot {
	return Snapshot{
		Config:           loaded.Config,
		ConfigPath:       loaded.ConfigPath,
		DomainListPath:   loaded.DomainPath,
		Entries:          append([]domain.Entry(nil), entries...),
		CATrusted:        activeCATrusted,
		PendingLifecycle: append([]string(nil), pending...),
	}
}

func loadDomainList(path string) ([]domain.Entry, string, error) {
	text, err := readText(path)
	if err != nil {
		return nil, "", err
	}
	entries, errs := domain.ParseList(text)
	if len(errs) > 0 {
		return nil, "", fmt.Errorf("invalid Domain List:\n%s", formatDomainErrors(errs))
	}
	return entries, text, nil
}

func readText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func lifecycleChanges(nextCATrusted, baselineCATrusted bool) []string {
	if nextCATrusted != baselineCATrusted {
		return []string{"ca-trusted"}
	}
	return nil
}

func formatDomainErrors(errs []domain.LineError) string {
	var lines []string
	for _, err := range errs {
		lines = append(lines, err.Error())
	}
	return strings.Join(lines, "\n")
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Entries = append([]domain.Entry(nil), snapshot.Entries...)
	snapshot.PendingLifecycle = append([]string(nil), snapshot.PendingLifecycle...)
	return snapshot
}
