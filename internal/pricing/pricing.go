// Package pricing exposes publicly available upstream model prices. Only
// OpenRouter publishes a public machine-readable catalogue; other providers
// leave the price unknown rather than guessing.
package pricing

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const defaultSource = "https://openrouter.ai/api/v1/models"

var (
	mu          sync.Mutex
	source      = defaultSource
	prices      map[string]Price
	fetchedAt   time.Time
	lastFailure time.Time
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
	lastFailure = time.Time{}
}

// All returns the public catalogue keyed by upstream model ID.
func All() map[string]Price {
	catalogue, ok := load()
	if !ok {
		return map[string]Price{}
	}
	out := make(map[string]Price, len(catalogue))
	for id, price := range catalogue {
		out[id] = price
	}
	return out
}

// Lookup returns the price for a provider's upstream model ID when a public
// price catalogue is available.
func Lookup(provider, upstream string) (Price, bool) {
	if !strings.EqualFold(provider, "openrouter") {
		return Price{}, false
	}
	catalogue, ok := load()
	if !ok {
		return Price{}, false
	}
	price, ok := catalogue[upstream]
	return price, ok
}

func load() (map[string]Price, bool) {
	mu.Lock()
	if prices != nil && time.Since(fetchedAt) <= 6*time.Hour {
		catalogue := prices
		mu.Unlock()
		return catalogue, true
	}
	if !lastFailure.IsZero() && time.Since(lastFailure) <= time.Minute {
		mu.Unlock()
		return nil, false
	}
	url := source
	mu.Unlock()
	if url == "" {
		return nil, false
	}
	loaded, err := fetch(url)
	mu.Lock()
	defer mu.Unlock()
	if err != nil {
		lastFailure = time.Now()
		return nil, false
	}
	prices = loaded
	fetchedAt = time.Now()
	lastFailure = time.Time{}
	return prices, true
}

func fetch(url string) (map[string]Price, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("price catalogue: HTTP %d", resp.StatusCode)
	}
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
