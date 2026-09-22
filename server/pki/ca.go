package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

const (
	DefaultDir = "secret"
	CertFile   = "ca.crt"
	KeyFile    = "ca.key"
	caCN       = "rbac-root-ca"
	caValidFor = 10 * 365 * 24 * time.Hour
)

type Bundle struct {
	Cert *x509.Certificate
	Key  *ecdsa.PrivateKey
	Dir  string
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("pki: stat %s: %w", path, err)
}

func CertPath(dir string) string {
	if dir == "" {
		dir = DefaultDir
	}
	return filepath.Join(dir, CertFile)
}

func KeyPath(dir string) string {
	if dir == "" {
		dir = DefaultDir
	}
	return filepath.Join(dir, KeyFile)
}

// Ensure loads an existing self-signed CA from dir, or generates one if both
// files are missing. Filenames are fixed (ca.crt / ca.key).
func Ensure(dir string) (*Bundle, error) {
	if dir == "" {
		dir = DefaultDir
	}
	crtPath, keyPath := CertPath(dir), KeyPath(dir)
	crtOK, err := fileExists(crtPath)
	if err != nil {
		return nil, err
	}
	keyOK, err := fileExists(keyPath)
	if err != nil {
		return nil, err
	}

	switch {
	case crtOK && keyOK:
		return load(dir)
	case !crtOK && !keyOK:
		b, err := generate()
		if err != nil {
			return nil, err
		}
		b.Dir = dir
		if err := b.write(); err != nil {
			return nil, err
		}
		return b, nil
	case keyOK && !crtOK:
		key, err := loadKey(keyPath)
		if err != nil {
			return nil, err
		}
		cert, err := selfSign(key)
		if err != nil {
			return nil, err
		}
		b := &Bundle{Cert: cert, Key: key, Dir: dir}
		if err := writeCert(crtPath, cert); err != nil {
			return nil, err
		}
		return b, nil
	default:
		return nil, fmt.Errorf("pki: %s exists but %s is missing; refuse to replace CA", crtPath, keyPath)
	}
}

func generate() (*Bundle, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("pki: generate key: %w", err)
	}
	cert, err := selfSign(key)
	if err != nil {
		return nil, err
	}
	return &Bundle{Cert: cert, Key: key}, nil
}

func selfSign(key *ecdsa.PrivateKey) (*x509.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("pki: serial: %w", err)
	}
	now := time.Now().Add(-time.Hour)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"rbac"},
			CommonName:   caCN,
		},
		NotBefore:             now,
		NotAfter:              now.Add(caValidFor),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
		MaxPathLenZero:        false,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("pki: create cert: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("pki: parse cert: %w", err)
	}
	return cert, nil
}

func load(dir string) (*Bundle, error) {
	cert, err := loadCert(CertPath(dir))
	if err != nil {
		return nil, err
	}
	key, err := loadKey(KeyPath(dir))
	if err != nil {
		return nil, err
	}
	if !equalPub(&key.PublicKey, cert.PublicKey) {
		return nil, fmt.Errorf("pki: ca.crt does not match ca.key")
	}
	return &Bundle{Cert: cert, Key: key, Dir: dir}, nil
}

func equalPub(a *ecdsa.PublicKey, pub any) bool {
	b, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return false
	}
	return a.Equal(b)
}

func loadCert(path string) (*x509.Certificate, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("pki: read %s: %w", path, err)
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("pki: %s is not a PEM certificate", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("pki: parse %s: %w", path, err)
	}
	return cert, nil
}

func loadKey(path string) (*ecdsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("pki: read %s: %w", path, err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("pki: %s is not PEM", path)
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		pk, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return nil, fmt.Errorf("pki: parse %s: %w", path, err)
		}
		ec, ok := pk.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("pki: %s is not an EC key", path)
		}
		return ec, nil
	}
	return key, nil
}

func (b *Bundle) write() error {
	if err := os.MkdirAll(b.Dir, 0o700); err != nil {
		return fmt.Errorf("pki: mkdir %s: %w", b.Dir, err)
	}
	if err := writeCert(CertPath(b.Dir), b.Cert); err != nil {
		return err
	}
	return writeKey(KeyPath(b.Dir), b.Key)
}

func writeCert(path string, cert *x509.Certificate) error {
	return atomicWrite(path, 0o644, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
}

func writeKey(path string, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("pki: marshal key: %w", err)
	}
	return atomicWrite(path, 0o600, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
}

func atomicWrite(path string, mode os.FileMode, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".pki-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
