package config

import "testing"

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
