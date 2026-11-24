// Package pricing exposes publicly available upstream model prices. Only
// OpenRouter publishes a public machine-readable catalogue; other providers
// leave the price unknown rather than guessing.
package pricing

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

const defaultSource = "https://openrouter.ai/api/v1/models"

var (
	mu        sync.Mutex
	source    = defaultSource
	prices    map[string]Price
	fetchedAt time.Time
)

// Price is the upstream USD price per token.
type Price struct {
	Prompt     string
	Completion string
}

type response struct {
	Data []struct {
		ID      string `json:"id"`
		Pricing struct {
			Prompt     string `json:"prompt"`
			Completion string `json:"completion"`
		} `json:"pricing"`
	} `json:"data"`
}

// SetSource overrides the price catalogue URL. An empty URL disables lookup.
func SetSource(url string) {
	mu.Lock()
	defer mu.Unlock()
	source = url
	prices = nil
	fetchedAt = time.Time{}
}

// Lookup returns the price for a provider's upstream model ID when a public
// price catalogue is available.
func Lookup(provider, upstream string) (Price, bool) {
	if !strings.EqualFold(provider, "openrouter") {
		return Price{}, false
	}
	mu.Lock()
	defer mu.Unlock()
	if time.Since(fetchedAt) > 6*time.Hour || prices == nil {
		if source == "" {
			return Price{}, false
		}
		loaded, err := fetch(source)
		if err != nil {
			return Price{}, false
		}
		prices = loaded
		fetchedAt = time.Now()
	}
	price, ok := prices[upstream]
	return price, ok
}

func fetch(url string) (map[string]Price, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload response
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	result := make(map[string]Price, len(payload.Data))
	for _, model := range payload.Data {
		if model.ID == "" {
			continue
		}
		result[model.ID] = Price{Prompt: model.Pricing.Prompt, Completion: model.Pricing.Completion}
	}
	return result, nil
}
