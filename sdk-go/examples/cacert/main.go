package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	rbac "github.com/aid297/rbac/sdk-go"
)

func main() {
	// Example: Using automatic CA certificate management
	// The SDK will automatically fetch and cache the server's CA certificate

	tmpDir, err := os.MkdirTemp("", "rbac-sdk-example")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	caPath := filepath.Join(tmpDir, "ca.pem")

	// Create client with automatic CA cert management
	client, err := rbac.NewClient(
		"https://localhost:8443",
		rbac.WithCACertPath(caPath),
	)
	if err != nil {
		log.Fatalf("failed to create client: %v", err)
	}

	// First call: SDK checks if CA cert exists locally
	// If missing, it fetches from https://localhost:8443/v1/ca-cert
	// and caches it at caPath for future use
	ctx := context.Background()
	err = client.Health(ctx)
	if err != nil {
		log.Fatalf("health check failed: %v", err)
	}

	fmt.Println("✓ Health check passed")
	fmt.Printf("✓ CA certificate cached at: %s\n", caPath)

	// Subsequent calls will use the cached CA cert without network fetch
	client2, err := rbac.NewClient(
		"https://localhost:8443",
		rbac.WithCACertPath(caPath),
	)
	if err != nil {
		log.Fatalf("failed to create second client: %v", err)
	}

	err = client2.Health(ctx)
	if err != nil {
		log.Fatalf("second health check failed: %v", err)
	}

	fmt.Println("✓ Second health check passed (using cached CA cert)")
}
