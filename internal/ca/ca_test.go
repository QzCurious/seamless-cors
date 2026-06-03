package ca

import (
	"os"
	"testing"
)

type fakeTrustStore struct {
	trusted int
	removed int
}

func (f *fakeTrustStore) InstallPAC(string) error { return nil }
func (f *fakeTrustStore) RestoreProxy() error     { return nil }
func (f *fakeTrustStore) TrustCA([]byte) error {
	f.trusted++
	return nil
}
func (f *fakeTrustStore) RemoveCA() error {
	f.removed++
	return nil
}

func TestEphemeralAuthorityCreatesTrustsAndFullyRemovesCA(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeTrustStore{}

	authority, err := Create(dir, fake)
	if err != nil {
		t.Fatal(err)
	}
	if fake.trusted != 1 {
		t.Fatalf("trust calls = %d", fake.trusted)
	}
	for _, path := range []string{authority.CertPath, authority.KeyPath, authority.MarkerPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}

	if err := Remove(authority, fake); err != nil {
		t.Fatal(err)
	}
	if fake.removed != 1 {
		t.Fatalf("remove calls = %d", fake.removed)
	}
	for _, path := range []string{authority.CertPath, authority.KeyPath, authority.MarkerPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err=%v", path, err)
		}
	}
}

func TestCARecoveryOnlyUsesCAMarker(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeTrustStore{}
	authority, err := Create(dir, fake)
	if err != nil {
		t.Fatal(err)
	}

	if err := Recover(authority.MarkerPath, fake); err != nil {
		t.Fatal(err)
	}
	if fake.removed != 1 {
		t.Fatalf("remove calls = %d", fake.removed)
	}
	if _, err := os.Stat(authority.CertPath); !os.IsNotExist(err) {
		t.Fatalf("cert should be recovered/removed, err=%v", err)
	}
}
