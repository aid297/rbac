package rbac

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCACache_LoadOrFetch_LocalMiss(t *testing.T) {
	// Setup mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/ca-cert" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("test-ca-cert"))
	}))
	defer server.Close()

	// Create temp directory for CA cert
	tmpDir := t.TempDir()
	caPath := filepath.Join(tmpDir, "ca.pem")

	cache := NewCACache(server.URL, caPath)
	pem, err := cache.LoadOrFetch(context.Background())
	if err != nil {
		t.Fatalf("LoadOrFetch failed: %v", err)
	}

	if string(pem) != "test-ca-cert" {
		t.Errorf("expected 'test-ca-cert', got %q", string(pem))
	}

	// Verify file was written
	data, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatalf("failed to read cached file: %v", err)
	}
	if string(data) != "test-ca-cert" {
		t.Errorf("cached file content mismatch: expected 'test-ca-cert', got %q", string(data))
	}
}

func TestCACache_LoadOrFetch_LocalHit(t *testing.T) {
	// Setup mock server - should not be called
	serverCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalled = true
	}))
	defer server.Close()

	// Pre-create local CA cert
	tmpDir := t.TempDir()
	caPath := filepath.Join(tmpDir, "ca.pem")
	expectedPEM := []byte("existing-ca-cert")
	if err := os.WriteFile(caPath, expectedPEM, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	cache := NewCACache(server.URL, caPath)
	pem, err := cache.LoadOrFetch(context.Background())
	if err != nil {
		t.Fatalf("LoadOrFetch failed: %v", err)
	}

	if string(pem) != string(expectedPEM) {
		t.Errorf("expected %q, got %q", string(expectedPEM), string(pem))
	}

	if serverCalled {
		t.Error("server should not have been called when local file exists")
	}
}

func TestCACache_LoadOrFetch_RetryOnFailure(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	caPath := filepath.Join(tmpDir, "ca.pem")

	cache := NewCACache(server.URL, caPath)
	_, err := cache.LoadOrFetch(context.Background())
	if err == nil {
		t.Fatal("expected error after retry, got nil")
	}

	// Should have attempted twice (initial + 1 retry)
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestCACache_LoadOrFetch_EmptyLocalFile(t *testing.T) {
	fetchCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fetched-cert"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	caPath := filepath.Join(tmpDir, "ca.pem")

	// Create empty file
	if err := os.WriteFile(caPath, []byte{}, 0644); err != nil {
		t.Fatalf("failed to create empty file: %v", err)
	}

	cache := NewCACache(server.URL, caPath)
	pem, err := cache.LoadOrFetch(context.Background())
	if err != nil {
		t.Fatalf("LoadOrFetch failed: %v", err)
	}

	if string(pem) != "fetched-cert" {
		t.Errorf("expected 'fetched-cert', got %q", string(pem))
	}

	if fetchCount != 1 {
		t.Errorf("expected 1 fetch for empty file, got %d", fetchCount)
	}
}

func TestCACache_FetchFromServer_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.WriteHeader(http.StatusOK)
		// Empty body
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	caPath := filepath.Join(tmpDir, "ca.pem")

	cache := NewCACache(server.URL, caPath)
	_, err := cache.LoadOrFetch(context.Background())
	if err == nil {
		t.Fatal("expected error for empty response, got nil")
	}
}

func TestCACache_FetchFromServer_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	caPath := filepath.Join(tmpDir, "ca.pem")

	cache := NewCACache(server.URL, caPath)
	_, err := cache.LoadOrFetch(context.Background())
	if err == nil {
		t.Fatal("expected error for non-OK status, got nil")
	}
}
