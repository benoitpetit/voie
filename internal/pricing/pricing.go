// Package pricing exposes publicly available upstream model prices. Prices
// are only attached when a provider publishes a machine-readable catalogue;
// other providers keep their price unknown rather than guessing.
package pricing

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const defaultSource = "https://openrouter.ai/api/v1/models"

type priceSource struct {
	url   string
	token func() string
}

type priceCache struct {
	prices      map[string]Price
	fetchedAt   time.Time
	lastFailure time.Time
}

var (
	mu      sync.Mutex
	sources = map[string]priceSource{"openrouter": {url: defaultSource}}
	caches  = map[string]*priceCache{}
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

// SetSource overrides the OpenRouter catalogue URL (used by tests).
func SetSource(url string) { RegisterSource("openrouter", url, nil) }

// RegisterSource declares an authenticated catalogue for a provider. An empty
// URL disables lookups for that provider.
func RegisterSource(provider, url string, token func() string) {
	mu.Lock()
	defer mu.Unlock()
	if url == "" {
		delete(sources, provider)
		delete(caches, provider)
		return
	}
	sources[provider] = priceSource{url: url, token: token}
	delete(caches, provider)
}

// All returns the public OpenRouter catalogue keyed by upstream model ID.
func All() map[string]Price { return AllFor("openrouter") }

// AllFor returns a provider's catalogue keyed by upstream model ID.
func AllFor(provider string) map[string]Price {
	catalogue, ok := loadFor(provider)
	if !ok {
		return map[string]Price{}
	}
	out := make(map[string]Price, len(catalogue))
	for id, price := range catalogue {
		out[id] = price
	}
	return out
}

// Lookup returns the price for a provider's upstream model ID.
func Lookup(provider, upstream string) (Price, bool) {
	catalogue, ok := loadFor(provider)
	if !ok {
		return Price{}, false
	}
	price, ok := catalogue[upstream]
	return price, ok
}

func loadFor(provider string) (map[string]Price, bool) {
	mu.Lock()
	src, ok := sources[provider]
	if !ok {
		mu.Unlock()
		return nil, false
	}
	cache := caches[provider]
	if cache == nil {
		cache = &priceCache{}
		caches[provider] = cache
	}
	if cache.prices != nil && time.Since(cache.fetchedAt) <= 6*time.Hour {
		catalogue := cache.prices
		mu.Unlock()
		return catalogue, true
	}
	if !cache.lastFailure.IsZero() && time.Since(cache.lastFailure) <= time.Minute {
		mu.Unlock()
		return nil, false
	}
	url, token := src.url, src.token
	mu.Unlock()

	loaded, err := fetch(url, token)
	mu.Lock()
	defer mu.Unlock()
	if err != nil {
		cache.lastFailure = time.Now()
		return nil, false
	}
	cache.prices = loaded
	cache.fetchedAt = time.Now()
	cache.lastFailure = time.Time{}
	return cache.prices, true
}

func fetch(url string, token func() string) (map[string]Price, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != nil {
		if value := token(); value != "" {
			req.Header.Set("Authorization", "Bearer "+value)
		}
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
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
