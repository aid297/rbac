package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/aid297/rbac/server/adminui"
	"github.com/aid297/rbac/server/cache"
	"github.com/aid297/rbac/server/config"
	"github.com/aid297/rbac/server/httpsvc"
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

	ca, err := pki.Ensure(cfg.CADir())
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opt := httpsvc.Options{}
	if cfg.HTTPEnabled() {
		opt.HTTPAddr = cfg.HTTPAddr()
	}
	if cfg.HTTPSEnabled() {
		srv, err := pki.EnsureServer(ca, cfg.CADir(), cfg.HTTPHost())
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		tlsCert, err := srv.TLSCertificate()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		opt.HTTPSAddr = cfg.HTTPSAddr()
		opt.TLSCert = &tlsCert
	}

	fmt.Printf("config_file=%s origin=%s policy_dir=%s encrypt=%t crypto=%s policy_file=%s ca_dir=%s cache=%t http=%t https=%t admin=%t http_addr=%s https_addr=%s admin_addr=%s bindings=%d\n",
		cfg.Path, cfg.Origin, cfg.Policy.Dir, cfg.EncryptEnabled(), cfg.Policy.Crypto, store.Path(), cfg.CADir(),
		cfg.CacheEnabled(), cfg.HTTPEnabled(), cfg.HTTPSEnabled(), cfg.AdminEnabled(), opt.HTTPAddr, opt.HTTPSAddr, cfg.AdminAddr(), len(store.ListBindings()))

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
				fmt.Fprintf(os.Stderr, "admin reload: %v\n", err)
				return
			}
			creds.Set(a.Username, a.Password)
		}); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
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
				fmt.Fprintf(os.Stderr, "%v\n", err)
				stop()
				wg.Wait()
				_ = store.Close()
				os.Exit(1)
			}
		}
		wg.Wait()
	}
	if err := store.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
