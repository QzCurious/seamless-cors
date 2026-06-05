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
	restored     int
	trusted      int
	removed      int
	trustErr     error
}

func (f *fakeAdapter) InstallPAC(url string) error {
	f.installedPAC = url
	return nil
}
func (f *fakeAdapter) RestoreProxy() error {
	f.restored++
	return nil
}
func (f *fakeAdapter) TrustCA([]byte) error {
	f.trusted++
	return f.trustErr
}
func (f *fakeAdapter) RemoveCA() error {
	f.removed++
	return nil
}

func TestStartGuidanceShowsEditableFilesAndManagedPAC(t *testing.T) {
	cfg := config.Default()
	var out bytes.Buffer

	writeStartGuidance(&out, config.LoadResult{
		Config:     cfg,
		ConfigPath: "/Users/example/.cors-gateway/config.yaml",
		DomainPath: "/Users/example/.cors-gateway/domains.txt",
	})

	got := out.String()
	want := "Transparent CORS Gateway running\n" +
		"config: /Users/example/.cors-gateway/config.yaml\n" +
		"domain-list: /Users/example/.cors-gateway/domains.txt\n" +
		"managed-pac: active\n"
	if got != want {
		t.Fatalf("start guidance = %q", got)
	}
	for _, unwanted := range []string{"proxy-listen:", "pac-listen:", "control-listen:"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("start guidance included %q:\n%s", unwanted, got)
		}
	}
}

func TestCATrustPromptWaitsWithApprovedCopy(t *testing.T) {
	var out bytes.Buffer

	if err := promptForCATrust(bytes.NewBufferString("x"), &out); err != nil {
		t.Fatal(err)
	}

	want := "ca-trusted: true requires adding the local CA certificate to the system trust settings.\n" +
		"You will see a system prompt asking for administrator approval to make this change.\n" +
		"\n" +
		"Press any key to continue...\n"
	if out.String() != want {
		t.Fatalf("CA trust prompt = %q", out.String())
	}
}

func TestCATrustPromptTreatsCtrlCAsCancel(t *testing.T) {
	var out bytes.Buffer

	err := promptForCATrust(bytes.NewBuffer([]byte{0x03}), &out)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("prompt error = %v", err)
	}
	if !strings.HasSuffix(out.String(), "Press any key to continue...\n") {
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
	if fake.restored != 1 {
		t.Fatalf("restore calls = %d", fake.restored)
	}
	if fake.removed != 1 {
		t.Fatalf("remove calls = %d", fake.removed)
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

func TestStatusAndStopDoNotBootstrapMissingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".cors-gateway")

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
	if !strings.Contains(out.String(), "no running gateway found") {
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
	if !strings.Contains(out.String(), "no running gateway found") {
		t.Fatalf("stop output = %q", out.String())
	}
	if !strings.Contains(out.String(), "cleaned stale gateway runtime state") {
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
	waitForFile(t, filepath.Join(home, ".cors-gateway", "runtime", "control-state.json"))
	waitForStatusOutput(t, "Transparent CORS Gateway status: running")

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
	waitForFile(t, filepath.Join(home, ".cors-gateway", "runtime", "control-state.json"))

	pacURL := "http://" + runtime.listeners[1].Addr().String() + "/proxy.pac"
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

func TestRuntimeIgnoresInvalidLiveDomainList(t *testing.T) {
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
	waitForFile(t, filepath.Join(home, ".cors-gateway", "runtime", "control-state.json"))

	pacURL := "http://" + runtime.listeners[1].Addr().String() + "/proxy.pac"
	waitForHTTPBody(t, pacURL, "api-one.example.test")

	if err := os.WriteFile(domainPath, []byte("api-two.example.test\nhttps://*.bad.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	body := fetchHTTPBody(t, pacURL)
	if !strings.Contains(body, "api-one.example.test") {
		t.Fatalf("invalid domain list replaced last good PAC body:\n%s", body)
	}
	if strings.Contains(body, "api-two.example.test") {
		t.Fatalf("invalid domain list was partially applied:\n%s", body)
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
	waitForFile(t, filepath.Join(home, ".cors-gateway", "runtime", "control-state.json"))

	pacURL := "http://" + runtime.listeners[1].Addr().String() + "/proxy.pac"
	waitForHTTPBody(t, pacURL, "override-one.example.test")

	changed := config.Default()
	changed.DomainList = configDomainPath
	changed.SourcePath = configPath
	changed.CATrusted = true
	writeConfigForRuntime(t, configPath, changed)
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
	waitForFile(t, filepath.Join(home, ".cors-gateway", "runtime", "control-state.json"))

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
	waitForFile(t, filepath.Join(home, ".cors-gateway", "runtime", "control-state.json"))

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
