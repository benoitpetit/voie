package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/transport/a2a"
)

// A2ACardDir is where discovered Agent Cards are cached.
func A2ACardDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "voie", "gateways", "a2a")
}

func a2aCardPath(name string) string {
	dir := A2ACardDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, strings.ToLower(strings.TrimSpace(name))+".json")
}

// FetchA2ACard discovers an agent card by its well-known URI, falling back to
// the URL itself for agents that publish the card at their base address.
func FetchA2ACard(url, token string) (a2a.AgentCard, error) {
	base := strings.TrimRight(strings.TrimSpace(url), "/")
	if base == "" {
		return a2a.AgentCard{}, fmt.Errorf("A2A URL is empty")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	candidates := []string{base + "/.well-known/agent-card.json", base}
	var lastErr error
	for _, endpoint := range candidates {
		request, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			lastErr = err
			continue
		}
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := client.Do(request)
		if err != nil {
			lastErr = err
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			_ = response.Body.Close()
			lastErr = fmt.Errorf("agent card %s: HTTP %d", endpoint, response.StatusCode)
			continue
		}
		var card a2a.AgentCard
		err = json.NewDecoder(response.Body).Decode(&card)
		_ = response.Body.Close()
		if err == nil && len(card.SupportedInterfaces) > 0 {
			return card, nil
		}
		if err == nil {
			err = fmt.Errorf("agent card %s has no supported interfaces", endpoint)
		}
		lastErr = err
	}
	return a2a.AgentCard{}, fmt.Errorf("fetch A2A agent card: %w", lastErr)
}

// ConnectA2A stores a named agent, caches its card and marks it connected.
func ConnectA2A(store *config.Credentials, name, url, token string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return fmt.Errorf("A2A agent name is required")
	}
	card, err := FetchA2ACard(url, token)
	if err != nil {
		return err
	}
	if err := store.SetA2AAgent(name, url); err != nil {
		return fmt.Errorf("save A2A agent %s: %w", name, err)
	}
	if token != "" {
		if err := store.Set(name, token); err != nil {
			return fmt.Errorf("save A2A token: %w", err)
		}
	}
	if err := store.SetState(name, "connected"); err != nil {
		return fmt.Errorf("record A2A state: %w", err)
	}
	path := a2aCardPath(name)
	if path == "" {
		return fmt.Errorf("cannot resolve user config directory")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// ReadA2ACard returns the cached card of a connected agent.
func ReadA2ACard(name string) (a2a.AgentCard, error) {
	path := a2aCardPath(name)
	if path == "" {
		return a2a.AgentCard{}, fmt.Errorf("cannot resolve user config directory")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return a2a.AgentCard{}, err
	}
	var card a2a.AgentCard
	if err := json.Unmarshal(data, &card); err != nil {
		return a2a.AgentCard{}, err
	}
	return card, nil
}

// A2AModelIDs returns the public voie model IDs advertised by one agent.
func A2AModelIDs(name string) []string {
	card, err := ReadA2ACard(name)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(card.Skills))
	for _, skill := range card.Skills {
		if skill.ID == "" {
			continue
		}
		ids = append(ids, "a2a-"+strings.ToLower(strings.TrimSpace(name))+"-"+skill.ID)
	}
	return ids
}
