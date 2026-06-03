package ca

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"cors-vpn/internal/platform"
	"cors-vpn/internal/recovery"
)

type EphemeralAuthority struct {
	MarkerPath string
	CertPath   string
	KeyPath    string
	CertPEM    []byte
}

func Create(dir string, adapter platform.Adapter) (*EphemeralAuthority, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "Transparent CORS Gateway Ephemeral User CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPath := filepath.Join(dir, "ephemeral-ca.pem")
	keyPath := filepath.Join(dir, "ephemeral-ca-key.pem")
	markerPath := filepath.Join(dir, "ca-marker.json")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return nil, err
	}
	if err := adapter.TrustCA(certPEM); err != nil {
		return nil, err
	}
	if err := recovery.WriteMarker(markerPath, recovery.Marker{Kind: "ca", Path: certPath, Files: []string{certPath, keyPath}}); err != nil {
		return nil, err
	}
	return &EphemeralAuthority{MarkerPath: markerPath, CertPath: certPath, KeyPath: keyPath, CertPEM: certPEM}, nil
}

func Remove(authority *EphemeralAuthority, adapter platform.Adapter) error {
	if authority == nil {
		return nil
	}
	_ = adapter.RemoveCA()
	_ = os.Remove(authority.CertPath)
	_ = os.Remove(authority.KeyPath)
	_ = os.Remove(authority.MarkerPath)
	return nil
}

func Recover(markerPath string, adapter platform.Adapter) error {
	marker, err := recovery.ReadMarker(markerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if marker.Kind != "ca" {
		return nil
	}
	_ = adapter.RemoveCA()
	for _, file := range marker.Files {
		_ = os.Remove(file)
	}
	_ = os.Remove(markerPath)
	return nil
}
