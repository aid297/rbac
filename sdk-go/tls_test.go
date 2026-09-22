package rbac

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func healthOK(w http.ResponseWriter, _ *http.Request) {
	writeResp(w, http.StatusOK, `{"status":"ok"}`)
}

func certPEM(t *testing.T, srv *httptest.Server) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
}

func TestTLSTrust(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(healthOK))
	t.Cleanup(srv.Close)
	ctx := context.Background()

	t.Run("CA cert trusted", func(t *testing.T) {
		c, err := NewClient(srv.URL, WithCACert(certPEM(t, srv)))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if err := c.Health(ctx); err != nil {
			t.Fatalf("Health with CA: %v", err)
		}
	})

	t.Run("CA cert file trusted", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ca.crt")
		if err := os.WriteFile(path, certPEM(t, srv), 0o600); err != nil {
			t.Fatalf("write ca: %v", err)
		}
		c, err := NewClient(srv.URL, WithCACertFile(path))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if err := c.Health(ctx); err != nil {
			t.Fatalf("Health with CA file: %v", err)
		}
	})

	t.Run("insecure skip verify", func(t *testing.T) {
		c, err := NewClient(srv.URL, WithInsecureSkipVerify(true))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if err := c.Health(ctx); err != nil {
			t.Fatalf("Health insecure: %v", err)
		}
	})

	t.Run("bring your own client", func(t *testing.T) {
		c, err := NewClient(srv.URL, WithHTTPClient(srv.Client()))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if err := c.Health(ctx); err != nil {
			t.Fatalf("Health with custom client: %v", err)
		}
	})

	t.Run("untrusted CA fails", func(t *testing.T) {
		c, err := NewClient(srv.URL)
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		err = c.Health(ctx)
		if err == nil {
			t.Fatal("want TLS verification error, got nil")
		}
		var ae *APIError
		if errors.As(err, &ae) {
			t.Fatalf("transport error must not be an *APIError: %v", err)
		}
	})
}

func TestCACertFileMissing(t *testing.T) {
	if _, err := NewClient("https://localhost:8443", WithCACertFile("/nonexistent/ca.crt")); err == nil {
		t.Fatal("want error for missing CA file")
	}
}
