package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("HOST", "")
	t.Setenv("PORT", "")
	t.Setenv("DEBUG", "")
	t.Setenv("DEFAULT_PROVIDER", "")
	t.Setenv("TIMEOUT", "")
	t.Setenv("API_TOKEN", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != "8080" || cfg.Timeout.String() != "2m0s" {
		t.Fatalf("defaults = %+v", cfg)
	}
}

func TestLoadValidOverrides(t *testing.T) {
	t.Setenv("HOST", "0.0.0.0")
	t.Setenv("PORT", "9090")
	t.Setenv("DEBUG", "true")
	t.Setenv("DEFAULT_PROVIDER", "duckai")
	t.Setenv("TIMEOUT", "45")
	t.Setenv("API_TOKEN", "secret")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "0.0.0.0" || cfg.Port != "9090" || !cfg.EnableDebug || cfg.DefaultProvider != "duckai" || cfg.Timeout.String() != "45s" || cfg.APIToken != "secret" {
		t.Fatalf("overrides = %+v", cfg)
	}
}

func TestLoadRejectsInvalidPortAndTimeout(t *testing.T) {
	t.Setenv("PORT", "70000")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted out-of-range port")
	}
	t.Setenv("PORT", "8080")
	t.Setenv("TIMEOUT", "0")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted zero timeout")
	}
}

func TestLoadRequiresTokenForNonLoopbackHost(t *testing.T) {
	t.Setenv("HOST", "0.0.0.0")
	t.Setenv("API_TOKEN", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted non-loopback host without API_TOKEN")
	}
}

func TestLoopbackDoesNotRequireToken(t *testing.T) {
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("API_TOKEN", "")
	if _, err := Load(); err != nil {
		t.Fatalf("Load() rejected loopback without token: %v", err)
	}
}
