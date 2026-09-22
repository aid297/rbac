package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"path/filepath"
	"time"
)

const (
	ServerCertFile = "server.crt"
	ServerKeyFile  = "server.key"
	serverCN       = "rbac-https"
	serverValidFor = 825 * 24 * time.Hour
)

func ServerCertPath(dir string) string {
	if dir == "" {
		dir = DefaultDir
	}
	return filepath.Join(dir, ServerCertFile)
}

func ServerKeyPath(dir string) string {
	if dir == "" {
		dir = DefaultDir
	}
	return filepath.Join(dir, ServerKeyFile)
}

// EnsureServer loads or issues a CA-signed server certificate for HTTPS.
// host is the listen IP or DNS name from server.http.host; loopback SANs are always added.
func EnsureServer(ca *Bundle, dir, host string) (*Bundle, error) {
	if ca == nil || ca.Cert == nil || ca.Key == nil {
		return nil, fmt.Errorf("pki: CA required to issue server certificate")
	}
	if dir == "" {
		dir = DefaultDir
	}
	crtPath, keyPath := ServerCertPath(dir), ServerKeyPath(dir)
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
		return loadServer(dir)
	case !crtOK && !keyOK:
		return issueAndWrite(ca, dir, host)
	case keyOK && !crtOK:
		key, err := loadKey(keyPath)
		if err != nil {
			return nil, err
		}
		cert, err := signServer(ca, key, host)
		if err != nil {
			return nil, err
		}
		if err := writeCert(crtPath, cert); err != nil {
			return nil, err
		}
		return &Bundle{Cert: cert, Key: key, Dir: dir}, nil
	default:
		return nil, fmt.Errorf("pki: %s exists but %s is missing; refuse to replace server key", crtPath, keyPath)
	}
}

func loadServer(dir string) (*Bundle, error) {
	cert, err := loadCert(ServerCertPath(dir))
	if err != nil {
		return nil, err
	}
	key, err := loadKey(ServerKeyPath(dir))
	if err != nil {
		return nil, err
	}
	if !equalPub(&key.PublicKey, cert.PublicKey) {
		return nil, fmt.Errorf("pki: server.crt does not match server.key")
	}
	return &Bundle{Cert: cert, Key: key, Dir: dir}, nil
}

func issueAndWrite(ca *Bundle, dir, host string) (*Bundle, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("pki: generate server key: %w", err)
	}
	cert, err := signServer(ca, key, host)
	if err != nil {
		return nil, err
	}
	b := &Bundle{Cert: cert, Key: key, Dir: dir}
	if err := osMkdirWriteServer(b); err != nil {
		return nil, err
	}
	return b, nil
}

func osMkdirWriteServer(b *Bundle) error {
	if err := writeCert(ServerCertPath(b.Dir), b.Cert); err != nil {
		return err
	}
	return writeKey(ServerKeyPath(b.Dir), b.Key)
}

func signServer(ca *Bundle, key *ecdsa.PrivateKey, host string) (*x509.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("pki: serial: %w", err)
	}
	now := time.Now().Add(-time.Hour)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"rbac"},
			CommonName:   serverCN,
		},
		NotBefore:             now,
		NotAfter:              now.Add(serverValidFor),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	addHostSAN(tmpl, host)
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.Key)
	if err != nil {
		return nil, fmt.Errorf("pki: create server cert: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("pki: parse server cert: %w", err)
	}
	return cert, nil
}

func addHostSAN(tmpl *x509.Certificate, host string) {
	h := net.ParseIP(host)
	switch {
	case host == "" || host == "0.0.0.0" || host == "::" || host == "[::]":
		return
	case h != nil:
		tmpl.IPAddresses = append(tmpl.IPAddresses, h)
	default:
		tmpl.DNSNames = append(tmpl.DNSNames, host)
	}
}

func (b *Bundle) TLSCertificate() (tls.Certificate, error) {
	if b == nil || b.Cert == nil || b.Key == nil {
		return tls.Certificate{}, fmt.Errorf("pki: incomplete bundle")
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: b.Cert.Raw})
	keyDER, err := x509.MarshalECPrivateKey(b.Key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("pki: marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return tls.X509KeyPair(certPEM, keyPEM)
}
