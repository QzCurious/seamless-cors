package ca

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"seamless-cors/internal/platform"
)

type EphemeralAuthority struct {
	CertPath string
	KeyPath  string
	CertPEM  []byte
	KeyPEM   []byte
	cert     *x509.Certificate
	key      *rsa.PrivateKey
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
		Subject:               pkix.Name{CommonName: "seamless-cors Ephemeral User CA"},
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
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return nil, err
	}
	if err := adapter.TrustCA(certPEM); err != nil {
		return nil, err
	}
	return &EphemeralAuthority{
		CertPath: certPath,
		KeyPath:  keyPath,
		CertPEM:  certPEM,
		KeyPEM:   keyPEM,
		cert:     template,
		key:      key,
	}, nil
}

func (a *EphemeralAuthority) TLSCertificate() (tls.Certificate, error) {
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
