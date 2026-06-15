package userca

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"seamless-cors/internal/platform"
)

const (
	CommonName      = "seamless-cors Installed User CA"
	CertFileName    = "root-ca.pem"
	KeyFileName     = "root-ca-key.pem"
	Validity        = 5 * 365 * 24 * time.Hour
	RenewalWindow   = 30 * 24 * time.Hour
	LeafValidity    = 30 * 24 * time.Hour
	LeafCacheMaxAge = 24 * time.Hour
)

type TrustStore interface {
	TrustedCAs() ([]platform.CARecord, error)
	TrustCA(certPEM []byte) error
	RemoveCAs(fingerprints []string) error
}

type Health string

const (
	HealthUsable             Health = "usable"
	HealthMissing            Health = "missing"
	HealthExpired            Health = "expired"
	HealthExpiringSoon       Health = "expiring-soon"
	HealthInvalid            Health = "invalid"
	HealthMultiple           Health = "multiple"
	HealthMismatchedMaterial Health = "mismatched-material"
	HealthUnsupported        Health = "unsupported"
	HealthUnknown            Health = "unknown"
)

type Report struct {
	Health  Health
	Expires time.Time
}

type EnsureResult struct {
	Report
	Changed bool
}

type Authority struct {
	CertPath string
	KeyPath  string
	CertPEM  []byte
	KeyPEM   []byte
	cert     *x509.Certificate
	key      *rsa.PrivateKey
}

func Inspect(dir string, store TrustStore) (Report, error) {
	report, _, err := inspect(dir, store, false)
	return report, err
}

func Ensure(dir string, store TrustStore) (*Authority, EnsureResult, error) {
	report, authority, err := inspect(dir, store, true)
	if err != nil {
		return nil, EnsureResult{Report: report}, err
	}
	if report.Health == HealthUsable {
		return authority, EnsureResult{Report: report}, nil
	}
	if err := Uninstall(dir, store); err != nil {
		return nil, EnsureResult{Report: report}, err
	}
	authority, err = createFresh(dir, store)
	if err != nil {
		return nil, EnsureResult{Report: report}, err
	}
	return authority, EnsureResult{
		Report: Report{
			Health:  HealthUsable,
			Expires: authority.cert.NotAfter,
		},
		Changed: true,
	}, nil
}

func Uninstall(dir string, store TrustStore) error {
	records, trustErr := store.TrustedCAs()
	var fingerprints []string
	for _, record := range records {
		fingerprints = append(fingerprints, record.SHA1)
	}
	removeErr := store.RemoveCAs(fingerprints)
	fileErr := os.RemoveAll(dir)
	return errors.Join(trustErr, removeErr, fileErr)
}

func Load(dir string) (*Authority, error) {
	certPath := filepath.Join(dir, CertFileName)
	keyPath := filepath.Join(dir, KeyFileName)
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	return parseAuthority(certPath, keyPath, certPEM, keyPEM)
}

func (a *Authority) TLSCertificate() (tls.Certificate, error) {
	cert, err := tls.X509KeyPair(a.CertPEM, a.KeyPEM)
	if err != nil {
		return tls.Certificate{}, err
	}
	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return tls.Certificate{}, err
	}
	return cert, nil
}

func inspect(dir string, store TrustStore, repairPermissions bool) (Report, *Authority, error) {
	records, err := store.TrustedCAs()
	if err != nil {
		return Report{Health: HealthUnknown}, nil, err
	}
	switch len(records) {
	case 0:
		return Report{Health: HealthMissing}, nil, nil
	case 1:
	default:
		return Report{Health: HealthMultiple}, nil, nil
	}
	authority, err := Load(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return Report{Health: HealthMismatchedMaterial}, nil, nil
		}
		return Report{Health: HealthInvalid}, nil, nil
	}
	fingerprint, err := SHA1Fingerprint(authority.CertPEM)
	if err != nil || fingerprint != records[0].SHA1 {
		return Report{Health: HealthMismatchedMaterial}, nil, nil
	}
	now := time.Now()
	if !now.Before(authority.cert.NotAfter) {
		return Report{Health: HealthExpired, Expires: authority.cert.NotAfter}, nil, nil
	}
	if now.Add(RenewalWindow).After(authority.cert.NotAfter) {
		return Report{Health: HealthExpiringSoon, Expires: authority.cert.NotAfter}, nil, nil
	}
	if repairPermissions {
		if err := repairAuthorityPermissions(dir, authority); err != nil {
			return Report{Health: HealthInvalid, Expires: authority.cert.NotAfter}, nil, err
		}
	}
	return Report{Health: HealthUsable, Expires: authority.cert.NotAfter}, authority, nil
}

var chmod = os.Chmod

func repairAuthorityPermissions(dir string, authority *Authority) error {
	return errors.Join(
		chmod(dir, 0o700),
		chmod(authority.CertPath, 0o600),
		chmod(authority.KeyPath, 0o600),
	)
}

func createFresh(dir string, store TrustStore) (*Authority, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: CommonName},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(Validity),
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
	certPath := filepath.Join(dir, CertFileName)
	keyPath := filepath.Join(dir, KeyFileName)
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return nil, err
	}
	if err := store.TrustCA(certPEM); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return &Authority{
		CertPath: certPath,
		KeyPath:  keyPath,
		CertPEM:  certPEM,
		KeyPEM:   keyPEM,
		cert:     template,
		key:      key,
	}, nil
}

func parseAuthority(certPath, keyPath string, certPEM, keyPEM []byte) (*Authority, error) {
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("CA certificate PEM is invalid")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, err
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != "RSA PRIVATE KEY" {
		return nil, fmt.Errorf("CA key PEM is invalid")
	}
	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, err
	}
	if cert.Subject.CommonName != CommonName || !cert.IsCA || !cert.BasicConstraintsValid {
		return nil, fmt.Errorf("CA certificate identity is invalid")
	}
	certKey, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("CA certificate public key is not RSA")
	}
	if certKey.N.Cmp(key.PublicKey.N) != 0 || certKey.E != key.PublicKey.E {
		return nil, fmt.Errorf("CA certificate and key do not match")
	}
	return &Authority{
		CertPath: certPath,
		KeyPath:  keyPath,
		CertPEM:  certPEM,
		KeyPEM:   keyPEM,
		cert:     cert,
		key:      key,
	}, nil
}

func SHA1Fingerprint(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("CA certificate PEM is invalid")
	}
	sum := sha1.Sum(block.Bytes)
	return strings.ToUpper(hex.EncodeToString(sum[:])), nil
}
