package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Host            string
	Port            string
	EnableDebug     bool
	DefaultProvider string
	Timeout         time.Duration
	APIToken        string
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
	cfg := &Config{
		Host: host, Port: port, EnableDebug: debug,
		DefaultProvider: strings.ToLower(strings.TrimSpace(os.Getenv("DEFAULT_PROVIDER"))),
		Timeout:         timeout, APIToken: os.Getenv("API_TOKEN"),
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
	if !isLoopbackHost(c.Host) && strings.TrimSpace(c.APIToken) == "" {
		return fmt.Errorf("API_TOKEN is required when HOST is not loopback")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(strings.TrimSpace(host), "[]"))
	return ip != nil && ip.IsLoopback()
}
