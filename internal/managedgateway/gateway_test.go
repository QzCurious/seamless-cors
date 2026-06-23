package managedgateway

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"seamless-cors/internal/config"
	"seamless-cors/internal/gatewaycoord"
	"seamless-cors/internal/gatewayfacade"
	"seamless-cors/internal/platform"
	"seamless-cors/internal/userca"
)

type fakeAdapter struct {
	installedPAC string
	pacStates    []platform.PACServiceState
	clearedPAC   int
	trusted      int
	cleanedCA    int
	caRecords    []platform.CARecord
	trustErr     error
	clearErr     error
	trustStarted chan struct{}
	releaseTrust chan struct{}
}

func (f *fakeAdapter) Capabilities() platform.CapabilityReport {
	return platform.CapabilityReport{
		Platform:          "test/test",
		Supported:         true,
		PACManagement:     platform.CapabilitySupported,
		CATrustManagement: platform.CapabilitySupported,
		RuntimeCleanup:    platform.CapabilitySupported,
	}
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
	return f.clearErr
}
func (f *fakeAdapter) TrustedCAs() ([]platform.CARecord, error) {
	return append([]platform.CARecord(nil), f.caRecords...), nil
}
func (f *fakeAdapter) TrustCA(certPEM []byte) error {
	f.trusted++
	if f.trustStarted != nil {
		close(f.trustStarted)
	}
	if f.releaseTrust != nil {
		<-f.releaseTrust
	}
	if f.trustErr == nil {
		fingerprint, err := userca.SHA1Fingerprint(certPEM)
		if err != nil {
			return err
		}
		block, _ := pem.Decode(certPEM)
		if block == nil {
			return errors.New("invalid test cert")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return err
		}
		f.caRecords = []platform.CARecord{{SHA1: fingerprint, CertPEM: certPEM, NotAfter: cert.NotAfter}}
	}
	return f.trustErr
}
func (f *fakeAdapter) RemoveCAs([]string) error {
	f.cleanedCA++
	f.caRecords = nil
	return nil
}

func TestLifecycleConsentPromptReportsManagedPACOnly(t *testing.T) {
	var out bytes.Buffer

	err := promptForLifecycleConsent(bytes.NewBufferString("yes"), &out, lifecycleConsentRequest{
		ManagedPAC: true,
		CurrentPACState: []platform.PACServiceState{{
			Name:    "Wi-Fi",
			URL:     "http://corp.example/proxy.pac",
			Enabled: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := out.String()
	for _, want := range []string{
		"Explicit Lifecycle Consent required",
		"Managed PAC Consent:",
		"Proceed? [y/N]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("consent prompt missing %q:\n%s", want, got)
		}
	}
}

func TestLifecycleConsentPromptDeclineCancels(t *testing.T) {
	var out bytes.Buffer

	err := promptForLifecycleConsent(bytes.NewBufferString("no"), &out, lifecycleConsentRequest{ManagedPAC: true})
	if !errors.Is(err, ErrLifecycleConsentDeclined) {
		t.Fatalf("prompt error = %v", err)
	}
	if !strings.Contains(out.String(), "Startup canceled") {
		t.Fatalf("prompt output = %q", out.String())
	}
}

func TestManagedGatewayUsesAdapterAndCleansUpLifecycleState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	domainPath := filepath.Join(configDir, "domains.txt")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigForRuntime(t, filepath.Join(configDir, "config.yaml"), config.Config{
		DomainList: domainPath,
		CATrusted:  true,
	})
	if err := os.WriteFile(domainPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAdapter{}
	var out bytes.Buffer

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (Gateway{Adapter: fake, Stdout: &out}).Start(ctx); err != nil {
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
	if !strings.Contains(out.String(), "Installed User CA added to the current user's SSL trust settings.") {
		t.Fatalf("success output = %q", out.String())
	}
}

func TestManagedGatewayPrintsCancelMessageWhenTrustApprovalDenied(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	domainPath := filepath.Join(configDir, "domains.txt")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigForRuntime(t, filepath.Join(configDir, "config.yaml"), config.Config{
		DomainList: domainPath,
		CATrusted:  true,
	})
	if err := os.WriteFile(domainPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAdapter{trustErr: platform.ErrTrustApprovalDenied}
	var out bytes.Buffer

	err := (Gateway{Adapter: fake, Stdout: &out}).Start(context.Background())
	if !errors.Is(err, platform.ErrTrustApprovalDenied) {
		t.Fatalf("serve error = %v", err)
	}

	want := "Certificate trust was not approved.\n" +
		"Run the command again and approve the system prompt.\n"
	if out.String() != want {
		t.Fatalf("cancel output = %q", out.String())
	}
	if fake.installedPAC != "" {
		t.Fatalf("trust denial installed PAC: %q", fake.installedPAC)
	}
}

func TestManagedGatewayWaitsForCATrustApprovalBeforeActivation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	domainPath := filepath.Join(configDir, "domains.txt")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigForRuntime(t, filepath.Join(configDir, "config.yaml"), config.Config{
		DomainList: domainPath,
		CATrusted:  true,
	})
	if err := os.WriteFile(domainPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAdapter{
		trustStarted: make(chan struct{}),
		releaseTrust: make(chan struct{}),
	}
	var out bytes.Buffer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- (Gateway{Adapter: fake, Stdout: &out}).Start(ctx) }()
	<-fake.trustStarted

	if fake.installedPAC != "" {
		t.Fatalf("pending trust approval installed PAC: %q", fake.installedPAC)
	}
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))
	if got := currentStatusOutput(t); !strings.Contains(got, "seamless-cors status: owner running") {
		t.Fatalf("pending trust approval status = %q", got)
	}
	if strings.Contains(out.String(), "seamless-cors running") {
		t.Fatalf("pending trust approval printed start guidance:\n%s", out.String())
	}

	close(fake.releaseTrust)
	waitForStatusOutput(t, "seamless-cors status: running")
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	got := out.String()
	caIndex := strings.Index(got, "Installed User CA added")
	runningIndex := strings.Index(got, "seamless-cors running")
	if caIndex < 0 || runningIndex < 0 || caIndex > runningIndex {
		t.Fatalf("start output order = %q", got)
	}
}

func TestStartRunsCleanupBeforeInvalidConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("domain-list: \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAdapter{}

	err := StartWithContextAndInput(context.Background(), bytes.NewBufferString(""), &bytes.Buffer{}, fake)
	if err == nil || !strings.Contains(err.Error(), "domain-list") {
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
		done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), &out, fake)
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

	err := StartWithContextAndInput(context.Background(), bytes.NewBufferString("no"), &out, fake)
	if !errors.Is(err, ErrLifecycleConsentDeclined) {
		t.Fatalf("start error = %v", err)
	}
	if fake.installedPAC != "" || fake.trusted != 0 {
		t.Fatalf("declined consent mutated OS state: PAC=%q trusted=%d", fake.installedPAC, fake.trusted)
	}
	if _, err := os.Stat(filepath.Join(configDir, "runtime", "gateway-state-cache.json")); !os.IsNotExist(err) {
		t.Fatalf("declined consent wrote runtime state: %v", err)
	}
	for _, want := range []string{"Managed PAC Consent:", "without restoring previous PAC state"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("consent output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "CA Trust Consent:") {
		t.Fatalf("CA trust should not use terminal lifecycle consent:\n%s", out.String())
	}
}

func TestStartWithVerifiedActiveGatewaySkipsCleanupAndConfigValidation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := config.Default()
	firstAdapter := &fakeAdapter{}
	configureGatewayForTest(t, cfg, "api.example.test\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, firstAdapter) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))
	waitForStatusOutput(t, "seamless-cors status: running")

	configDir := filepath.Join(home, ".seamless-cors")
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("domain-list: \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secondAdapter := &fakeAdapter{}
	var out bytes.Buffer

	if err := StartWithContextAndInput(context.Background(), bytes.NewBufferString(""), &out, secondAdapter); err != nil {
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

func TestServeOwnerPublishesRouterOnlyOwnerWithoutManagedPAC(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fake := &fakeAdapter{}
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)

	go func() { done <- (Gateway{Adapter: fake, Stdout: &out}).ServeOwner(ctx) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))
	waitForStatusOutput(t, "gateway-runtime: inactive")
	if status := currentStatusOutput(t); strings.Contains(status, "cleanup-needed") {
		t.Fatalf("router-only owner reported live cache as cleanup-needed:\n%s", status)
	}

	if fake.installedPAC != "" || fake.clearedPAC != 0 || fake.trusted != 0 {
		t.Fatalf("serve mutated lifecycle state: PAC=%q cleared=%d trusted=%d", fake.installedPAC, fake.clearedPAC, fake.trusted)
	}
	if running, err := Running(); err != nil {
		t.Fatal(err)
	} else if running {
		t.Fatal("router-only owner should not report Gateway Runtime as running")
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRouterHostedStartOutlivesHTTPStartRequest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	domainPath := filepath.Join(configDir, "domains.txt")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigForRuntime(t, filepath.Join(configDir, "config.yaml"), config.Config{
		DomainList: domainPath,
		CATrusted:  false,
	})
	if err := os.WriteFile(domainPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- (Gateway{Adapter: fake, Stdout: io.Discard}).ServeOwner(ctx) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))

	coord, err := gatewaycoord.Default()
	if err != nil {
		t.Fatal(err)
	}
	cache := coord.Verify().Cache
	var result gatewayfacade.StartResult
	if err := callJSON(http.MethodPost, cache, "/start", nil, &result); err != nil {
		t.Fatal(err)
	}
	if result.Kind != gatewayfacade.StartResultStarted {
		t.Fatalf("start result = %s", result.Kind)
	}
	waitForStatusOutput(t, "seamless-cors status: running")
	time.Sleep(100 * time.Millisecond)
	status := currentStatusOutput(t)
	if !strings.Contains(status, "seamless-cors status: running") {
		t.Fatalf("router-hosted start did not survive response:\n%s", status)
	}
	if strings.Contains(status, "cleanup-needed") {
		t.Fatalf("running owner reported live footprint as cleanup-needed:\n%s", status)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestStartWithRouterOnlyOwnerFailsWithoutDelegatingOrCleanup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ownerAdapter := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- (Gateway{Adapter: ownerAdapter, Stdout: io.Discard}).ServeOwner(ctx) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))

	startAdapter := &fakeAdapter{}
	var out bytes.Buffer
	if err := StartWithContextAndInput(context.Background(), bytes.NewBufferString(""), &out, startAdapter); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "gateway owner already running") {
		t.Fatalf("start output = %q", out.String())
	}
	if startAdapter.clearedPAC != 0 || startAdapter.installedPAC != "" {
		t.Fatalf("owner-blocked start mutated lifecycle state: cleared=%d PAC=%q", startAdapter.clearedPAC, startAdapter.installedPAC)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServeOwnerMayClaimStaleCacheWithoutGatewayFootprintCleanup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeStaleRuntimeState(t)
	fake := &fakeAdapter{
		pacStates: []platform.PACServiceState{{
			Name:    "Wi-Fi",
			URL:     "http://127.0.0.1:8079/seamless-cors.pac",
			Enabled: true,
		}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)

	go func() { done <- (Gateway{Adapter: fake, Stdout: io.Discard}).ServeOwner(ctx) }()
	waitForStatusOutput(t, "gateway-runtime: inactive")

	if fake.clearedPAC != 0 {
		t.Fatalf("serve cleaned PAC for stale cache: %d", fake.clearedPAC)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServeOwnerExitsWhenGatewayStateLeaseIsLost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- (Gateway{Adapter: &fakeAdapter{}, Stdout: io.Discard}).ServeOwner(ctx) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))
	runtimeDir, err := config.RuntimeDir()
	if err != nil {
		t.Fatal(err)
	}
	coord := gatewaycoord.New(runtimeDir)

	if err := coord.Remove(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve owner did not exit after lease loss")
	}
}

func TestRuntimeOwnerCleansUpWhenGatewayStateLeaseIsLost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	domainPath := filepath.Join(configDir, "domains.txt")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigForRuntime(t, filepath.Join(configDir, "config.yaml"), config.Config{
		DomainList: domainPath,
		CATrusted:  false,
	})
	if err := os.WriteFile(domainPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))
	runtimeDir, err := config.RuntimeDir()
	if err != nil {
		t.Fatal(err)
	}
	coord := gatewaycoord.New(runtimeDir)

	if err := coord.Remove(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime owner did not exit after lease loss")
	}
	if fake.clearedPAC == 0 {
		t.Fatal("runtime owner did not run Gateway Footprint Cleanup after lease loss")
	}
}

func TestStopReportsOwnerCleanupFailureAndKeepsOwnerForRetry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	domainPath := filepath.Join(configDir, "domains.txt")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigForRuntime(t, filepath.Join(configDir, "config.yaml"), config.Config{
		DomainList: domainPath,
		CATrusted:  false,
	})
	if err := os.WriteFile(domainPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))

	fake.clearErr = errors.New("pac cleanup denied")
	var out bytes.Buffer
	err := (Gateway{Adapter: fake, Stdout: &out}).Stop()
	if err == nil || !strings.Contains(err.Error(), "pac cleanup denied") {
		t.Fatalf("stop error = %v", err)
	}
	select {
	case err := <-done:
		t.Fatalf("owner exited after failed cleanup: %v", err)
	default:
	}
	if active, err := activeOwnerState(); err != nil {
		t.Fatal(err)
	} else if !active {
		t.Fatal("owner was not reachable after failed cleanup")
	}

	fake.clearErr = nil
	out.Reset()
	if err := (Gateway{Adapter: fake, Stdout: &out}).Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("owner did not exit after successful cleanup retry")
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

func TestStatusReportsStaleGatewayStateCacheWithoutCleanup(t *testing.T) {
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
	if !strings.Contains(out.String(), "stale Gateway State Cache detected") {
		t.Fatalf("status output = %q", out.String())
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("status removed runtime state: %v", err)
	}
}

func TestStopCleansStaleGatewayStateCache(t *testing.T) {
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
		t.Fatalf("stale Gateway State Cache still exists: %v", err)
	}
}

func TestManagedGatewayReloadsDomainListIntoGeneratedPAC(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	domainPath := filepath.Join(home, "domains.txt")
	cfg := config.Default()
	cfg.DomainList = domainPath

	configureGatewayForTest(t, cfg, "api-one.example.test\n")
	fake := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))

	pacURL := waitForInstalledPAC(t, fake)
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

func TestManagedGatewayStopsOnInvalidLiveDomainList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	domainPath := filepath.Join(home, "domains.txt")
	cfg := config.Default()
	cfg.DomainList = domainPath

	configureGatewayForTest(t, cfg, "api-one.example.test\n")
	fake := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))

	pacURL := waitForInstalledPAC(t, fake)
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

func TestManagedGatewayAppliesEmptyLiveDomainList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	domainPath := filepath.Join(home, "domains.txt")
	cfg := config.Default()
	cfg.DomainList = domainPath

	configureGatewayForTest(t, cfg, "api-one.example.test\n")
	fake := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))

	pacURL := waitForInstalledPAC(t, fake)
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

func TestManagedGatewayLiveConfigFollowsConfigDomainList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	firstDomainPath := filepath.Join(home, "first-domains.txt")
	secondDomainPath := filepath.Join(home, "second-domains.txt")
	configPath, err := config.DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(firstDomainPath, []byte("first.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondDomainPath, []byte("second-one.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DomainList = firstDomainPath
	cfg.SourcePath = configPath
	cfg.CATrusted = false
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigForRuntime(t, configPath, cfg)

	fake := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))

	pacURL := waitForInstalledPAC(t, fake)
	waitForHTTPBody(t, pacURL, "first.example.test")
	waitForStatusOutput(t, "domain-list: "+firstDomainPath)

	changed := config.Default()
	changed.DomainList = secondDomainPath
	changed.SourcePath = configPath
	changed.CATrusted = true
	writeConfigForRuntime(t, configPath, changed)
	waitForStatusOutput(t, "pending lifecycle changes: ca-trusted")
	if status := currentStatusOutput(t); !strings.Contains(status, "ca-trusted: false") {
		t.Fatalf("restart-applied lifecycle changed active CA trust:\n%s", status)
	}
	waitForStatusOutput(t, "domain-list: "+secondDomainPath)
	waitForHTTPBody(t, pacURL, "second-one.example.test")

	if err := os.WriteFile(secondDomainPath, []byte("second-two.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForHTTPBody(t, pacURL, "second-two.example.test")
	body := fetchHTTPBody(t, pacURL)
	if strings.Contains(body, "first.example.test") {
		t.Fatalf("old Domain List entry remained after config change:\n%s", body)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestManagedGatewayStopsOnInvalidLiveConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	domainPath := filepath.Join(home, "domains.txt")
	configPath, err := config.DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(domainPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DomainList = domainPath
	cfg.SourcePath = configPath

	writeConfigForRuntime(t, configPath, cfg)
	fake := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))

	if err := os.WriteFile(configPath, []byte("domain-list: \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "domain-list") {
			t.Fatalf("serve error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime did not stop after invalid live config")
	}
}

func configureGatewayForTest(t *testing.T, cfg config.Config, domainText string) {
	t.Helper()
	if cfg.SourcePath == "" {
		configPath, err := config.DefaultConfigPath()
		if err != nil {
			t.Fatal(err)
		}
		cfg.SourcePath = configPath
	}
	if cfg.DomainList == "" {
		cfg.DomainList = config.Default().DomainList
	}
	domainPath, err := config.ExpandPath(cfg.DomainList)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.SourcePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(domainPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if domainText == "" {
		domainText = "# no active domains\n"
	}
	if err := os.WriteFile(domainPath, []byte(domainText), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.DomainList = domainPath
	writeConfigForRuntime(t, cfg.SourcePath, cfg)
}

func writeConfigForRuntime(t *testing.T, path string, cfg config.Config) {
	t.Helper()
	text := "domain-list: " + cfg.DomainList + "\n" +
		"ca-trusted: " + map[bool]string{true: "true", false: "false"}[cfg.CATrusted] + "\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func restoreAdapter(t *testing.T, adapter platform.Adapter) {
	t.Helper()
	previous := platform.CurrentAdapter
	platform.CurrentAdapter = adapter
	t.Cleanup(func() {
		platform.CurrentAdapter = previous
	})
}

func writeStaleRuntimeState(t *testing.T) string {
	t.Helper()
	runtimeDir, err := config.RuntimeDir()
	if err != nil {
		t.Fatal(err)
	}
	coord := gatewaycoord.New(runtimeDir)
	err = coord.Write(gatewaycoord.GatewayStateCache{
		HTTPRouterListen: "127.0.0.1:1",
		Token:            "stale-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(runtimeDir, gatewaycoord.StateFileName)
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

func waitForInstalledPAC(t *testing.T, fake *fakeAdapter) string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if fake.installedPAC != "" {
			return fake.installedPAC
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for PAC install")
	return ""
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

func currentStatusOutput(t *testing.T) string {
	t.Helper()
	var out bytes.Buffer
	if err := Status(&out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	return out.String()
}
