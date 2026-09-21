package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"

	"rbac/cache"
	"rbac/crypto"
	"rbac/persist"
	"rbac/pki"
)

const (
	EnvConfig        = "RBAC_CONFIG"
	EnvPolicyKey     = "RBAC_POLICY_KEY"
	EnvPolicyKeyPrev = "RBAC_POLICY_KEY_PREV"
	EnvCachePassword = "RBAC_CACHE_PASSWORD"
	DefaultFileName  = "config.yaml"
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
	CA     CA     `mapstructure:"ca"`
	Cache  Cache  `mapstructure:"cache"`
	key    []byte
}

type CA struct {
	Dir string `mapstructure:"dir"`
}

type Cache struct {
	Enable   bool   `mapstructure:"enable"`
	Kind     string `mapstructure:"kind"`
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type Policy struct {
	Dir     string `mapstructure:"dir"`
	Encrypt *bool  `mapstructure:"encrypt"`
	Crypto  string `mapstructure:"crypto"`
	Key     string `mapstructure:"key"`
}

func (c *Config) EncryptEnabled() bool {
	return c != nil && c.Policy.Encrypt != nil && *c.Policy.Encrypt
}

func (c *Config) CADir() string {
	if c == nil || strings.TrimSpace(c.CA.Dir) == "" {
		return pki.DefaultDir
	}
	return c.CA.Dir
}

func (c *Config) PolicyPath() string {
	return persist.FilePath(c.Policy.Dir)
}

func (c *Config) CacheEnabled() bool {
	if c == nil || !c.Cache.Enable {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(c.Cache.Kind), cache.KindRedis)
}

func (c *Config) CachePassword() string {
	if env := strings.TrimSpace(os.Getenv(EnvCachePassword)); env != "" {
		return env
	}
	if c == nil {
		return ""
	}
	return c.Cache.Password
}

func (c *Config) Algorithm() (crypto.Algorithm, error) {
	return crypto.Lookup(c.Policy.Crypto)
}

func (c *Config) KeyBytes() []byte {
	return append([]byte(nil), c.key...)
}

func (c *Config) PrevKeyBytes() []byte {
	s := strings.TrimSpace(os.Getenv(EnvPolicyKeyPrev))
	if s == "" {
		return nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
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

// Load reads YAML via viper, then ensures policy.crypto and policy.key exist.
// Missing default file uses built-in defaults; RBAC_CONFIG / --config must exist.
func Load(flagPath string) (*Config, error) {
	path, origin := ResolvePath(flagPath)
	path = absPath(path)

	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.SetDefault("policy.dir", persist.DefaultDir)
	v.SetDefault("ca.dir", pki.DefaultDir)
	v.SetDefault("cache.enable", false)
	v.SetDefault("cache.kind", cache.KindRedis)
	v.SetDefault("cache.addr", cache.DefaultAddr)

	cfg := &Config{
		Path:   path,
		Origin: origin,
		Policy: Policy{Dir: persist.DefaultDir},
		CA:     CA{Dir: pki.DefaultDir},
		Cache: Cache{
			Enable: false,
			Kind:   cache.KindRedis,
			Addr:   cache.DefaultAddr,
		},
	}

	if err := v.ReadInConfig(); err != nil {
		if !(origin == OriginDefault && isNotExist(err)) {
			return nil, fmt.Errorf("config: read %s: %w", path, err)
		}
	} else {
		if err := v.Unmarshal(cfg); err != nil {
			return nil, fmt.Errorf("config: unmarshal: %w", err)
		}
		cfg.Path = path
		cfg.Origin = origin
		if strings.TrimSpace(cfg.Policy.Dir) == "" {
			cfg.Policy.Dir = persist.DefaultDir
		}
		if strings.TrimSpace(cfg.CA.Dir) == "" {
			cfg.CA.Dir = pki.DefaultDir
		}
		if strings.TrimSpace(cfg.Cache.Addr) == "" {
			cfg.Cache.Addr = cache.DefaultAddr
		}
	}

	if err := cfg.validateCache(); err != nil {
		return nil, err
	}

	if err := cfg.ensureSecrets(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validateCache() error {
	if c == nil || !c.Cache.Enable {
		return nil
	}
	kind := strings.ToLower(strings.TrimSpace(c.Cache.Kind))
	if kind != cache.KindRedis {
		return fmt.Errorf("config: cache.enable requires cache.kind: %s", cache.KindRedis)
	}
	c.Cache.Kind = cache.KindRedis
	if strings.TrimSpace(c.Cache.Addr) == "" {
		c.Cache.Addr = cache.DefaultAddr
	}
	return nil
}

func (c *Config) ensureSecrets() error {
	dirty := false
	if strings.TrimSpace(c.CA.Dir) == "" {
		c.CA.Dir = pki.DefaultDir
		dirty = true
	}
	if c.Policy.Encrypt == nil {
		off := false
		c.Policy.Encrypt = &off
		dirty = true
	}
	if strings.TrimSpace(c.Policy.Crypto) == "" {
		c.Policy.Crypto = crypto.DefaultName
		dirty = true
	}
	alg, err := crypto.Lookup(c.Policy.Crypto)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if c.Policy.Crypto != alg.Name() {
		c.Policy.Crypto = alg.Name()
		dirty = true
	}

	if env := strings.TrimSpace(os.Getenv(EnvPolicyKey)); env != "" {
		key, err := parseKey(env, alg.KeySize())
		if err != nil {
			return fmt.Errorf("config: %s: %w", EnvPolicyKey, err)
		}
		c.key = key
		if dirty {
			return c.writeFile()
		}
		return nil
	}

	if strings.TrimSpace(c.Policy.Key) != "" {
		key, err := parseKey(c.Policy.Key, alg.KeySize())
		if err != nil {
			return fmt.Errorf("config: policy.key: %w", err)
		}
		c.key = key
		if dirty {
			return c.writeFile()
		}
		return nil
	}

	if !c.EncryptEnabled() {
		if dirty {
			return c.writeFile()
		}
		return nil
	}

	raw, err := alg.GenerateKey()
	if err != nil {
		return fmt.Errorf("config: generate key: %w", err)
	}
	c.key = raw
	c.Policy.Key = hex.EncodeToString(raw)
	return c.writeFile()
}

func (c *Config) cacheKind() string {
	if c == nil || strings.TrimSpace(c.Cache.Kind) == "" {
		return cache.KindRedis
	}
	return c.Cache.Kind
}

func (c *Config) cacheAddr() string {
	if c == nil || strings.TrimSpace(c.Cache.Addr) == "" {
		return cache.DefaultAddr
	}
	return c.Cache.Addr
}

func parseKey(s string, want int) ([]byte, error) {
	key, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("key must be hex: %w", err)
	}
	if len(key) != want {
		return nil, fmt.Errorf("%w: got %d want %d", crypto.ErrKeySize, len(key), want)
	}
	return key, nil
}

func (c *Config) writeFile() error {
	body := fmt.Sprintf("policy:\n  dir: %s\n  encrypt: %t\n  crypto: %s\n  key: %s\nca:\n  dir: %s\ncache:\n  enable: %t\n  kind: %s\n  addr: %s\n  password: %s\n  db: %d\n",
		c.Policy.Dir, c.EncryptEnabled(), c.Policy.Crypto, c.Policy.Key, c.CADir(),
		c.Cache.Enable, c.cacheKind(), c.cacheAddr(), c.Cache.Password, c.Cache.DB)
	dir := filepath.Dir(c.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("config: temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, c.Path); err != nil {
		return fmt.Errorf("config: write %s: %w", c.Path, err)
	}
	return nil
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
