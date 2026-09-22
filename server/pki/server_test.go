package pki

import (
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureServerIssuedByCA(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secret")
	ca, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := EnsureServer(ca, dir, "10.0.0.5")
	if err != nil {
		t.Fatal(err)
	}
	if srv.Cert.IsCA {
		t.Fatal("server cert must not be a CA")
	}
	if err := srv.Cert.CheckSignatureFrom(ca.Cert); err != nil {
		t.Fatal(err)
	}
	found := false
	want := net.ParseIP("10.0.0.5")
	for _, ip := range srv.Cert.IPAddresses {
		if ip.Equal(want) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing host SAN")
	}
	if _, err := os.Stat(ServerCertPath(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.TLSCertificate(); err != nil {
		t.Fatal(err)
	}
	again, err := EnsureServer(ca, dir, "10.0.0.5")
	if err != nil {
		t.Fatal(err)
	}
	if srv.Cert.SerialNumber.Cmp(again.Cert.SerialNumber) != 0 {
		t.Fatal("reload should keep server cert")
	}
}

func TestEnsureServerRefusesOrphanCert(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secret")
	ca, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureServer(ca, dir, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(ServerKeyPath(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureServer(ca, dir, "127.0.0.1"); err == nil {
		t.Fatal("expected error")
	}
}

func TestServerCertVerifyWithCAPool(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secret")
	ca, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := EnsureServer(ca, dir, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	opts := x509.VerifyOptions{DNSName: "localhost", Roots: pool}
	if _, err := srv.Cert.Verify(opts); err != nil {
		t.Fatal(err)
	}
}
