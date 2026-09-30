package persist

import (
	"context"
	"errors"
)

var (
	ErrCacheMiss = errors.New("persist: cache miss")
	errClosed    = errors.New("persist: store closed")
)

// Cache is an optional whole-blob replica of the policy file.
// The file remains the durable source of truth; a crash always reloads from disk.
type Cache interface {
	Get(ctx context.Context) ([]byte, error)
	Set(ctx context.Context, blob []byte) error
	Close() error
}

// UseCache attaches a cache and switches file writes to asynchronous.
// Memory is loaded from the file first; the cache is then overwritten from that
// snapshot so a crash cannot resurrect a newer Redis blob.
func (s *Store) UseCache(c Cache) error {
	if c == nil {
		return errors.New("persist: nil cache")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errClosed
	}
	if s.cache != nil {
		return errors.New("persist: cache already attached")
	}
	s.cache = c
	s.async = true
	if err := s.cacheSetLocked(); err != nil {
		s.cache = nil
		s.async = false
		return err
	}
	s.startAsyncLocked()
	return nil
}

func (s *Store) CacheEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache != nil
}
