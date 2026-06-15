package app

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"seamless-cors/internal/config"
	"seamless-cors/internal/managedgateway"
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
	return nil
}
func (f *fakeAdapter) TrustedCAs() ([]platform.CARecord, error) {
	return append([]platform.CARecord(nil), f.caRecords...), nil
}
func (f *fakeAdapter) TrustCA(certPEM []byte) error {
	f.trusted++
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

func TestCheckReportsCapabilitiesWithoutCreatingHomeConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	restoreAdapter(t, &fakeAdapter{})
	var out bytes.Buffer

	if err := Check(&out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".seamless-cors")); !os.IsNotExist(err) {
		t.Fatalf("check created home config directory: %v", err)
	}
	for _, want := range []string{
		"platform: test/test",
		"supported: true",
		"pac-management: supported",
		"ca-trust-management: supported",
		"runtime-cleanup: supported",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("check output missing %q:\n%s", want, out.String())
		}
	}
}

func TestInstallIsConfigIndependentAndIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fake := &fakeAdapter{}
	restoreAdapter(t, fake)
	var out bytes.Buffer

	if err := Install(&out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".seamless-cors", "config.yaml")); !os.IsNotExist(err) {
		t.Fatalf("install created config: %v", err)
	}
	if fake.trusted != 1 {
		t.Fatalf("trust calls = %d", fake.trusted)
	}

	out.Reset()
	if err := Install(&out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if fake.trusted != 1 {
		t.Fatalf("idempotent install trust calls = %d", fake.trusted)
	}
	if !strings.Contains(out.String(), "Installed User CA is already usable.") {
		t.Fatalf("idempotent install output = %q", out.String())
	}
}

func TestUninstallRefusesWhileManagedGatewayIsRunning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	domainPath := filepath.Join(configDir, "domains.txt")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigForGateway(t, filepath.Join(configDir, "config.yaml"), config.Config{
		DomainList: domainPath,
		CATrusted:  false,
	})
	if err := os.WriteFile(domainPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAdapter{}
	restoreAdapter(t, fake)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- managedgateway.StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake)
	}()
	waitForFile(t, filepath.Join(configDir, "runtime", "control-state.json"))

	var out bytes.Buffer
	err := Uninstall(&out, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatalf("uninstall error = %v", err)
	}
	if !strings.Contains(out.String(), "stop it before uninstalling") {
		t.Fatalf("uninstall output = %q", out.String())
	}

	cancel()
	if err := <-done; err != nil {
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

func writeConfigForGateway(t *testing.T, path string, cfg config.Config) {
	t.Helper()
	text := "domain-list: " + cfg.DomainList + "\n" +
		"ca-trusted: " + map[bool]string{true: "true", false: "false"}[cfg.CATrusted] + "\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
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
