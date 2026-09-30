package pki

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCreatesAndReloads(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secret")
	b1, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b1.Cert == nil || !b1.Cert.IsCA {
		t.Fatal("expected CA cert")
	}
	if _, err := os.Stat(CertPath(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(KeyPath(dir)); err != nil {
		t.Fatal(err)
	}
	b2, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b1.Cert.SerialNumber.Cmp(b2.Cert.SerialNumber) != 0 {
		t.Fatal("reload should keep the same CA")
	}
}

func TestEnsureRecreatesCertFromKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secret")
	if _, err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(CertPath(dir)); err != nil {
		t.Fatal(err)
	}
	b, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b.Cert == nil || !b.Cert.IsCA {
		t.Fatal("expected rebuilt cert")
	}
}

func TestEnsureRefusesOrphanCert(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secret")
	if _, err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(KeyPath(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(dir); err == nil {
		t.Fatal("expected error when key missing")
	}
}

func TestEmptyDirUsesDefaultName(t *testing.T) {
	if CertPath("") != filepath.Join(DefaultDir, CertFile) {
		t.Fatal(CertPath(""))
	}
}
