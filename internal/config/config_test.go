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
	if cfg.CATrusted {
		t.Fatalf("CATrusted = true")
	}
}

func TestLoadIgnoresUnknownConfigKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, "config.yaml")
	domainPath := filepath.Join(home, "domains.txt")
	if err := os.WriteFile(configPath, []byte("unknown-setting: ignored\nlog-level: ignored\ndomain-list: "+domainPath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadExisting(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DomainPath != domainPath {
		t.Fatalf("domain path = %q", loaded.DomainPath)
	}
}

func TestLoadOrBootstrapCreatesCommentedDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var out bytes.Buffer

	loaded, err := LoadOrBootstrap("", &out)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Bootstrapped {
		t.Fatal("expected first-start bootstrap")
	}
	if loaded.Config.CATrusted {
		t.Fatal("ca-trusted default should require config edit")
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
	if bytes.Contains(configText, []byte("log-level")) {
		t.Fatalf("generated config included obsolete log-level setting:\n%s", configText)
	}
	if !bytes.Contains(out.Bytes(), []byte("Created:")) {
		t.Fatalf("bootstrap output = %q", out.String())
	}
	if bytes.Contains(out.Bytes(), []byte("Add at least one domain")) {
		t.Fatalf("bootstrap output treated empty Domain List as invalid: %q", out.String())
	}
}
