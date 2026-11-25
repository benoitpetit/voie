package config

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestCredentialStoreFileMode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store, err := LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("openrouter", "sk-or-v1-x"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(CredentialsPath())
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o, want 600", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(CredentialsPath()))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("dir mode = %o, want 700", got)
	}
}

func TestCredentialStoreEnvOverridesFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store, err := LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("openrouter", "from-file"); err != nil {
		t.Fatal(err)
	}
	if got := store.Token("openrouter"); got != "from-file" {
		t.Fatalf("token before env = %q", got)
	}
	t.Setenv("VOIE_OPENROUTER_KEY", "from-env")
	if got, want := store.Token("openrouter"), "from-env"; got != want {
		t.Errorf("Token = %q, want %q", got, want)
	}
}

func TestCredentialStoreRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store, err := LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("openrouter", "a"); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("orcarouter", "b"); err != nil {
		t.Fatal(err)
	}
	names := store.Providers()
	sort.Strings(names)
	if len(names) != 2 || names[0] != "openrouter" || names[1] != "orcarouter" {
		t.Errorf("Providers = %v", names)
	}
	if err := store.Delete("openrouter"); err != nil {
		t.Fatal(err)
	}
	if got := store.Token("openrouter"); got != "" {
		t.Errorf("token after Delete = %q, want empty", got)
	}
	if got := store.Token("orcarouter"); got != "b" {
		t.Errorf("other token = %q", got)
	}
}

func TestCredentialStoreOpencodeSharesEnv(t *testing.T) {
	if got, want := ProviderKeyEnv("opencode-go"), "OPENCODE_ZEN_KEY"; got != want {
		t.Errorf("ProviderKeyEnv(opencode-go) = %q, want %q", got, want)
	}
	if got, want := ProviderKeyEnv("openrouter"), "VOIE_OPENROUTER_KEY"; got != want {
		t.Errorf("ProviderKeyEnv(openrouter) = %q, want %q", got, want)
	}
}

func TestCredentialsWriteIsAtomicAndKeeps0600(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := CredentialsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"providers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("openrouter", "token"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials mode = %o, want 600", info.Mode().Perm())
	}
	reloaded, err := LoadCredentials()
	if err != nil || reloaded.Token("openrouter") != "token" {
		t.Fatalf("reloaded = %+v, %v", reloaded, err)
	}
	if entries, err := os.ReadDir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	} else {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".credentials-") && entry.Name() != filepath.Base(path) {
				t.Fatalf("temporary credential file left behind: %s", entry.Name())
			}
		}
	}
}

func TestA2AAgentURLPersists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store, err := LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetA2AAgent("one", "https://a.example"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.A2AAgentURL("one"); got != "https://a.example" {
		t.Fatalf("URL = %q", got)
	}
	names := reloaded.A2AAgentNames()
	if len(names) != 1 || names[0] != "one" {
		t.Fatalf("names = %v", names)
	}
}
