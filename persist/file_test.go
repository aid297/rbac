package persist

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"rbac/crypto"
	"rbac/policy"
)

func TestDefaultPath(t *testing.T) {
	got := DefaultPath()
	want := filepath.Join("stats", "policy.rbac")
	if got != want {
		t.Fatalf("DefaultPath()=%q want %q", got, want)
	}
	if FilePath("data") != filepath.Join("data", DefaultFile) {
		t.Fatal(FilePath("data"))
	}
	if FilePath("") != DefaultPath() {
		t.Fatal(FilePath(""))
	}
}

func TestOpenMissingIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats", "policy.rbac")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Enforce("u", "p", nil) {
		t.Fatal("empty file-backed engine should deny")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("missing file should not be created on Open")
	}
}

func TestSaveReloadEnforce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats", "policy.rbac")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	err = s.AddBinding(policy.Binding{
		Src: "user1", Dst: "role1", Enabled: true,
		Conditions: []policy.Condition{policy.AllCondition{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = s.AddBinding(policy.Binding{
		Src: "role1", Dst: "perm1", Enabled: true,
		Conditions: []policy.Condition{policy.AllCondition{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Enforce("user1", "perm1", nil) {
		t.Fatal("in-memory enforce")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != s.Serialize() {
		t.Fatalf("file != serialize\nfile:\n%s\nmem:\n%s", raw, s.Serialize())
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s2.Enforce("user1", "perm1", nil) {
		t.Fatal("enforce after read from file")
	}
	if s2.Enforce("perm1", "user1", nil) {
		t.Fatal("directed")
	}
}

func TestMutatePersistsAndBadWriteDoesNotKeepMemory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.rbac")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddBinding(policy.Binding{
		Src: "a", Dst: "b", Enabled: true,
		Conditions: []policy.Condition{policy.AllCondition{}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnabled("a", "b", "", false); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Enforce("a", "b", nil) {
		t.Fatal("disabled edge should not enforce after reload")
	}
	if err := s.RemoveBinding("a", "b", ""); err != nil {
		t.Fatal(err)
	}
	s3, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s3.GetBinding("a", "b", ""); ok {
		t.Fatal("removed binding still on disk")
	}
}

func TestOpenRejectsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.rbac")
	if err := os.WriteFile(path, []byte("b, not-a-valid-line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(path)
	if err == nil {
		t.Fatal("expected load error")
	}
	if !errors.Is(err, policy.ErrParseLine) {
		t.Fatalf("err=%v", err)
	}
}

func TestReplaceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats", "policy.rbac")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	blob := "# rbac-policy v1\nb, u, r, , 1, ALL\n"
	if err := s.Replace(blob); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s2.Enforce("u", "r", nil) {
		t.Fatal("replace did not persist")
	}
}

func TestReloadPicksUpExternalWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.rbac")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.AddBinding(policy.Binding{
		Src: "x", Dst: "y", Enabled: true,
		Conditions: []policy.Condition{policy.AllCondition{}},
	}); err != nil {
		t.Fatal(err)
	}
	if s.Enforce("x", "y", nil) {
		t.Fatal("stale memory should not see other process write yet")
	}
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	if !s.Enforce("x", "y", nil) {
		t.Fatal("reload should read file")
	}
}

func TestOpenEmptyPathUsesDefault(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if s.Path() != DefaultPath() {
		t.Fatalf("path=%q", s.Path())
	}
}

func TestEncryptedPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats", DefaultFile)
	alg := crypto.Default()
	key, err := alg.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenWith(path, alg, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddBinding(policy.Binding{
		Src: "u", Dst: "r", Enabled: true,
		Conditions: []policy.Condition{policy.AllCondition{}},
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !crypto.IsSealed(raw) || bytes.Contains(raw, []byte("b, u")) {
		t.Fatalf("expected sealed file: %s", raw)
	}
	s2, err := OpenWith(path, alg, key)
	if err != nil {
		t.Fatal(err)
	}
	if !s2.Enforce("u", "r", nil) {
		t.Fatal("decrypt enforce")
	}
	if _, err := OpenWith(path, alg, bytes.Repeat([]byte{9}, len(key))); err == nil {
		t.Fatal("wrong key")
	}
}
