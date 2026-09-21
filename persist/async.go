package persist

import "fmt"

func (s *Store) startAsyncLocked() {
	if s.asyncCh != nil {
		return
	}
	s.asyncCh = make(chan struct{}, 1)
	s.asyncStop = make(chan struct{})
	s.asyncDone = make(chan struct{})
	go s.asyncLoop()
}

func (s *Store) kickAsyncLocked() {
	if s.asyncCh == nil {
		return
	}
	select {
	case s.asyncCh <- struct{}{}:
	default:
	}
}

func (s *Store) asyncLoop() {
	defer close(s.asyncDone)
	for {
		select {
		case <-s.asyncCh:
			s.flushFile()
		case <-s.asyncStop:
			s.flushFile()
			return
		}
	}
}

func (s *Store) flushFile() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.saveLocked()
}

// Flush writes the current snapshot to disk immediately.
func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errClosed
	}
	return s.saveLocked()
}

// Close stops the async file writer, flushes disk, and closes the cache.
func (s *Store) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		stop := s.asyncStop
		done := s.asyncDone
		c := s.cache
		s.mu.Unlock()

		if stop != nil {
			close(stop)
			<-done
		}

		s.mu.Lock()
		err = s.saveLocked()
		s.cache = nil
		s.mu.Unlock()

		if c != nil {
			if cerr := c.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("persist: cache close: %w", cerr)
			}
		}
	})
	return err
}
