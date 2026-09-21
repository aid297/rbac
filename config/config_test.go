package config

import (
	"os"
	"path/filepath"
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
}

func TestMissingExplicitFileErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(EnvConfig, "")
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
	mustWrite(t, filepath.Join(dir, DefaultFileName), "policy:\n  dir: \"\"\n")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Policy.Dir != persist.DefaultDir {
		t.Fatalf("dir=%s", cfg.Policy.Dir)
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

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
