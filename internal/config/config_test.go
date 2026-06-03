package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigMatchesPRD(t *testing.T) {
	cfg := Default()
	if cfg.ProxyListen != "127.0.0.1:8080" {
		t.Fatalf("ProxyListen = %q", cfg.ProxyListen)
	}
	if cfg.PACListen != "127.0.0.1:8079" {
		t.Fatalf("PACListen = %q", cfg.PACListen)
	}
	if cfg.ControlListen != "127.0.0.1:8078" {
		t.Fatalf("ControlListen = %q", cfg.ControlListen)
	}
	if !cfg.ManagedSystemProxy {
		t.Fatalf("ManagedSystemProxy = false")
	}
	if cfg.CATrusted {
		t.Fatalf("CATrusted = true")
	}
}

func TestValidateRejectsListenerURL(t *testing.T) {
	cfg := Default()
	cfg.ProxyListen = "http://127.0.0.1:8080"
	if err := Validate(cfg); err == nil {
		t.Fatal("expected listener URL to fail validation")
	}
}

func TestLoadOrBootstrapCreatesCommentedDefaultsAndAppliesOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var out bytes.Buffer

	loaded, err := LoadOrBootstrap("", Overrides{
		ProxyListen:  "127.0.0.1:18080",
		CATrusted:    true,
		CATrustedSet: true,
	}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Bootstrapped {
		t.Fatal("expected first-start bootstrap")
	}
	if loaded.Config.ProxyListen != "127.0.0.1:18080" {
		t.Fatalf("proxy-listen = %q", loaded.Config.ProxyListen)
	}
	if !loaded.Config.CATrusted {
		t.Fatal("ca-trusted override was not applied")
	}
	if loaded.DomainPath != filepath.Join(home, ".cors-gateway", "domains.txt") {
		t.Fatalf("domain path = %q", loaded.DomainPath)
	}
	configText, err := os.ReadFile(loaded.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(configText, []byte("# Local proxy endpoint")) {
		t.Fatalf("generated config is not commented:\n%s", configText)
	}
	if !bytes.Contains(out.Bytes(), []byte("Created:")) {
		t.Fatalf("bootstrap output = %q", out.String())
	}
}
