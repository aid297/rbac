package httpsvc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"rbac/persist"
)

type Options struct {
	HTTPAddr  string
	HTTPSAddr string
	TLSCert   *tls.Certificate
}

type gate struct {
	inflight sync.WaitGroup
}

func (g *gate) Pause(string) error {
	g.inflight.Wait()
	return nil
}

func (g *gate) Resume() error { return nil }

func (g *gate) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.inflight.Add(1)
		defer g.inflight.Done()
		next.ServeHTTP(w, r)
	})
}

// Serve runs HTTP and/or HTTPS until ctx is cancelled. Empty addrs are skipped.
func Serve(ctx context.Context, store *persist.Store, opt Options) error {
	if store == nil {
		return fmt.Errorf("httpsvc: nil store")
	}
	if opt.HTTPAddr == "" && opt.HTTPSAddr == "" {
		return nil
	}
	if opt.HTTPSAddr != "" && opt.TLSCert == nil {
		return fmt.Errorf("httpsvc: TLS certificate required for HTTPS")
	}

	g := new(gate)
	persist.SetServiceControl(g)
	defer persist.SetServiceControl(nil)

	h := g.wrap(NewHandler(store))
	var servers []*http.Server
	errCh := make(chan error, 2)

	if opt.HTTPAddr != "" {
		s := newHTTPServer(opt.HTTPAddr, h)
		servers = append(servers, s)
		go func() { errCh <- s.ListenAndServe() }()
	}
	if opt.HTTPSAddr != "" {
		s := newHTTPServer(opt.HTTPSAddr, h)
		s.TLSConfig = &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{*opt.TLSCert},
		}
		servers = append(servers, s)
		go func() { errCh <- s.ListenAndServeTLS("", "") }()
	}

	select {
	case <-ctx.Done():
		shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, s := range servers {
			_ = s.Shutdown(shCtx)
		}
		return nil
	case err := <-errCh:
		shCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		for _, s := range servers {
			_ = s.Shutdown(shCtx)
		}
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func newHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
