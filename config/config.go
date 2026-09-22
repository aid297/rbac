package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
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
	EnvConfig            = "RBAC_CONFIG"
	EnvPolicyKey         = "RBAC_POLICY_KEY"
	EnvPolicyKeyPrev     = "RBAC_POLICY_KEY_PREV"
	EnvCachePassword     = "RBAC_CACHE_PASSWORD"
	DefaultFileName      = "config.yaml"
	DefaultHTTPPort      = 8080
	DefaultHTTPSPort     = 8443
	DefaultHTTPHost      = "0.0.0.0"
	DefaultAdminUser     = "admin"
	DefaultAdminPassword = "admin"
	DefaultAdminHost     = "127.0.0.1"
	DefaultAdminPort     = 9090
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
	Server Server `mapstructure:"server"`
	Admin  Admin  `mapstructure:"admin"`
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

type Server struct {
	HTTP  HTTPEndpoint  `mapstructure:"http"`
	HTTPS HTTPSEndpoint `mapstructure:"https"`
}

type HTTPEndpoint struct {
	Enable bool   `mapstructure:"enable"`
	Host   string `mapstructure:"host"`
	Port   int    `mapstructure:"port"`
}

type HTTPSEndpoint struct {
	Enable bool `mapstructure:"enable"`
	Port   int  `mapstructure:"port"`
}

type Admin struct {
	Enable   bool   `mapstructure:"enable"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
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

func (c *Config) HTTPEnabled() bool {
	return c != nil && c.Server.HTTP.Enable
}

func (c *Config) HTTPSEnabled() bool {
	return c != nil && c.Server.HTTPS.Enable
}

func (c *Config) HTTPHost() string {
	if c == nil || strings.TrimSpace(c.Server.HTTP.Host) == "" {
		return DefaultHTTPHost
	}
	return strings.TrimSpace(c.Server.HTTP.Host)
}

func (c *Config) HTTPPort() int {
	if c == nil || c.Server.HTTP.Port == 0 {
		return DefaultHTTPPort
	}
	return c.Server.HTTP.Port
}

func (c *Config) HTTPSPort() int {
	if c == nil || c.Server.HTTPS.Port == 0 {
		return DefaultHTTPSPort
	}
	return c.Server.HTTPS.Port
}

func (c *Config) HTTPAddr() string {
	return net.JoinHostPort(c.HTTPHost(), fmt.Sprintf("%d", c.HTTPPort()))
}

func (c *Config) HTTPSAddr() string {
	return net.JoinHostPort(c.HTTPHost(), fmt.Sprintf("%d", c.HTTPSPort()))
}

func (c *Config) AdminEnabled() bool {
	return c != nil && c.Admin.Enable
}

func (c *Config) AdminUsername() string {
	if c == nil || strings.TrimSpace(c.Admin.Username) == "" {
		return DefaultAdminUser
	}
	return strings.TrimSpace(c.Admin.Username)
}

func (c *Config) AdminPassword() string {
	if c == nil || c.Admin.Password == "" {
		return DefaultAdminPassword
	}
	return c.Admin.Password
}

func (c *Config) AdminHost() string {
	if c == nil || strings.TrimSpace(c.Admin.Host) == "" {
		return DefaultAdminHost
	}
	return strings.TrimSpace(c.Admin.Host)
}

func (c *Config) AdminPort() int {
	if c == nil || c.Admin.Port == 0 {
		return DefaultAdminPort
	}
	return c.Admin.Port
}

func (c *Config) AdminAddr() string {
	return net.JoinHostPort(c.AdminHost(), fmt.Sprintf("%d", c.AdminPort()))
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
	v.SetDefault("server.http.enable", false)
	v.SetDefault("server.http.host", DefaultHTTPHost)
	v.SetDefault("server.http.port", DefaultHTTPPort)
	v.SetDefault("server.https.enable", false)
	v.SetDefault("server.https.port", DefaultHTTPSPort)
	v.SetDefault("admin.enable", false)
	v.SetDefault("admin.username", DefaultAdminUser)
	v.SetDefault("admin.password", DefaultAdminPassword)
	v.SetDefault("admin.host", DefaultAdminHost)
	v.SetDefault("admin.port", DefaultAdminPort)

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
		Server: Server{
			HTTP:  HTTPEndpoint{Host: DefaultHTTPHost, Port: DefaultHTTPPort},
			HTTPS: HTTPSEndpoint{Port: DefaultHTTPSPort},
		},
		Admin: Admin{
			Username: DefaultAdminUser,
			Password: DefaultAdminPassword,
			Host:     DefaultAdminHost,
			Port:     DefaultAdminPort,
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
		if strings.TrimSpace(cfg.Server.HTTP.Host) == "" {
			cfg.Server.HTTP.Host = DefaultHTTPHost
		}
		if cfg.Server.HTTP.Port == 0 {
			cfg.Server.HTTP.Port = DefaultHTTPPort
		}
		if cfg.Server.HTTPS.Port == 0 {
			cfg.Server.HTTPS.Port = DefaultHTTPSPort
		}
	}

	if err := cfg.validateCache(); err != nil {
		return nil, err
	}
	if err := cfg.validateServer(); err != nil {
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

func (c *Config) validateServer() error {
	if c == nil {
		return nil
	}
	if c.Server.HTTP.Port < 0 || c.Server.HTTP.Port > 65535 {
		return fmt.Errorf("config: server.http.port out of range")
	}
	if c.Server.HTTPS.Port < 0 || c.Server.HTTPS.Port > 65535 {
		return fmt.Errorf("config: server.https.port out of range")
	}
	if c.Server.HTTP.Port == 0 {
		c.Server.HTTP.Port = DefaultHTTPPort
	}
	if c.Server.HTTPS.Port == 0 {
		c.Server.HTTPS.Port = DefaultHTTPSPort
	}
	if strings.TrimSpace(c.Server.HTTP.Host) == "" {
		c.Server.HTTP.Host = DefaultHTTPHost
	}
	if c.HTTPEnabled() && c.HTTPSEnabled() && c.HTTPPort() == c.HTTPSPort() {
		return fmt.Errorf("config: server.http.port and server.https.port must differ")
	}
	if c.Admin.Port < 0 || c.Admin.Port > 65535 {
		return fmt.Errorf("config: admin.port out of range")
	}
	if c.Admin.Port == 0 {
		c.Admin.Port = DefaultAdminPort
	}
	if strings.TrimSpace(c.Admin.Host) == "" {
		c.Admin.Host = DefaultAdminHost
	}
	if c.AdminEnabled() && c.HTTPEnabled() && c.AdminPort() == c.HTTPPort() && c.AdminHost() == c.HTTPHost() {
		return fmt.Errorf("config: admin.port must differ from server.http.port on the same host")
	}
	if c.AdminEnabled() && c.HTTPSEnabled() && c.AdminPort() == c.HTTPSPort() && c.AdminHost() == c.HTTPHost() {
		return fmt.Errorf("config: admin.port must differ from server.https.port on the same host")
	}
	return nil
}

func (c *Config) ensureSecrets() error {
	dirty := false
	if strings.TrimSpace(c.CA.Dir) == "" {
		c.CA.Dir = pki.DefaultDir
		dirty = true
	}
	if strings.TrimSpace(c.Admin.Username) == "" {
		c.Admin.Username = DefaultAdminUser
		dirty = true
	}
	if c.Admin.Password == "" {
		c.Admin.Password = DefaultAdminPassword
		dirty = true
	}
	if strings.TrimSpace(c.Admin.Host) == "" {
		c.Admin.Host = DefaultAdminHost
		dirty = true
	}
	if c.Admin.Port == 0 {
		c.Admin.Port = DefaultAdminPort
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
	body := fmt.Sprintf("policy:\n  dir: %s\n  encrypt: %t\n  crypto: %s\n  key: %s\nca:\n  dir: %s\ncache:\n  enable: %t\n  kind: %s\n  addr: %s\n  password: %s\n  db: %d\nserver:\n  http:\n    enable: %t\n    host: %s\n    port: %d\n  https:\n    enable: %t\n    port: %d\nadmin:\n  enable: %t\n  username: %s\n  password: %s\n  host: %s\n  port: %d\n",
		c.Policy.Dir, c.EncryptEnabled(), c.Policy.Crypto, c.Policy.Key, c.CADir(),
		c.Cache.Enable, c.cacheKind(), c.cacheAddr(), c.Cache.Password, c.Cache.DB,
		c.HTTPEnabled(), c.HTTPHost(), c.HTTPPort(), c.HTTPSEnabled(), c.HTTPSPort(),
		c.AdminEnabled(), c.AdminUsername(), c.AdminPassword(), c.AdminHost(), c.AdminPort())
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
