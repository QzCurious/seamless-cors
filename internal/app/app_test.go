package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"cors-vpn/internal/config"
	"cors-vpn/internal/domain"
)

type fakeAdapter struct {
	installedPAC string
	restored     int
	trusted      int
	removed      int
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
	return nil
}
func (f *fakeAdapter) RemoveCA() error {
	f.removed++
	return nil
}

func TestStartGuidanceShowsEditableFilesForManagedSystemProxy(t *testing.T) {
	cfg := config.Default()
	cfg.ManagedSystemProxy = true
	var out bytes.Buffer

	writeStartGuidance(&out, config.LoadResult{
		Config:     cfg,
		ConfigPath: "/Users/example/.cors-gateway/config.yaml",
		DomainPath: "/Users/example/.cors-gateway/domains.txt",
	})

	got := out.String()
	want := "Transparent CORS Gateway running\n" +
		"config: /Users/example/.cors-gateway/config.yaml\n" +
		"domain-list: /Users/example/.cors-gateway/domains.txt\n"
	if got != want {
		t.Fatalf("start guidance = %q", got)
	}
	for _, unwanted := range []string{"proxy-listen:", "pac-listen:", "control-listen:"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("start guidance included %q:\n%s", unwanted, got)
		}
	}
}

func TestStartGuidanceShowsProxyListenerForManualProxyMode(t *testing.T) {
	cfg := config.Default()
	cfg.ManagedSystemProxy = false
	var out bytes.Buffer

	writeStartGuidance(&out, config.LoadResult{
		Config:        cfg,
		ConfigPath:    "/Users/example/.cors-gateway/config.yaml",
		DomainPath:    "/Users/example/.cors-gateway/domains.txt",
		OverrideNames: []string{"managed-system-proxy"},
	})

	got := out.String()
	want := "Transparent CORS Gateway running\n" +
		"config: /Users/example/.cors-gateway/config.yaml\n" +
		"domain-list: /Users/example/.cors-gateway/domains.txt\n" +
		"proxy-listen: 127.0.0.1:8080\n" +
		"one-run overrides: managed-system-proxy\n"
	if got != want {
		t.Fatalf("start guidance = %q", got)
	}
	for _, unwanted := range []string{"pac-listen:", "control-listen:"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("start guidance included %q:\n%s", unwanted, got)
		}
	}
}

func TestRuntimeUsesAdapterAndCleansUpLifecycleState(t *testing.T) {
	entry, err := domain.ParseEntry("api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.ProxyListen = "127.0.0.1:0"
	cfg.PACListen = "127.0.0.1:0"
	cfg.ControlListen = "127.0.0.1:0"
	cfg.ManagedSystemProxy = true
	cfg.CATrusted = true
	fake := &fakeAdapter{}
	runtime, err := NewRuntime(cfg, []domain.Entry{entry}, fake, &bytes.Buffer{})
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
}
