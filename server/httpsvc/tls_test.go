package httpsvc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/aid297/rbac/server/pki"
)

func TestHTTPSSelfSignedRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secret")
	ca, err := pki.Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := pki.EnsureServer(ca, dir, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	cert, err := srv.TLSCertificate()
	if err != nil {
		t.Fatal(err)
	}

	s := testStore(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hs := newHTTPServer(ln.Addr().String(), NewHandler(s))
	hs.TLSConfig = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	go func() { _ = hs.ServeTLS(ln, "", "") }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = hs.Shutdown(ctx)
	})

	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		},
	}
	var res *http.Response
	deadline := time.Now().Add(2 * time.Second)
	for {
		res, err = client.Get("https://" + ln.Addr().String() + "/healthz")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("%s %s", res.Status, b)
	}
}
