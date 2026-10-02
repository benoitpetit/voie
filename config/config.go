package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Host               string
	Port               string
	EnableDebug        bool
	DefaultProvider    string
	Timeout            time.Duration
	APIToken           string
	RouterModel        string
	SynthesisModel     string
	RoutingConfigPath  string
	ConversationDBPath string
	ConversationTTL    time.Duration
	FallbackEnabled    *bool
	FallbackMaxRetries *int
	FallbackMaxModels  *int
}

func Load() (*Config, error) {
	host := strings.TrimSpace(os.Getenv("HOST"))
	if host == "" {
		host = "127.0.0.1"
	}
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	debug := false
	if value := strings.TrimSpace(os.Getenv("DEBUG")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("invalid DEBUG value %q: %w", value, err)
		}
		debug = parsed
	}
	timeout := 120 * time.Second
	if value := strings.TrimSpace(os.Getenv("TIMEOUT")); value != "" {
		seconds, err := strconv.Atoi(value)
		if err != nil || seconds <= 0 {
			return nil, fmt.Errorf("TIMEOUT must be a positive number of seconds")
		}
		timeout = time.Duration(seconds) * time.Second
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user configuration directory: %w", err)
	}
	routingConfigPath := strings.TrimSpace(os.Getenv("ROUTING_CONFIG_PATH"))
	if routingConfigPath == "" {
		routingConfigPath = filepath.Join(configDir, "voie", "routing.json")
	}
	conversationDBPath := strings.TrimSpace(os.Getenv("CONVERSATION_DB_PATH"))
	if conversationDBPath == "" {
		conversationDBPath = filepath.Join(configDir, "voie", "conversations.db")
	}
	conversationTTL := 720 * time.Hour
	if value := strings.TrimSpace(os.Getenv("CONVERSATION_TTL")); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("CONVERSATION_TTL must be a positive duration")
		}
		conversationTTL = parsed
	}
	fallbackEnabled, err := parseOptionalBool("FALLBACK_ENABLED")
	if err != nil {
		return nil, err
	}
	fallbackMaxRetries, err := parseOptionalInt("FALLBACK_MAX_RETRIES")
	if err != nil {
		return nil, err
	}
	fallbackMaxModels, err := parseOptionalInt("FALLBACK_MAX_MODELS")
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host: host, Port: port, EnableDebug: debug,
		DefaultProvider: strings.ToLower(strings.TrimSpace(os.Getenv("DEFAULT_PROVIDER"))),
		Timeout:         timeout, APIToken: os.Getenv("API_TOKEN"),
		RouterModel:       strings.TrimSpace(os.Getenv("ROUTER_MODEL")),
		SynthesisModel:    strings.TrimSpace(os.Getenv("SYNTHESIS_MODEL")),
		RoutingConfigPath: routingConfigPath, ConversationDBPath: conversationDBPath,
		ConversationTTL:    conversationTTL,
		FallbackEnabled:    fallbackEnabled,
		FallbackMaxRetries: fallbackMaxRetries,
		FallbackMaxModels:  fallbackMaxModels,
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("configuration is required")
	}
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("HOST must not be empty")
	}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("PORT must be an integer from 1 to 65535")
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("TIMEOUT must be positive")
	}
	if c.ConversationTTL == 0 {
		c.ConversationTTL = 720 * time.Hour
	}
	if c.ConversationTTL < 0 {
		return fmt.Errorf("CONVERSATION_TTL must be positive")
	}
	if !isLoopbackHost(c.Host) && strings.TrimSpace(c.APIToken) == "" {
		return fmt.Errorf("API_TOKEN is required when HOST is not loopback")
	}
	for name, value := range map[string]*int{"FALLBACK_MAX_RETRIES": c.FallbackMaxRetries, "FALLBACK_MAX_MODELS": c.FallbackMaxModels} {
		if value != nil && (*value < 0 || *value > 3) {
			return fmt.Errorf("%s must be between 0 and 3", name)
		}
	}
	return nil
}

func parseOptionalBool(name string) (*bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return nil, fmt.Errorf("invalid %s value %q: %w", name, value, err)
	}
	return &parsed, nil
}

func parseOptionalInt(name string) (*int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil, fmt.Errorf("invalid %s value %q: %w", name, value, err)
	}
	return &parsed, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(strings.TrimSpace(host), "[]"))
	return ip != nil && ip.IsLoopback()
}
