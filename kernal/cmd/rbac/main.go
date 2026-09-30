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

	"github.com/aid297/rbac/kernal/adminui"
	"github.com/aid297/rbac/kernal/rbac/cache"
	"github.com/aid297/rbac/kernal/config"
	"github.com/aid297/rbac/kernal/grpc-server"
	"github.com/aid297/rbac/kernal/http-server"
	"github.com/aid297/rbac/kernal/logging"
	"github.com/aid297/rbac/kernal/rbac/persist"
	"github.com/aid297/rbac/kernal/pki"
	"github.com/aid297/rbac/kernal/svcctl"
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

	svcLog := logging.FromZap(logger)
	persist.SetLogger(svcLog)
	httpserver.SetLogger(svcLog)
	grpcserver.SetLogger(svcLog)

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

	needTLS := cfg.HTTPSEnabled() || cfg.GRPCTLSEnabled()

	httpOpt := httpserver.Options{CACertPEM: caCertPEM}
	grpcOpt := grpcserver.Options{CACertPEM: caCertPEM}
	if cfg.HTTPEnabled() {
		httpOpt.HTTPAddr = cfg.HTTPAddr()
	}
	if cfg.HTTPSEnabled() {
		httpOpt.HTTPSAddr = cfg.HTTPSAddr()
	}
	if cfg.GRPCEnabled() {
		grpcOpt.Addr = cfg.GRPCAddr()
	}
	if cfg.GRPCTLSEnabled() {
		grpcOpt.TLSAddr = cfg.GRPCTLSAddr()
	}
	if needTLS {
		srv, err := pki.EnsureServer(ca, cfg.CADir(), cfg.HTTPHost())
		if err != nil {
			logger.Fatal("ensure server cert failed", zap.Error(err))
		}
		tlsCert, err := srv.TLSCertificate()
		if err != nil {
			logger.Fatal("read TLS certificate failed", zap.Error(err))
		}
		if cfg.HTTPSEnabled() {
			httpOpt.TLSCert = &tlsCert
		}
		if cfg.GRPCTLSEnabled() {
			grpcOpt.TLSCert = &tlsCert
		}
	}

	gate := new(svcctl.Gate)
	apiOn := cfg.HTTPEnabled() || cfg.HTTPSEnabled() || cfg.GRPCEnabled() || cfg.GRPCTLSEnabled()
	if apiOn {
		persist.SetServiceControl(gate)
		defer persist.SetServiceControl(nil)
		httpOpt.Gate = gate
		grpcOpt.Gate = gate
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
		zap.Bool("grpc", cfg.GRPCEnabled()),
		zap.Bool("grpc_tls", cfg.GRPCTLSEnabled()),
		zap.Bool("admin", cfg.AdminEnabled()),
		zap.String("http_addr", httpOpt.HTTPAddr),
		zap.String("https_addr", httpOpt.HTTPSAddr),
		zap.String("grpc_addr", grpcOpt.Addr),
		zap.String("grpc_tls_addr", grpcOpt.TLSAddr),
		zap.String("admin_addr", cfg.AdminAddr()),
		zap.Int("bindings", len(store.ListBindings())),
	)

	errCh := make(chan error, 4)
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
		run(func() error { return httpserver.Serve(ctx, store, httpOpt) })
	}
	if cfg.GRPCEnabled() || cfg.GRPCTLSEnabled() {
		n++
		run(func() error { return grpcserver.Serve(ctx, store, grpcOpt) })
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
