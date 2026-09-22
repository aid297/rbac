package adminui

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"time"

	"rbac/persist"
)

//go:embed page.html
var pageHTML []byte

func Serve(ctx context.Context, addr string, store *persist.Store, creds *Creds) error {
	if store == nil || creds == nil {
		return fmt.Errorf("adminui: store and creds required")
	}
	if addr == "" {
		return fmt.Errorf("adminui: empty listen address")
	}
	s := &http.Server{
		Addr:              addr,
		Handler:           NewHandler(store, creds, pageHTML),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- s.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.Shutdown(shCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
