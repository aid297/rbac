package rbac

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// CACache handles loading or fetching a CA certificate from a remote server.
type CACache struct {
	baseURL string
	path    string
	client  *http.Client
}

// NewCACache creates a CACache that will fetch the CA cert from baseURL/v1/ca-cert
// and store it at path.
func NewCACache(baseURL, path string) *CACache {
	return &CACache{
		baseURL: baseURL,
		path:    path,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// LoadOrFetch checks if the CA cert exists at the configured path. If it does,
// it reads and returns the PEM bytes. If not, it fetches from the server's
// /v1/ca-cert endpoint, writes to the local path, and returns the PEM bytes.
func (c *CACache) LoadOrFetch(ctx context.Context) ([]byte, error) {
	// Check if local file exists
	if _, err := os.Stat(c.path); err == nil {
		// File exists, read it
		pem, err := os.ReadFile(c.path)
		if err != nil {
			return nil, fmt.Errorf("rbac: read CA cert from %q: %w", c.path, err)
		}
		if len(pem) > 0 {
			return pem, nil
		}
		// Empty file, treat as missing and refetch
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("rbac: stat CA cert path %q: %w", c.path, err)
	}

	// File doesn't exist or is empty, fetch from server
	return c.fetchFromServer(ctx)
}

// fetchFromServer downloads the CA cert from /v1/ca-cert with retry-once logic.
func (c *CACache) fetchFromServer(ctx context.Context) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt < 2; attempt++ {
		pem, err := c.doFetch(ctx)
		if err == nil {
			// Write to local path
			if err := os.WriteFile(c.path, pem, 0644); err != nil {
				return nil, fmt.Errorf("rbac: write CA cert to %q: %w", c.path, err)
			}
			return pem, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("rbac: failed to fetch CA cert after 2 attempts: %w", lastErr)
}

// doFetch performs a single fetch from /v1/ca-cert.
func (c *CACache) doFetch(ctx context.Context) ([]byte, error) {
	url := c.baseURL + "/v1/ca-cert"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("rbac: build CA cert request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rbac: fetch CA cert: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rbac: fetch CA cert: server returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10)) // 64KB max
	if err != nil {
		return nil, fmt.Errorf("rbac: read CA cert response: %w", err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("rbac: CA cert endpoint returned empty response")
	}

	return data, nil
}
