package main

import (
	"context"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"go.uber.org/zap"

	"github.com/aid297/rbac/server/adminui"
	"github.com/aid297/rbac/server/cache"
	"github.com/aid297/rbac/server/config"
	"github.com/aid297/rbac/server/httpsvc"
	"github.com/aid297/rbac/server/logging"
	"github.com/aid297/rbac/server/persist"
	"github.com/aid297/rbac/server/pki"
)

func main() {
	configPath := flag.String("config", "", "config file path (RBAC_CONFIG overrides this; default is ./config.yaml)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	configDir := filepath.Dir(cfg.Path)
	logger, err := logging.Init(cfg.LogLevel(), cfg.LogFile(), cfg.LogDebug(), configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	alg, err := cfg.Algorithm()
	if err != nil {
		logger.Fatal("algorithm lookup failed", zap.Error(err))
	}

	var store *persist.Store
	if cfg.EncryptEnabled() && os.Getenv("RBAC_DEBUG") != "1" {
		store, err = persist.OpenWithPrev(cfg.PolicyPath(), alg, cfg.KeyBytes(), cfg.PrevKeyBytes())
	} else {
		store, err = persist.OpenWithPrev(cfg.PolicyPath(), nil, cfg.KeyBytes(), cfg.PrevKeyBytes())
	}
	if err != nil {
		logger.Fatal("open store failed", zap.Error(err))
	}
	if err := store.Reconcile(); err != nil {
		logger.Fatal("reconcile failed", zap.Error(err))
	}

	if cfg.CacheEnabled() {
		rc, err := cache.OpenRedis(cfg.Cache.Addr, cfg.CachePassword(), cfg.Cache.DB)
		if err != nil {
			logger.Fatal("open redis failed", zap.Error(err))
		}
		if err := store.UseCache(rc); err != nil {
			_ = rc.Close()
			logger.Fatal("use cache failed", zap.Error(err))
		}
		logger.Info("redis cache enabled", zap.String("addr", cfg.Cache.Addr))
	}

	ca, err := pki.Ensure(cfg.CADir())
	if err != nil {
		logger.Fatal("ensure CA failed", zap.Error(err))
	}
	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Cert.Raw})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opt := httpsvc.Options{CACertPEM: caCertPEM}
	if cfg.HTTPEnabled() {
		opt.HTTPAddr = cfg.HTTPAddr()
	}
	if cfg.HTTPSEnabled() {
		srv, err := pki.EnsureServer(ca, cfg.CADir(), cfg.HTTPHost())
		if err != nil {
			logger.Fatal("ensure server cert failed", zap.Error(err))
		}
		tlsCert, err := srv.TLSCertificate()
		if err != nil {
			logger.Fatal("read TLS certificate failed", zap.Error(err))
		}
		opt.HTTPSAddr = cfg.HTTPSAddr()
		opt.TLSCert = &tlsCert
	}

	logger.Info("rbac server starting",
		zap.String("config_file", cfg.Path),
		zap.String("origin", string(cfg.Origin)),
		zap.String("policy_dir", cfg.Policy.Dir),
		zap.Bool("encrypt", cfg.EncryptEnabled()),
		zap.String("crypto", cfg.Policy.Crypto),
		zap.String("policy_file", store.Path()),
		zap.String("ca_dir", cfg.CADir()),
		zap.Bool("cache", cfg.CacheEnabled()),
		zap.Bool("http", cfg.HTTPEnabled()),
		zap.Bool("https", cfg.HTTPSEnabled()),
		zap.Bool("admin", cfg.AdminEnabled()),
		zap.String("http_addr", opt.HTTPAddr),
		zap.String("https_addr", opt.HTTPSAddr),
		zap.String("admin_addr", cfg.AdminAddr()),
		zap.Int("bindings", len(store.ListBindings())),
	)

	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	run := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				select {
				case errCh <- err:
				default:
				}
			}
		}()
	}

	n := 0
	if cfg.AdminEnabled() {
		creds := adminui.NewCreds(cfg.AdminUsername(), cfg.AdminPassword())
		if err := config.WatchFile(cfg.Path, ctx.Done(), func() {
			a, err := config.ReadAdmin(cfg.Path)
			if err != nil {
				logger.Warn("admin reload failed", zap.Error(err))
				return
			}
			creds.Set(a.Username, a.Password)
			logger.Info("admin credentials reloaded")
		}); err != nil {
			logger.Fatal("watch config file failed", zap.Error(err))
		}
		n++
		run(func() error { return adminui.Serve(ctx, cfg.AdminAddr(), store, creds) })
	}
	if cfg.HTTPEnabled() || cfg.HTTPSEnabled() {
		n++
		run(func() error { return httpsvc.Serve(ctx, store, opt) })
	}

	if n == 0 {
		<-ctx.Done()
	} else {
		select {
		case <-ctx.Done():
		case err := <-errCh:
			if err != nil {
				logger.Error("service error", zap.Error(err))
				stop()
				wg.Wait()
				_ = store.Close()
				os.Exit(1)
			}
		}
		wg.Wait()
	}
	if err := store.Close(); err != nil {
		logger.Error("close store failed", zap.Error(err))
		os.Exit(1)
	}
	logger.Info("rbac server stopped")
}
