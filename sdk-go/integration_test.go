//go:build integration

package rbac

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestCACertAutoFetch_Integration verifies the full CA cert auto-fetch flow
// against a real server instance. Requires RBAC_SERVER_URL env var.
func TestCACertAutoFetch_Integration(t *testing.T) {
	serverURL := os.Getenv("RBAC_SERVER_URL")
	if serverURL == "" {
		t.Skip("set RBAC_SERVER_URL to run integration test")
	}

	tmpDir := t.TempDir()
	caPath := filepath.Join(tmpDir, "ca.pem")

	// First call: should fetch and cache CA cert
	client1, err := NewClient(serverURL, WithCACertPath(caPath))
	if err != nil {
		t.Fatalf("failed to create client on first call: %v", err)
	}

	err = client1.Health(context.Background())
	if err != nil {
		t.Fatalf("health check failed on first call: %v", err)
	}

	// Verify CA cert was cached
	if _, err := os.Stat(caPath); os.IsNotExist(err) {
		t.Fatal("CA cert should have been cached locally")
	}

	// Second call: should use cached CA cert (no network fetch)
	client2, err := NewClient(serverURL, WithCACertPath(caPath))
	if err != nil {
		t.Fatalf("failed to create client on second call: %v", err)
	}

	err = client2.Health(context.Background())
	if err != nil {
		t.Fatalf("health check failed on second call: %v", err)
	}
}
