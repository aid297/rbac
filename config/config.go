package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"

	"rbac/persist"
)

const (
	EnvConfig       = "RBAC_CONFIG"
	DefaultFileName = "config.yaml"
)

// Origin is how the config file path was chosen.
// Precedence: RBAC_CONFIG > --config > <cwd>/config.yaml.
type Origin string

const (
	OriginEnv     Origin = "env"
	OriginFlag    Origin = "flag"
	OriginDefault Origin = "default"
)

type Config struct {
	// Path is the resolved config file (absolute when possible).
	Path string `mapstructure:"-"`
	// Origin is env, flag, or default.
	Origin Origin `mapstructure:"-"`
	Policy Policy `mapstructure:"policy"`
}

type Policy struct {
	Dir string `mapstructure:"dir"`
}

func (c *Config) PolicyPath() string {
	return persist.FilePath(c.Policy.Dir)
}

func ResolvePath(flagPath string) (path string, origin Origin) {
	if v := strings.TrimSpace(os.Getenv(EnvConfig)); v != "" {
		return v, OriginEnv
	}
	if v := strings.TrimSpace(flagPath); v != "" {
		return v, OriginFlag
	}
	return defaultConfigPath(), OriginDefault
}

func defaultConfigPath() string {
	wd, err := os.Getwd()
	if err != nil {
		return DefaultFileName
	}
	return filepath.Join(wd, DefaultFileName)
}

func absPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	wd, err := os.Getwd()
	if err != nil {
		return path
	}
	return filepath.Join(wd, path)
}

// Load reads YAML via viper. Missing default file uses built-in defaults;
// a path from RBAC_CONFIG or --config must exist.
func Load(flagPath string) (*Config, error) {
	path, origin := ResolvePath(flagPath)
	path = absPath(path)

	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.SetDefault("policy.dir", persist.DefaultDir)

	if err := v.ReadInConfig(); err != nil {
		if origin == OriginDefault && isNotExist(err) {
			return &Config{
				Path:   path,
				Origin: origin,
				Policy: Policy{Dir: persist.DefaultDir},
			}, nil
		}
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	cfg := new(Config)
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}
	cfg.Path = path
	cfg.Origin = origin
	if strings.TrimSpace(cfg.Policy.Dir) == "" {
		cfg.Policy.Dir = persist.DefaultDir
	}
	return cfg, nil
}

func isNotExist(err error) bool {
	if err == nil {
		return false
	}
	var pe *os.PathError
	if errors.As(err, &pe) && errors.Is(pe.Err, os.ErrNotExist) {
		return true
	}
	return errors.Is(err, os.ErrNotExist) || os.IsNotExist(err)
}
