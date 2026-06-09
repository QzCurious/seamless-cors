package ca

import (
	"os"
	"testing"

	"seamless-cors/internal/platform"
)

type fakeTrustStore struct {
	trusted int
	removed int
}

func (f *fakeTrustStore) InstallPAC(string) error { return nil }
func (f *fakeTrustStore) CurrentPACState() ([]platform.PACServiceState, error) {
	return nil, nil
}
func (f *fakeTrustStore) ClearOwnedPAC() error          { return nil }
func (f *fakeTrustStore) HasCAFootprint() (bool, error) { return false, nil }
func (f *fakeTrustStore) TrustCA([]byte) error {
	f.trusted++
	return nil
}
func (f *fakeTrustStore) CleanupCAFootprint() error {
	f.removed++
	return nil
}

func TestEphemeralAuthorityCreatesAndTrustsCA(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeTrustStore{}

	authority, err := Create(dir, fake)
	if err != nil {
		t.Fatal(err)
	}
	if fake.trusted != 1 {
		t.Fatalf("trust calls = %d", fake.trusted)
	}
	for _, path := range []string{authority.CertPath, authority.KeyPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}
}
