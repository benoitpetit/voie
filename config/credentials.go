package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CredentialsPath is the single file holding provider tokens. It is kept
// outside the routing policy so a token never leaks into a shared config.
func CredentialsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "voie", "credentials.json")
}

// ProviderKeyEnv returns the environment override for a provider token.
// The two opencode providers deliberately share the gateway key variable.
func ProviderKeyEnv(provider string) string {
	if provider == "opencode-zen" || provider == "opencode-go" {
		return "OPENCODE_ZEN_KEY"
	}
	return "VOIE_" + strings.ToUpper(strings.ReplaceAll(provider, "-", "_")) + "_KEY"
}

// Credentials is an in-memory snapshot of the credential file.
type Credentials struct {
	Tokens map[string]string `json:"providers"`
}

// LoadCredentials reads the credential file. A missing file is an empty store.
func LoadCredentials() (*Credentials, error) {
	store := &Credentials{Tokens: make(map[string]string)}
	path := CredentialsPath()
	if path == "" {
		return store, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(data, store); err != nil {
		return nil, err
	}
	if store.Tokens == nil {
		store.Tokens = make(map[string]string)
	}
	return store, nil
}

// Raw returns only the saved value, without environment override.
func (c *Credentials) Raw(provider string) string {
	return strings.TrimSpace(c.Tokens[provider])
}

// Token returns the environment override when set, otherwise the saved token.
func (c *Credentials) Token(provider string) string {
	if value := strings.TrimSpace(os.Getenv(ProviderKeyEnv(provider))); value != "" {
		return value
	}
	return strings.TrimSpace(c.Tokens[provider])
}

func (c *Credentials) write() error {
	path := CredentialsPath()
	if path == "" {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func (c *Credentials) Set(provider, token string) error {
	c.Tokens[provider] = strings.TrimSpace(token)
	return c.write()
}

func (c *Credentials) Delete(provider string) error {
	delete(c.Tokens, provider)
	return c.write()
}

func (c *Credentials) Providers() []string {
	names := make([]string, 0, len(c.Tokens))
	for name, token := range c.Tokens {
		if strings.TrimSpace(token) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
