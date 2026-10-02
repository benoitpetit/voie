package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("HOST", "")
	t.Setenv("PORT", "")
	t.Setenv("DEBUG", "")
	t.Setenv("DEFAULT_PROVIDER", "")
	t.Setenv("TIMEOUT", "")
	t.Setenv("API_TOKEN", "")
	t.Setenv("ROUTER_MODEL", "")
	t.Setenv("SYNTHESIS_MODEL", "")
	t.Setenv("ROUTING_CONFIG_PATH", "")
	t.Setenv("CONVERSATION_DB_PATH", "")
	t.Setenv("CONVERSATION_TTL", "")
	t.Setenv("FALLBACK_ENABLED", "")
	t.Setenv("FALLBACK_MAX_RETRIES", "")
	t.Setenv("FALLBACK_MAX_MODELS", "")
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != "8080" || cfg.Timeout.String() != "2m0s" {
		t.Fatalf("defaults = %+v", cfg)
	}
	if cfg.ConversationTTL != 720*time.Hour || cfg.RoutingConfigPath != filepath.Join(configHome, "voie", "routing.json") || cfg.ConversationDBPath != filepath.Join(configHome, "voie", "conversations.db") {
		t.Fatalf("routing/conversation defaults = %+v", cfg)
	}
	if cfg.FallbackEnabled != nil || cfg.FallbackMaxRetries != nil || cfg.FallbackMaxModels != nil {
		t.Fatalf("fallback defaults must stay nil = %+v", cfg)
	}
}

func TestLoadValidOverrides(t *testing.T) {
	t.Setenv("HOST", "0.0.0.0")
	t.Setenv("PORT", "9090")
	t.Setenv("DEBUG", "true")
	t.Setenv("DEFAULT_PROVIDER", "duckai")
	t.Setenv("TIMEOUT", "45")
	t.Setenv("API_TOKEN", "secret")
	t.Setenv("ROUTER_MODEL", "provider/router")
	t.Setenv("SYNTHESIS_MODEL", "provider/synthesis")
	t.Setenv("ROUTING_CONFIG_PATH", "/tmp/routing.json")
	t.Setenv("CONVERSATION_DB_PATH", "/tmp/conversations.db")
	t.Setenv("CONVERSATION_TTL", "48h")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "0.0.0.0" || cfg.Port != "9090" || !cfg.EnableDebug || cfg.DefaultProvider != "duckai" || cfg.Timeout.String() != "45s" || cfg.APIToken != "secret" {
		t.Fatalf("overrides = %+v", cfg)
	}
	if cfg.RouterModel != "provider/router" || cfg.SynthesisModel != "provider/synthesis" || cfg.RoutingConfigPath != "/tmp/routing.json" || cfg.ConversationDBPath != "/tmp/conversations.db" || cfg.ConversationTTL != 48*time.Hour {
		t.Fatalf("routing/conversation overrides = %+v", cfg)
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

func TestLoadRejectsInvalidConversationTTL(t *testing.T) {
	t.Setenv("CONVERSATION_TTL", "0")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted zero conversation TTL")
	}
	t.Setenv("CONVERSATION_TTL", "not-a-duration")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted invalid conversation TTL")
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

func TestLoadParsesFallbackEnvironment(t *testing.T) {
	t.Setenv("FALLBACK_ENABLED", "false")
	t.Setenv("FALLBACK_MAX_RETRIES", "3")
	t.Setenv("FALLBACK_MAX_MODELS", "2")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FallbackEnabled == nil || *cfg.FallbackEnabled {
		t.Fatalf("FallbackEnabled = %v, want false", cfg.FallbackEnabled)
	}
	if cfg.FallbackMaxRetries == nil || *cfg.FallbackMaxRetries != 3 {
		t.Fatalf("FallbackMaxRetries = %v, want 3", cfg.FallbackMaxRetries)
	}
	if cfg.FallbackMaxModels == nil || *cfg.FallbackMaxModels != 2 {
		t.Fatalf("FallbackMaxModels = %v, want 2", cfg.FallbackMaxModels)
	}
}

func TestLoadRejectsInvalidFallbackVariables(t *testing.T) {
	t.Setenv("FALLBACK_ENABLED", "not-a-bool")
	t.Setenv("FALLBACK_MAX_RETRIES", "")
	t.Setenv("FALLBACK_MAX_MODELS", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "FALLBACK_ENABLED") {
		t.Fatalf("Load() = %v, want FALLBACK_ENABLED error", err)
	}

	t.Setenv("FALLBACK_ENABLED", "")
	t.Setenv("FALLBACK_MAX_RETRIES", "not-a-number")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "FALLBACK_MAX_RETRIES") {
		t.Fatalf("Load() = %v, want FALLBACK_MAX_RETRIES error", err)
	}

	t.Setenv("FALLBACK_MAX_RETRIES", "")
	t.Setenv("FALLBACK_MAX_MODELS", "-1")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "FALLBACK_MAX_MODELS") {
		t.Fatalf("Load() = %v, want FALLBACK_MAX_MODELS error", err)
	}
}
