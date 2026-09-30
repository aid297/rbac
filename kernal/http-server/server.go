package httpsvc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aid297/rbac/server/persist"
	"github.com/aid297/rbac/server/svcctl"
)

// Options configures HTTP and/or HTTPS listeners.
type Options struct {
	HTTPAddr  string
	HTTPSAddr string
	TLSCert   *tls.Certificate
	CACertPEM []byte // PEM-encoded CA certificate for /v1/ca-cert endpoint
	// Gate tracks in-flight requests for persist.ServiceControl.Pause.
	// If nil and OwnServiceControl is true, a private gate is created and registered.
	Gate *svcctl.Gate
	// OwnServiceControl registers Gate (or a private one) with persist when true.
	// Prefer sharing one Gate from main when HTTP and gRPC both run.
	OwnServiceControl bool
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

	gate := opt.Gate
	if gate == nil {
		gate = new(svcctl.Gate)
		persist.SetServiceControl(gate)
		defer persist.SetServiceControl(nil)
	} else if opt.OwnServiceControl {
		persist.SetServiceControl(gate)
		defer persist.SetServiceControl(nil)
	}

	h := wrapGate(gate, NewHandler(store, opt.CACertPEM))
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

func wrapGate(g *svcctl.Gate, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		done := g.Track()
		defer done()
		next.ServeHTTP(w, r)
	})
}

func newHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
