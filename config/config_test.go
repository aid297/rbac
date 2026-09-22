package config

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rbac/persist"
)

func TestEnvBeatsFlagBeatsDefault(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	defaultFile := filepath.Join(dir, DefaultFileName)
	flagFile := filepath.Join(dir, "flag.yaml")
	envFile := filepath.Join(dir, "env.yaml")
	mustWrite(t, defaultFile, "policy:\n  dir: from-default\n")
	mustWrite(t, flagFile, "policy:\n  dir: from-flag\n")
	mustWrite(t, envFile, "policy:\n  dir: from-env\n")

	t.Setenv(EnvConfig, "")
	t.Setenv(EnvPolicyKey, "")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Origin != OriginDefault || cfg.Policy.Dir != "from-default" {
		t.Fatalf("default: origin=%s dir=%s path=%s", cfg.Origin, cfg.Policy.Dir, cfg.Path)
	}
	if cfg.PolicyPath() != persist.FilePath("from-default") {
		t.Fatalf("PolicyPath=%s", cfg.PolicyPath())
	}
	if cfg.EncryptEnabled() || cfg.Policy.Crypto != "aes-256-gcm" {
		t.Fatalf("encrypt=%t crypto=%s", cfg.EncryptEnabled(), cfg.Policy.Crypto)
	}
	raw, err := os.ReadFile(defaultFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "encrypt: false") || !strings.Contains(string(raw), "crypto: aes-256-gcm") {
		t.Fatalf("backfill not written: %s", raw)
	}

	cfg, err = Load(flagFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Origin != OriginFlag || cfg.Policy.Dir != "from-flag" {
		t.Fatalf("flag: origin=%s dir=%s", cfg.Origin, cfg.Policy.Dir)
	}

	t.Setenv(EnvConfig, envFile)
	cfg, err = Load(flagFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Origin != OriginEnv || cfg.Policy.Dir != "from-env" {
		t.Fatalf("env: origin=%s dir=%s", cfg.Origin, cfg.Policy.Dir)
	}
}

func TestMissingDefaultUsesBuiltins(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(EnvConfig, "")
	t.Setenv(EnvPolicyKey, "")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Origin != OriginDefault {
		t.Fatalf("origin=%s", cfg.Origin)
	}
	if cfg.Policy.Dir != persist.DefaultDir {
		t.Fatalf("dir=%s", cfg.Policy.Dir)
	}
	if cfg.PolicyPath() != persist.DefaultPath() {
		t.Fatalf("PolicyPath=%s", cfg.PolicyPath())
	}
	if cfg.CADir() != "secret" {
		t.Fatalf("CADir=%s", cfg.CADir())
	}
	if _, err := os.Stat(cfg.Path); err != nil {
		t.Fatal("expected generated config.yaml")
	}
}

func TestMissingExplicitFileErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(EnvConfig, "")
	t.Setenv(EnvPolicyKey, "")
	_, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("expected error")
	}

	t.Setenv(EnvConfig, filepath.Join(t.TempDir(), "missing-env.yaml"))
	_, err = Load("")
	if err == nil {
		t.Fatal("expected env error")
	}
}

func TestEmptyPolicyDirFallsBack(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv(EnvConfig, "")
	t.Setenv(EnvPolicyKey, "")
	mustWrite(t, filepath.Join(dir, DefaultFileName), "policy:\n  dir: \"\"\nca:\n  dir: \"\"\n")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Policy.Dir != persist.DefaultDir {
		t.Fatalf("dir=%s", cfg.Policy.Dir)
	}
	if cfg.CADir() != "secret" {
		t.Fatalf("CADir=%s", cfg.CADir())
	}
}

func TestEnvPolicyKeyNotWritten(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	path := filepath.Join(dir, DefaultFileName)
	mustWrite(t, path, "policy:\n  dir: stats\n")
	key := strings.Repeat("ab", 32)
	t.Setenv(EnvConfig, "")
	t.Setenv(EnvPolicyKey, key)
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(cfg.KeyBytes()) != key {
		t.Fatal("runtime key mismatch")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), key) {
		t.Fatal("env key must not be written to config")
	}
}

func TestEmptyEncryptBackfilledFalse(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	path := filepath.Join(dir, DefaultFileName)
	mustWrite(t, path, "policy:\n  dir: stats\n  encrypt:\n  crypto:\n  key:\n")
	t.Setenv(EnvConfig, "")
	t.Setenv(EnvPolicyKey, "")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EncryptEnabled() {
		t.Fatal("empty encrypt should become false")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "encrypt: false") {
		t.Fatalf("encrypt backfill not written: %s", raw)
	}
}

func TestEmptyCryptoBackfilled(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	path := filepath.Join(dir, DefaultFileName)
	mustWrite(t, path, "policy:\n  dir: stats\n  encrypt: true\n  crypto:\n  key:\n")
	t.Setenv(EnvConfig, "")
	t.Setenv(EnvPolicyKey, "")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Policy.Crypto != "aes-256-gcm" {
		t.Fatalf("crypto=%s", cfg.Policy.Crypto)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "crypto: aes-256-gcm") {
		t.Fatalf("backfill not written: %s", raw)
	}
}

func TestEncryptFalseSkipsKey(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	path := filepath.Join(dir, DefaultFileName)
	mustWrite(t, path, "policy:\n  dir: stats\n  encrypt: false\n")
	t.Setenv(EnvConfig, "")
	t.Setenv(EnvPolicyKey, "")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EncryptEnabled() {
		t.Fatal("encrypt should stay false")
	}
	if cfg.Policy.Crypto != "aes-256-gcm" {
		t.Fatalf("crypto=%s", cfg.Policy.Crypto)
	}
	if cfg.Policy.Key != "" || len(cfg.KeyBytes()) != 0 {
		t.Fatal("key should not be generated when encrypt is false")
	}
}

func TestResolvePathPriority(t *testing.T) {
	t.Setenv(EnvConfig, "env.yaml")
	p, o := ResolvePath("flag.yaml")
	if p != "env.yaml" || o != OriginEnv {
		t.Fatalf("got %s %s", p, o)
	}
	t.Setenv(EnvConfig, "")
	p, o = ResolvePath("flag.yaml")
	if p != "flag.yaml" || o != OriginFlag {
		t.Fatalf("got %s %s", p, o)
	}
}

func TestCacheDisabledByDefault(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(EnvConfig, "")
	t.Setenv(EnvPolicyKey, "")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CacheEnabled() {
		t.Fatal("cache should be off")
	}
}

func TestCacheEnableRequiresRedisKind(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv(EnvConfig, "")
	t.Setenv(EnvPolicyKey, "")
	mustWrite(t, filepath.Join(dir, DefaultFileName), "cache:\n  enable: true\n  kind: memory\n")
	if _, err := Load(""); err == nil {
		t.Fatal("expected error")
	}
	mustWrite(t, filepath.Join(dir, DefaultFileName), "cache:\n  enable: true\n  kind: redis\n")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.CacheEnabled() || cfg.Cache.Addr == "" {
		t.Fatalf("enable=%t addr=%s", cfg.CacheEnabled(), cfg.Cache.Addr)
	}
}

func TestServerDefaultsAndSamePortRejected(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(EnvConfig, "")
	t.Setenv(EnvPolicyKey, "")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPEnabled() || cfg.HTTPSEnabled() {
		t.Fatal("servers should be off")
	}
	if cfg.HTTPPort() != DefaultHTTPPort || cfg.HTTPSPort() != DefaultHTTPSPort {
		t.Fatalf("ports %d %d", cfg.HTTPPort(), cfg.HTTPSPort())
	}
	if cfg.HTTPAddr() != "0.0.0.0:8080" {
		t.Fatalf("addr=%s", cfg.HTTPAddr())
	}

	dir := t.TempDir()
	t.Chdir(dir)
	mustWrite(t, filepath.Join(dir, DefaultFileName), "server:\n  http:\n    enable: true\n    port: 8443\n  https:\n    enable: true\n    port: 8443\n")
	if _, err := Load(""); err == nil {
		t.Fatal("expected same-port error")
	}
}

func TestAdminDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(EnvConfig, "")
	t.Setenv(EnvPolicyKey, "")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdminEnabled() {
		t.Fatal("admin should be off")
	}
	if cfg.AdminUsername() != DefaultAdminUser || cfg.AdminPassword() != DefaultAdminPassword {
		t.Fatalf("user=%s", cfg.AdminUsername())
	}
	if cfg.AdminAddr() != "127.0.0.1:9090" {
		t.Fatalf("addr=%s", cfg.AdminAddr())
	}
}

func TestAdminEmptyCredsBecomeDefault(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv(EnvConfig, "")
	t.Setenv(EnvPolicyKey, "")
	mustWrite(t, filepath.Join(dir, DefaultFileName), "admin:\n  enable: true\n  username:\n  password:\n")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AdminEnabled() {
		t.Fatal("enable")
	}
	if cfg.AdminUsername() != "admin" || cfg.AdminPassword() != "admin" {
		t.Fatalf("%s %s", cfg.AdminUsername(), cfg.AdminPassword())
	}
}

func TestReadAdminFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFileName)
	mustWrite(t, path, "admin:\n  enable: true\n  username: ops\n  password: p@ss\n  host: 10.0.0.2\n  port: 9191\n")
	a, err := ReadAdmin(path)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Enable || a.Username != "ops" || a.Password != "p@ss" || a.Host != "10.0.0.2" || a.Port != 9191 {
		t.Fatalf("%+v", a)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
