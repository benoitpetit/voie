package clinebridge

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"
)

const defaultFreeFeed = "https://api.cline.bot/api/v1/ai/cline/recommended-models"

// Config is the local bridge configuration.
type Config struct {
	Listen      string
	ClinePath   string
	AgentArgs   []string
	ScratchDir  string
	HookPath    string
	LocalKey    string
	Model       string
	FreeFeedURL string
}

type freeFeed struct {
	Free []struct {
		ID string `json:"id"`
	} `json:"free"`
}

var (
	freeMu             sync.Mutex
	freeIDs            []string
	freeUntil          time.Time
	currentFreeFeedURL string
)

// freeModels returns the live Cline free-model feed, cached for ten minutes.
func freeModels(ctx context.Context) ([]string, error) {
	freeMu.Lock()
	defer freeMu.Unlock()
	if time.Now().Before(freeUntil) && len(freeIDs) > 0 {
		return append([]string(nil), freeIDs...), nil
	}
	url := os.Getenv("VOIE_CLINE_FREE_FEED_URL")
	if url == "" {
		url = currentFreeFeedURL
	}
	if url == "" {
		url = defaultFreeFeed
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Cline/3.0.73")
	req.Header.Set("X-CLIENT-TYPE", "cline-sdk")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var feed freeFeed
	if err := json.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(feed.Free))
	for _, item := range feed.Free {
		if item.ID != "" {
			ids = append(ids, item.ID)
		}
	}
	freeIDs = ids
	freeUntil = time.Now().Add(10 * time.Minute)
	return append([]string(nil), ids...), nil
}
