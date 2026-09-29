package svcctl

import "sync"

// Gate tracks in-flight requests so persist.ServiceControl.Pause can drain them.
// Share one Gate across HTTP and gRPC listeners.
type Gate struct {
	inflight sync.WaitGroup
}

// Pause blocks until all tracked requests finish.
func (g *Gate) Pause(string) error {
	if g == nil {
		return nil
	}
	g.inflight.Wait()
	return nil
}

// Resume is a no-op; callers clear the paused flag via persist.
func (g *Gate) Resume() error { return nil }

// Track marks one in-flight request. Call the returned function when done.
func (g *Gate) Track() (done func()) {
	if g == nil {
		return func() {}
	}
	g.inflight.Add(1)
	return g.inflight.Done
}
