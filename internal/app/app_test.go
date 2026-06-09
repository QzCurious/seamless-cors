package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"seamless-cors/internal/config"
	"seamless-cors/internal/control"
	"seamless-cors/internal/domain"
	"seamless-cors/internal/platform"
)

type fakeAdapter struct {
	installedPAC string
	pacStates    []platform.PACServiceState
	clearedPAC   int
	trusted      int
	cleanedCA    int
	caFootprint  bool
	trustErr     error
}

func (f *fakeAdapter) InstallPAC(url string) error {
	f.installedPAC = url
	return nil
}
func (f *fakeAdapter) CurrentPACState() ([]platform.PACServiceState, error) {
	return f.pacStates, nil
}
func (f *fakeAdapter) ClearOwnedPAC() error {
	f.clearedPAC++
	return nil
}
func (f *fakeAdapter) HasCAFootprint() (bool, error) { return f.caFootprint, nil }
func (f *fakeAdapter) TrustCA([]byte) error {
	f.trusted++
	return f.trustErr
}
func (f *fakeAdapter) CleanupCAFootprint() error {
	f.cleanedCA++
	return nil
}

func TestStartGuidanceShowsEditableFilesAndManagedPAC(t *testing.T) {
	cfg := config.Default()
	var out bytes.Buffer

	writeStartGuidance(&out, config.LoadResult{
		Config:     cfg,
		ConfigPath: "/Users/example/.seamless-cors/config.yaml",
		DomainPath: "/Users/example/.seamless-cors/domains.txt",
	})

	got := out.String()
	want := "seamless-cors running\n" +
		"config: /Users/example/.seamless-cors/config.yaml\n" +
		"domain-list: /Users/example/.seamless-cors/domains.txt\n" +
		"managed-pac: active\n"
	if got != want {
		t.Fatalf("start guidance = %q", got)
	}
	for _, unwanted := range []string{"runtime-proxy-endpoint:", "runtime-pac-endpoint:", "runtime-control-endpoint:"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("start guidance included %q:\n%s", unwanted, got)
		}
	}
}

func TestLifecycleConsentPromptCombinesCATrustConsent(t *testing.T) {
	var out bytes.Buffer

	err := promptForLifecycleConsent(bytes.NewBufferString("yes"), &out, lifecycleConsentRequest{CATrust: true})
	if err != nil {
		t.Fatal(err)
	}

	got := out.String()
	for _, want := range []string{
		"Explicit Lifecycle Consent required",
		"CA Trust Consent:",
		"Proceed? [y/N]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("consent prompt missing %q:\n%s", want, got)
		}
	}
}

func TestLifecycleConsentPromptDeclineCancels(t *testing.T) {
	var out bytes.Buffer

	err := promptForLifecycleConsent(bytes.NewBufferString("no"), &out, lifecycleConsentRequest{CATrust: true})
	if !errors.Is(err, ErrLifecycleConsentDeclined) {
		t.Fatalf("prompt error = %v", err)
	}
	if !strings.Contains(out.String(), "Startup canceled") {
		t.Fatalf("prompt output = %q", out.String())
	}
}

func TestRuntimeUsesAdapterAndCleansUpLifecycleState(t *testing.T) {
	entry, err := domain.ParseEntry("api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.CATrusted = true
	fake := &fakeAdapter{}
	var out bytes.Buffer
	runtime, err := NewRuntime(cfg, []domain.Entry{entry}, fake, &out)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runtime.Serve(ctx); err != nil {
		t.Fatal(err)
	}

	if fake.installedPAC == "" {
		t.Fatal("managed runtime did not install PAC")
	}
	if fake.trusted != 1 {
		t.Fatalf("trusted calls = %d", fake.trusted)
	}
	if fake.clearedPAC == 0 {
		t.Fatalf("PAC cleanup calls = %d", fake.clearedPAC)
	}
	if fake.cleanedCA == 0 {
		t.Fatalf("CA cleanup calls = %d", fake.cleanedCA)
	}
	if !strings.Contains(out.String(), "Local CA certificate added to the system trust settings.") {
		t.Fatalf("success output = %q", out.String())
	}
}

func TestRuntimePrintsCancelMessageWhenTrustApprovalDenied(t *testing.T) {
	entry, err := domain.ParseEntry("api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.CATrusted = true
	fake := &fakeAdapter{trustErr: platform.ErrTrustApprovalDenied}
	var out bytes.Buffer
	runtime, err := NewRuntime(cfg, []domain.Entry{entry}, fake, &out)
	if err != nil {
		t.Fatal(err)
	}

	err = runtime.Serve(context.Background())
	if !errors.Is(err, platform.ErrTrustApprovalDenied) {
		t.Fatalf("serve error = %v", err)
	}

	want := "Certificate trust was not approved.\n" +
		"Run the command again and approve the system prompt, or set ca-trusted: false.\n"
	if out.String() != want {
		t.Fatalf("cancel output = %q", out.String())
	}
}

func TestStartRunsCleanupBeforeInvalidConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("log-level: nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAdapter{}

	err := StartWithContextAndInput(context.Background(), bytes.NewBufferString(""), &bytes.Buffer{}, config.Overrides{}, fake)
	if err == nil || !strings.Contains(err.Error(), "log-level") {
		t.Fatalf("start error = %v", err)
	}
	if fake.clearedPAC != 1 {
		t.Fatalf("cleanup before config validation calls = %d", fake.clearedPAC)
	}
	if fake.installedPAC != "" || fake.trusted != 0 {
		t.Fatalf("startup mutated OS state after invalid config: PAC=%q trusted=%d", fake.installedPAC, fake.trusted)
	}
}

func TestStartAllowsEmptyDomainListAndInstallsManagedPAC(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	domainPath := filepath.Join(configDir, "domains.txt")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigForRuntime(t, filepath.Join(configDir, "config.yaml"), config.Config{
		DomainList: domainPath,
		LogLevel:   "info",
		CATrusted:  false,
	})
	if err := os.WriteFile(domainPath, []byte("# add domains when needed\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAdapter{}
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)

	go func() {
		done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), &out, config.Overrides{}, fake)
	}()
	waitForStatusOutput(t, "domains: 0")
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if fake.installedPAC == "" {
		t.Fatal("empty Domain List start did not install managed PAC")
	}
	if !strings.Contains(out.String(), "seamless-cors running") {
		t.Fatalf("start output = %q", out.String())
	}
}

func TestStartDeclinedLifecycleConsentDoesNotMutateOSOrRuntimeState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	domainPath := filepath.Join(configDir, "domains.txt")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigForRuntime(t, filepath.Join(configDir, "config.yaml"), config.Config{
		DomainList: domainPath,
		LogLevel:   "info",
		CATrusted:  true,
	})
	if err := os.WriteFile(domainPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAdapter{
		pacStates: []platform.PACServiceState{{
			Name:    "Wi-Fi",
			URL:     "http://corp.example/proxy.pac",
			Enabled: true,
		}},
	}
	var out bytes.Buffer

	err := StartWithContextAndInput(context.Background(), bytes.NewBufferString("no"), &out, config.Overrides{}, fake)
	if !errors.Is(err, ErrLifecycleConsentDeclined) {
		t.Fatalf("start error = %v", err)
	}
	if fake.installedPAC != "" || fake.trusted != 0 {
		t.Fatalf("declined consent mutated OS state: PAC=%q trusted=%d", fake.installedPAC, fake.trusted)
	}
	if _, err := os.Stat(filepath.Join(configDir, "runtime", "control-state.json")); !os.IsNotExist(err) {
		t.Fatalf("declined consent wrote runtime state: %v", err)
	}
	for _, want := range []string{"Managed PAC Consent:", "CA Trust Consent:", "without restoring previous PAC state"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("consent output missing %q:\n%s", want, out.String())
		}
	}
}

func TestStartWithVerifiedActiveGatewaySkipsCleanupAndConfigValidation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	entry, err := domain.ParseEntry("api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	firstAdapter := &fakeAdapter{}
	first, err := NewRuntime(cfg, []domain.Entry{entry}, firstAdapter, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- first.Serve(ctx) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "control-state.json"))
	waitForStatusOutput(t, "seamless-cors status: running")

	configDir := filepath.Join(home, ".seamless-cors")
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("log-level: nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secondAdapter := &fakeAdapter{}
	var out bytes.Buffer

	if err := StartWithContextAndInput(context.Background(), bytes.NewBufferString(""), &out, config.Overrides{}, secondAdapter); err != nil {
		t.Fatal(err)
	}
	if secondAdapter.clearedPAC != 0 || secondAdapter.cleanedCA != 0 {
		t.Fatalf("active second start ran cleanup: PAC=%d CA=%d", secondAdapter.clearedPAC, secondAdapter.cleanedCA)
	}
	if !strings.Contains(out.String(), "already running") {
		t.Fatalf("start output = %q", out.String())
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestStatusAndStopDoNotBootstrapMissingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")

	var out bytes.Buffer
	if err := Status(&out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(configDir); !os.IsNotExist(err) {
		t.Fatalf("status created config directory: %v", err)
	}
	if !strings.Contains(out.String(), "not running") {
		t.Fatalf("status output = %q", out.String())
	}

	out.Reset()
	if err := Stop(&out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(configDir); !os.IsNotExist(err) {
		t.Fatalf("stop created config directory: %v", err)
	}
	if !strings.Contains(out.String(), "no running seamless-cors found") {
		t.Fatalf("stop output = %q", out.String())
	}
}

func TestStatusReportsStaleRuntimeStateWithoutCleanup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	statePath := writeStaleRuntimeState(t)

	var out bytes.Buffer
	if err := Status(&out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "not running") {
		t.Fatalf("status output = %q", out.String())
	}
	if !strings.Contains(out.String(), "stale runtime state detected") {
		t.Fatalf("status output = %q", out.String())
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("status removed runtime state: %v", err)
	}
}

func TestStopCleansStaleRuntimeState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	statePath := writeStaleRuntimeState(t)

	var out bytes.Buffer
	if err := Stop(&out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no running seamless-cors found") {
		t.Fatalf("stop output = %q", out.String())
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("stale runtime state still exists: %v", err)
	}
}

func TestRuntimeRejectsSecondUserInstance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	entry, err := domain.ParseEntry("api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()

	first, err := NewRuntime(cfg, []domain.Entry{entry}, &fakeAdapter{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- first.Serve(ctx) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "control-state.json"))
	waitForStatusOutput(t, "seamless-cors status: running")

	second, err := NewRuntime(cfg, []domain.Entry{entry}, &fakeAdapter{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	err = second.Serve(context.Background())
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second instance error = %v", err)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeReloadsDomainListIntoGeneratedPAC(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	domainPath := filepath.Join(home, "domains.txt")
	if err := os.WriteFile(domainPath, []byte("api-one.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, errs, err := loadDomainList(domainPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("domain errors = %v", errs)
	}
	cfg := config.Default()
	cfg.DomainList = domainPath

	runtime, err := NewRuntime(cfg, entries, &fakeAdapter{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Serve(ctx) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "control-state.json"))

	pacURL := "http://" + runtime.listeners[1].Addr().String() + "/seamless-cors.pac"
	waitForHTTPBody(t, pacURL, "api-one.example.test")

	if err := os.WriteFile(domainPath, []byte("api-two.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForHTTPBody(t, pacURL, "api-two.example.test")

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeStopsOnInvalidLiveDomainList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	domainPath := filepath.Join(home, "domains.txt")
	if err := os.WriteFile(domainPath, []byte("api-one.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, errs, err := loadDomainList(domainPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("domain errors = %v", errs)
	}
	cfg := config.Default()
	cfg.DomainList = domainPath

	runtime, err := NewRuntime(cfg, entries, &fakeAdapter{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Serve(ctx) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "control-state.json"))

	pacURL := "http://" + runtime.listeners[1].Addr().String() + "/seamless-cors.pac"
	waitForHTTPBody(t, pacURL, "api-one.example.test")

	if err := os.WriteFile(domainPath, []byte("api-two.example.test\nhttps://*.bad.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "Fatal Domain List Error") {
			t.Fatalf("serve error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime did not stop after invalid live Domain List")
	}
	cancel()
}

func TestRuntimeAppliesEmptyLiveDomainList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	domainPath := filepath.Join(home, "domains.txt")
	if err := os.WriteFile(domainPath, []byte("api-one.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, errs, err := loadDomainList(domainPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("domain errors = %v", errs)
	}
	cfg := config.Default()
	cfg.DomainList = domainPath

	runtime, err := NewRuntime(cfg, entries, &fakeAdapter{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Serve(ctx) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "control-state.json"))

	pacURL := "http://" + runtime.listeners[1].Addr().String() + "/seamless-cors.pac"
	waitForHTTPBody(t, pacURL, "api-one.example.test")

	if err := os.WriteFile(domainPath, []byte("# no active domains\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForStatusOutput(t, "domains: 0")
	body := fetchHTTPBody(t, pacURL)
	if strings.Contains(body, "api-one.example.test") {
		t.Fatalf("empty Domain List kept stale PAC route:\n%s", body)
	}
	if !strings.Contains(body, "return 'DIRECT'") {
		t.Fatalf("empty Domain List PAC should fall through to DIRECT:\n%s", body)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeLiveConfigPreservesDomainListOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDomainPath := filepath.Join(home, "config-domains.txt")
	overrideDomainPath := filepath.Join(home, "override-domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(configDomainPath, []byte("config.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overrideDomainPath, []byte("override-one.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DomainList = configDomainPath
	cfg.SourcePath = configPath
	writeConfigForRuntime(t, configPath, cfg)

	cfg.DomainList = overrideDomainPath
	entries, errs, err := loadDomainList(overrideDomainPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("domain errors = %v", errs)
	}
	runtime, err := NewRuntime(cfg, entries, &fakeAdapter{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	runtime.overrides = config.Overrides{DomainList: overrideDomainPath}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Serve(ctx) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "control-state.json"))

	pacURL := "http://" + runtime.listeners[1].Addr().String() + "/seamless-cors.pac"
	waitForHTTPBody(t, pacURL, "override-one.example.test")
	waitForStatusOutput(t, "domain-list: "+overrideDomainPath)

	changed := config.Default()
	changed.DomainList = configDomainPath
	changed.LogLevel = "debug"
	changed.SourcePath = configPath
	changed.CATrusted = true
	writeConfigForRuntime(t, configPath, changed)
	waitForStatusOutput(t, "log-level: debug")
	waitForStatusOutput(t, "pending lifecycle changes: ca-trusted")

	if err := os.WriteFile(overrideDomainPath, []byte("override-two.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForHTTPBody(t, pacURL, "override-two.example.test")
	body := fetchHTTPBody(t, pacURL)
	if strings.Contains(body, "config.example.test") {
		t.Fatalf("config domain list replaced override domain list:\n%s", body)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeStopsOnInvalidLiveConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(domainPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("domain-list: "+domainPath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry, err := domain.ParseEntry("api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DomainList = domainPath
	cfg.SourcePath = configPath

	runtime, err := NewRuntime(cfg, []domain.Entry{entry}, &fakeAdapter{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Serve(ctx) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "control-state.json"))

	if err := os.WriteFile(configPath, []byte("log-level: nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "log-level") {
			t.Fatalf("serve error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime did not stop after invalid live config")
	}
}

func TestStatusReportsPendingLifecycleChanges(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(domainPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DomainList = domainPath
	cfg.SourcePath = configPath
	writeConfigForRuntime(t, configPath, cfg)
	entry, err := domain.ParseEntry("api.example.test")
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := NewRuntime(cfg, []domain.Entry{entry}, &fakeAdapter{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Serve(ctx) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "control-state.json"))

	changed := cfg
	changed.CATrusted = true
	writeConfigForRuntime(t, configPath, changed)

	waitForStatusOutput(t, "pending lifecycle changes: ca-trusted")
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func writeConfigForRuntime(t *testing.T, path string, cfg config.Config) {
	t.Helper()
	text := "domain-list: " + cfg.DomainList + "\n" +
		"log-level: " + cfg.LogLevel + "\n" +
		"ca-trusted: " + map[bool]string{true: "true", false: "false"}[cfg.CATrusted] + "\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeStaleRuntimeState(t *testing.T) string {
	t.Helper()
	runtimeDir, err := config.RuntimeDir()
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(runtimeDir, "control-state.json")
	err = control.WriteRuntimeState(statePath, control.RuntimeState{
		State: control.State{
			ControlListen: "127.0.0.1:1",
		},
		Token: "stale-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	return statePath
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func waitForHTTPBody(t *testing.T, url, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			last = string(body)
			if strings.Contains(last, want) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in %s; last body:\n%s", want, url, last)
}

func fetchHTTPBody(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func waitForStatusOutput(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		var out bytes.Buffer
		if err := Status(&out, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		last = out.String()
		if strings.Contains(last, want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in status; last output:\n%s", want, last)
}
