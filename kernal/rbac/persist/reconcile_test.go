package persist

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aid297/rbac/server/crypto"
	"github.com/aid297/rbac/server/policy"
)

func resetPause() {
	requestResume()
}

func bind(t *testing.T, s *Store) {
	t.Helper()
	if err := s.AddBinding(policy.Binding{
		Src: "u", Dst: "r", Enabled: true,
		Conditions: []policy.Condition{policy.AllCondition{}},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReconcilePlaintextToEncrypt(t *testing.T) {
	resetPause()
	path := filepath.Join(t.TempDir(), DefaultFile)
	plain, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	bind(t, plain)

	alg := crypto.Default()
	key, err := alg.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenWith(path, alg, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Reconcile(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !crypto.IsSealed(raw) {
		t.Fatal("expected sealed after reconcile")
	}
	if !s.Enforce("u", "r", nil) {
		t.Fatal("cache")
	}
	if p, _ := Paused(); p {
		t.Fatal("should resume after success")
	}
}

func TestReconcileEncryptToPlaintext(t *testing.T) {
	resetPause()
	path := filepath.Join(t.TempDir(), DefaultFile)
	alg := crypto.Default()
	key, _ := alg.GenerateKey()
	enc, err := OpenWith(path, alg, key)
	if err != nil {
		t.Fatal(err)
	}
	bind(t, enc)

	s, err := OpenWith(path, nil, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Reconcile(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if crypto.IsSealed(raw) || !bytes.Contains(raw, []byte("b, u")) {
		t.Fatalf("expected plaintext: %s", raw)
	}
}

func TestReconcileAlgorithmChange(t *testing.T) {
	resetPause()
	path := filepath.Join(t.TempDir(), DefaultFile)
	old, _ := crypto.Lookup(crypto.NameAES128GCM)
	key, _ := old.GenerateKey()
	s, err := OpenWith(path, old, key)
	if err != nil {
		t.Fatal(err)
	}
	bind(t, s)

	neu, _ := crypto.Lookup(crypto.NameSM4)
	s2, err := OpenWith(path, neu, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.Reconcile(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	name, err := crypto.EnvelopeAlg(raw)
	if err != nil || name != crypto.NameSM4 {
		t.Fatalf("alg=%s err=%v", name, err)
	}
	if !s2.Enforce("u", "r", nil) {
		t.Fatal("after alg migrate")
	}
}

func TestReconcileKeyRotate(t *testing.T) {
	resetPause()
	path := filepath.Join(t.TempDir(), DefaultFile)
	alg := crypto.Default()
	oldKey, _ := alg.GenerateKey()
	newKey, _ := alg.GenerateKey()
	s, err := OpenWith(path, alg, oldKey)
	if err != nil {
		t.Fatal(err)
	}
	bind(t, s)

	s2, err := OpenWithPrev(path, alg, newKey, oldKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.Reconcile(); err != nil {
		t.Fatal(err)
	}
	s3, err := OpenWith(path, alg, newKey)
	if err != nil {
		t.Fatal(err)
	}
	if !s3.Enforce("u", "r", nil) {
		t.Fatal("new key should open migrated file")
	}
}

func TestReconcileMismatchPauses(t *testing.T) {
	resetPause()
	path := filepath.Join(t.TempDir(), DefaultFile)
	alg := crypto.Default()
	key, _ := alg.GenerateKey()
	s, err := OpenWith(path, alg, key)
	if err != nil {
		t.Fatal(err)
	}
	bind(t, s)

	wrong, _ := alg.GenerateKey()
	_, err = OpenWith(path, alg, wrong)
	if err == nil {
		t.Fatal("expected decrypt error")
	}
	if !errors.Is(err, ErrCryptoMismatch) {
		t.Fatalf("err=%v", err)
	}

	s2, err := OpenWithPrev(path, alg, wrong, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.Reconcile(); err != nil {
		t.Fatal(err)
	}

	resetPause()
	_, err = OpenWith(path, alg, bytes.Repeat([]byte{1}, 32))
	if err == nil {
		t.Fatal("wrong key no prev")
	}
}

func TestReconfigureRewrites(t *testing.T) {
	resetPause()
	path := filepath.Join(t.TempDir(), DefaultFile)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	bind(t, s)
	alg := crypto.Default()
	key, _ := alg.GenerateKey()
	if err := s.Reconfigure(alg, key); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !crypto.IsSealed(raw) {
		t.Fatal("reconfigure should encrypt")
	}
	if p, _ := Paused(); p {
		t.Fatal("should resume")
	}
}
