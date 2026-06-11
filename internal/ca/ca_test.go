package ca

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"seamless-cors/internal/platform"
)

type fakeTrustStore struct {
	records []platform.CARecord
	trusted int
	removed int
}

func (f *fakeTrustStore) TrustedCAs() ([]platform.CARecord, error) {
	return append([]platform.CARecord(nil), f.records...), nil
}

func (f *fakeTrustStore) TrustCA(certPEM []byte) error {
	f.trusted++
	fingerprint, err := SHA1Fingerprint(certPEM)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	f.records = []platform.CARecord{{
		SHA1:     fingerprint,
		CertPEM:  certPEM,
		NotAfter: cert.NotAfter,
	}}
	return nil
}

func (f *fakeTrustStore) RemoveCAs([]string) error {
	f.removed++
	f.records = nil
	return nil
}

func TestEnsureInstallsMissingCA(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeTrustStore{}

	authority, result, err := Ensure(dir, fake)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("missing CA should install fresh material")
	}
	if fake.trusted != 1 || fake.removed != 1 {
		t.Fatalf("trust lifecycle calls: trusted=%d removed=%d", fake.trusted, fake.removed)
	}
	for _, path := range []string{authority.CertPath, authority.KeyPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}
}

func TestEnsureReusesUsableCAAndRepairsPermissions(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeTrustStore{}
	authority, _, err := Ensure(dir, fake)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(authority.KeyPath, 0o644); err != nil {
		t.Fatal(err)
	}

	reused, result, err := Ensure(dir, fake)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatal("usable CA should not be replaced")
	}
	if reused.CertPath != authority.CertPath {
		t.Fatalf("reused cert path = %q", reused.CertPath)
	}
	info, err := os.Stat(authority.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("key mode = %#o", got)
	}
}

func TestEnsureFailsWhenPermissionRepairFails(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeTrustStore{}
	if _, _, err := Ensure(dir, fake); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("chmod failed")
	originalChmod := chmod
	chmod = func(string, os.FileMode) error {
		return wantErr
	}
	defer func() {
		chmod = originalChmod
	}()

	authority, _, err := Ensure(dir, fake)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Ensure error = %v, want %v", err, wantErr)
	}
	if authority != nil {
		t.Fatal("CA with unrepaired permissions should not be reused")
	}
}

func TestEnsureReplacesMismatchedMaterial(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeTrustStore{}
	if _, _, err := Ensure(dir, fake); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, KeyFileName), []byte("bad key"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, result, err := Ensure(dir, fake)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("mismatched material should be replaced")
	}
	if fake.removed < 2 {
		t.Fatalf("remove calls = %d", fake.removed)
	}
}

func TestUninstallRemovesTrustAndLocalMaterial(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeTrustStore{}
	if _, _, err := Ensure(dir, fake); err != nil {
		t.Fatal(err)
	}

	if err := Uninstall(dir, fake); err != nil {
		t.Fatal(err)
	}
	if len(fake.records) != 0 {
		t.Fatalf("trusted records remained: %d", len(fake.records))
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("CA dir remained: %v", err)
	}
}

func TestAuthorityExposesTLSCertificate(t *testing.T) {
	authority, _, err := Ensure(t.TempDir(), &fakeTrustStore{})
	if err != nil {
		t.Fatal(err)
	}

	cert, err := authority.TLSCertificate()
	if err != nil {
		t.Fatal(err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("missing certificate chain")
	}
	if cert.PrivateKey == nil {
		t.Fatal("missing private key")
	}
	if cert.Leaf == nil {
		t.Fatal("missing parsed leaf")
	}
	if !cert.Leaf.IsCA {
		t.Fatal("leaf is not a CA certificate")
	}
}
