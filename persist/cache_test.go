package persist

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"rbac/policy"
)

type memCache struct {
	mu     sync.Mutex
	blob   []byte
	fail   bool
	sets   int
	closed bool
}

func (m *memCache) Get(context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.blob) == 0 {
		return nil, ErrCacheMiss
	}
	return append([]byte(nil), m.blob...), nil
}

func (m *memCache) Set(_ context.Context, blob []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("cache down")
	}
	m.sets++
	m.blob = append([]byte(nil), blob...)
	return nil
}

func (m *memCache) Close() error {
	m.closed = true
	return nil
}

func TestUseCacheWarmsFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultFile)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	bind(t, s)

	mc := new(memCache)
	if err := s.UseCache(mc); err != nil {
		t.Fatal(err)
	}
	got, err := mc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != s.Serialize() {
		t.Fatalf("cache != file snapshot")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCacheWriteIsSyncFileIsAsync(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultFile)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mc := new(memCache)
	if err := s.UseCache(mc); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	if err := s.AddBinding(policy.Binding{
		Src: "a", Dst: "b", Enabled: true,
		Conditions: []policy.Condition{policy.AllCondition{}},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := mc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != s.Serialize() {
		t.Fatal("cache should update before return")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		raw, err := os.ReadFile(path)
		if err == nil && string(raw) == s.Serialize() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("async file write did not land: err=%v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCrashRecoversFromFileNotCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultFile)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mc := new(memCache)
	if err := s.UseCache(mc); err != nil {
		t.Fatal(err)
	}
	bind(t, s)
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	stale := "# rbac-policy v1\nb, stale, x, , 1, ALL\n"
	if err := mc.Set(context.Background(), []byte(stale)); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s2.Enforce("u", "r", nil) {
		t.Fatal("file is source of truth")
	}
	if s2.Enforce("stale", "x", nil) {
		t.Fatal("must not load crash-stale cache")
	}
	if err := s2.UseCache(mc); err != nil {
		t.Fatal(err)
	}
	got, err := mc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != s2.Serialize() {
		t.Fatal("restart should overwrite cache from file")
	}
	if err := s2.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCacheSetFailureRollsBackMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultFile)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mc := new(memCache)
	if err := s.UseCache(mc); err != nil {
		t.Fatal(err)
	}
	mc.fail = true
	if err := s.AddBinding(policy.Binding{
		Src: "a", Dst: "b", Enabled: true,
		Conditions: []policy.Condition{policy.AllCondition{}},
	}); err == nil {
		t.Fatal("expected cache error")
	}
	if s.Enforce("a", "b", nil) {
		t.Fatal("memory should roll back")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCloseFlushesAndClosesCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultFile)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mc := new(memCache)
	if err := s.UseCache(mc); err != nil {
		t.Fatal(err)
	}
	bind(t, s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s.closed {
		t.Fatal("expected closed")
	}
	if string(raw) == "" {
		t.Fatal("close should flush file")
	}
	if !mc.closed {
		t.Fatal("cache not closed")
	}
	if err := s.AddBinding(policy.Binding{
		Src: "x", Dst: "y", Enabled: true,
		Conditions: []policy.Condition{policy.AllCondition{}},
	}); err == nil {
		t.Fatal("mutate after close")
	}
}
