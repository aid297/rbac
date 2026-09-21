package persist

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"rbac/policy"
)

// DefaultDir is the cwd-relative data directory used when config does not set policy.dir.
// DefaultFile is the in-program policy filename; config never names the file.
const (
	DefaultDir  = "stats"
	DefaultFile = "policy.rbac"
)

func DefaultPath() string {
	return FilePath(DefaultDir)
}

// FilePath joins a directory with the built-in filename. Empty dir uses DefaultDir.
func FilePath(dir string) string {
	if dir == "" {
		dir = DefaultDir
	}
	return filepath.Join(dir, DefaultFile)
}

// Store is a file-backed policy engine. Memory is the query path; the file is
// the durable copy. Path is the full file path (directory + DefaultFile).
type Store struct {
	path string
	eng  *policy.Engine
	mu   sync.Mutex
}

func Open(path string) (*Store, error) {
	if path == "" {
		path = DefaultPath()
	}
	s := new(Store)
	s.path = path
	s.eng = policy.NewEngine()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) AddBinding(b policy.Binding) error {
	return s.mutate(func() error { return s.eng.AddBinding(b) })
}

func (s *Store) UpdateBinding(b policy.Binding) error {
	return s.mutate(func() error { return s.eng.UpdateBinding(b) })
}

func (s *Store) RemoveBinding(src, dst, scenario string) error {
	return s.mutate(func() error { return s.eng.RemoveBinding(src, dst, scenario) })
}

func (s *Store) SetEnabled(src, dst, scenario string, enabled bool) error {
	return s.mutate(func() error { return s.eng.SetEnabled(src, dst, scenario, enabled) })
}

// Replace loads a policy blob into memory and persists it (fail-fast, no partial file).
func (s *Store) Replace(blob string) error {
	return s.mutate(func() error { return s.eng.Load(blob) })
}

func (s *Store) GetBinding(src, dst, scenario string) (policy.Binding, bool) {
	return s.eng.GetBinding(src, dst, scenario)
}

func (s *Store) ListBindings() []policy.Binding { return s.eng.ListBindings() }

func (s *Store) Enforce(subject, target string, ctx *policy.EvalContext) bool {
	return s.eng.Enforce(subject, target, ctx)
}

func (s *Store) Reachable(subject string, ctx *policy.EvalContext) []string {
	return s.eng.Reachable(subject, ctx)
}

func (s *Store) Serialize() string { return s.eng.Serialize() }

// Reload replaces the in-memory snapshot from disk. Missing file yields an empty engine.
func (s *Store) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eng = policy.NewEngine()
	return s.loadLocked()
}

func (s *Store) mutate(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(); err != nil {
		return err
	}
	if err := s.saveLocked(); err != nil {
		_ = s.loadLocked()
		return err
	}
	return nil
}

func (s *Store) loadLocked() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.eng = policy.NewEngine()
			return nil
		}
		return fmt.Errorf("persist: read %s: %w", s.path, err)
	}
	eng := policy.NewEngine()
	if err := eng.Load(string(data)); err != nil {
		return fmt.Errorf("persist: load %s: %w", s.path, err)
	}
	s.eng = eng
	return nil
}

func (s *Store) saveLocked() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("persist: mkdir %s: %w", dir, err)
	}
	blob := []byte(s.eng.Serialize())
	tmp, err := os.CreateTemp(dir, ".rbac-*.tmp")
	if err != nil {
		return fmt.Errorf("persist: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(blob); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("persist: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("persist: rename: %w", err)
	}
	return nil
}
