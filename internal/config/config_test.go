package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigMatchesPRD(t *testing.T) {
	cfg := Default()
	if cfg.DomainList != "~/.seamless-cors/domains.txt" {
		t.Fatalf("DomainList = %q", cfg.DomainList)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("LogLevel = %q", cfg.LogLevel)
	}
	if cfg.CATrusted {
		t.Fatalf("CATrusted = true")
	}
}

func TestLoadIgnoresUnknownConfigKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, "config.yaml")
	domainPath := filepath.Join(home, "domains.txt")
	if err := os.WriteFile(configPath, []byte("unknown-setting: ignored\ndomain-list: "+domainPath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadExisting(configPath, Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DomainPath != domainPath {
		t.Fatalf("domain path = %q", loaded.DomainPath)
	}
}

func TestLoadOrBootstrapCreatesCommentedDefaultsAndAppliesOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var out bytes.Buffer

	loaded, err := LoadOrBootstrap("", Overrides{
		CATrusted:    true,
		CATrustedSet: true,
	}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Bootstrapped {
		t.Fatal("expected first-start bootstrap")
	}
	if !loaded.Config.CATrusted {
		t.Fatal("ca-trusted override was not applied")
	}
	if loaded.DomainPath != filepath.Join(home, ".seamless-cors", "domains.txt") {
		t.Fatalf("domain path = %q", loaded.DomainPath)
	}
	configText, err := os.ReadFile(loaded.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(configText, []byte("unknown-setting")) {
		t.Fatalf("generated config included runtime settings:\n%s", configText)
	}
	if !bytes.Contains(configText, []byte("# One domain or origin per line.")) {
		t.Fatalf("generated config is not commented:\n%s", configText)
	}
	for _, line := range []string{
		"# log-level: debug",
		"# log-level: info",
		"# log-level: warn",
		"# log-level: error",
	} {
		if !bytes.Contains(configText, []byte(line)) {
			t.Fatalf("generated config missing %q:\n%s", line, configText)
		}
	}
	if !bytes.Contains(out.Bytes(), []byte("Created:")) {
		t.Fatalf("bootstrap output = %q", out.String())
	}
}
