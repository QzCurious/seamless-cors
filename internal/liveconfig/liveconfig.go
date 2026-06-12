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

type Config struct {
	effective        config.Config
	configPath       string
	domainListPath   string
	entries          []domain.Entry
	pendingLifecycle []string
}

type Event struct {
	Config Config
	Err    error
}

type Source struct {
	mu                sync.RWMutex
	config            Config
	desiredConfig     config.Config
	baselineCATrusted bool
	lastConfigText    string
	lastDomainText    string
}

func LoadOrBootstrap(configPath string, stdout io.Writer) (*Source, Config, error) {
	loaded, err := config.LoadOrBootstrap(configPath, stdout)
	if err != nil {
		return nil, Config{}, err
	}
	entries, domainText, err := loadDomainList(loaded.DomainPath)
	if err != nil {
		return nil, Config{}, err
	}
	configText, err := readText(loaded.ConfigPath)
	if err != nil {
		return nil, Config{}, err
	}
	live := configFromLoadResult(loaded, entries, nil, loaded.Config.CATrusted)
	source := newSource(loaded.Config, live, configText, domainText)
	return source, live, nil
}

func newSource(desired config.Config, live Config, configText, domainText string) *Source {
	live = cloneConfig(live)
	return &Source{
		config:            live,
		desiredConfig:     desired,
		baselineCATrusted: live.CATrusted(),
		lastConfigText:    configText,
		lastDomainText:    domainText,
	}
}

func (s *Source) Config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConfig(s.config)
}

func (s *Source) Watch(ctx context.Context, interval time.Duration) <-chan Event {
	events := make(chan Event, 1)
	live := s.Config()
	if live.ConfigPath() == "" && live.DomainListPath() == "" {
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
	current := s.Config()
	var configText string
	var configChanged bool
	if current.ConfigPath() != "" {
		var configErr error
		configText, configErr = readText(current.ConfigPath())
		if configErr != nil {
			return Event{Err: fmt.Errorf("Fatal Config Error: %w", configErr)}, true
		}
		configChanged = configText != s.lastConfigText
	}
	domainPath := current.DomainListPath()
	var loaded config.LoadResult
	if configChanged {
		var err error
		loaded, err = config.LoadExisting(current.ConfigPath())
		if err != nil {
			return Event{Err: fmt.Errorf("Fatal Config Error: %w", err)}, true
		}
		domainPath = loaded.DomainPath
	} else {
		s.mu.RLock()
		desired := s.desiredConfig
		s.mu.RUnlock()
		loaded = config.LoadResult{
			Config:     desired,
			ConfigPath: current.ConfigPath(),
			DomainPath: current.DomainListPath(),
		}
	}
	domainText, domainErr := readText(domainPath)
	if domainErr != nil {
		return Event{Err: fmt.Errorf("Fatal Domain List Error: %w", domainErr)}, true
	}
	domainChanged := domainText != s.lastDomainText || domainPath != current.DomainListPath()
	if !configChanged && !domainChanged {
		return Event{}, false
	}
	entries, errs := domain.ParseList(domainText)
	if len(errs) > 0 {
		return Event{Err: fmt.Errorf("Fatal Domain List Error: invalid Domain List:\n%s", formatDomainErrors(errs))}, true
	}
	next := configFromLoadResult(loaded, entries, lifecycleChanges(loaded.Config.CATrusted, s.baselineCATrusted), s.baselineCATrusted)
	s.mu.Lock()
	s.config = next
	s.desiredConfig = loaded.Config
	s.lastConfigText = configText
	s.lastDomainText = domainText
	s.mu.Unlock()
	return Event{Config: cloneConfig(next)}, true
}

func configFromLoadResult(loaded config.LoadResult, entries []domain.Entry, pending []string, activeCATrusted bool) Config {
	effective := loaded.Config
	effective.SourcePath = loaded.ConfigPath
	effective.DomainList = loaded.DomainPath
	effective.CATrusted = activeCATrusted
	return Config{
		effective:        effective,
		configPath:       loaded.ConfigPath,
		domainListPath:   loaded.DomainPath,
		entries:          append([]domain.Entry(nil), entries...),
		pendingLifecycle: append([]string(nil), pending...),
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

func (c Config) Effective() config.Config {
	return c.effective
}

func (c Config) CATrusted() bool {
	return c.effective.CATrusted
}

func (c Config) Entries() []domain.Entry {
	return append([]domain.Entry(nil), c.entries...)
}

func (c Config) PendingLifecycle() []string {
	return append([]string(nil), c.pendingLifecycle...)
}

func (c Config) ConfigPath() string {
	return c.configPath
}

func (c Config) DomainListPath() string {
	return c.domainListPath
}

func cloneConfig(live Config) Config {
	live.entries = append([]domain.Entry(nil), live.entries...)
	live.pendingLifecycle = append([]string(nil), live.pendingLifecycle...)
	return live
}
