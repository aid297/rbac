package main

import (
	"flag"
	"fmt"
	"os"

	"rbac/config"
	"rbac/persist"
)

func main() {
	configPath := flag.String("config", "", "config file path (RBAC_CONFIG overrides this; default is ./config.yaml)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	store, err := persist.Open(cfg.PolicyPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("config_file=%s origin=%s policy_dir=%s policy_file=%s bindings=%d\n",
		cfg.Path, cfg.Origin, cfg.Policy.Dir, store.Path(), len(store.ListBindings()))
}
