package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"rbac/cache"
	"rbac/config"
	"rbac/persist"
	"rbac/pki"
)

func main() {
	configPath := flag.String("config", "", "config file path (RBAC_CONFIG overrides this; default is ./config.yaml)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	alg, err := cfg.Algorithm()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	var store *persist.Store
	if cfg.EncryptEnabled() && os.Getenv("RBAC_DEBUG") != "1" {
		store, err = persist.OpenWithPrev(cfg.PolicyPath(), alg, cfg.KeyBytes(), cfg.PrevKeyBytes())
	} else {
		store, err = persist.OpenWithPrev(cfg.PolicyPath(), nil, cfg.KeyBytes(), cfg.PrevKeyBytes())
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if err := store.Reconcile(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if cfg.CacheEnabled() {
		rc, err := cache.OpenRedis(cfg.Cache.Addr, cfg.CachePassword(), cfg.Cache.DB)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		if err := store.UseCache(rc); err != nil {
			_ = rc.Close()
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
	}

	if _, err := pki.Ensure(cfg.CADir()); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("config_file=%s origin=%s policy_dir=%s encrypt=%t crypto=%s policy_file=%s ca_dir=%s cache=%t cache_kind=%s bindings=%d\n",
		cfg.Path, cfg.Origin, cfg.Policy.Dir, cfg.EncryptEnabled(), cfg.Policy.Crypto, store.Path(), cfg.CADir(),
		cfg.CacheEnabled(), cfg.Cache.Kind, len(store.ListBindings()))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	if err := store.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
