package app

import (
	"context"
	"net/http"
)

func defaultHealthProbe(ctx context.Context, providerURL string, client *http.Client) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, providerURL, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}
