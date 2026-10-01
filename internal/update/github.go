package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type latestRelease struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func fetchLatestRelease(ctx context.Context, client *http.Client, apiBase *url.URL, owner, repository string) (*latestRelease, error) {
	endpoint := *apiBase
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repository) + "/releases/latest"
	endpoint.RawPath = ""
	body, err := getBytes(ctx, client, endpoint.String(), maxReleaseJSON)
	if err != nil {
		return nil, fmt.Errorf("fetch latest GitHub release: %w", err)
	}
	var release latestRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("decode latest GitHub release: %w", err)
	}
	if strings.TrimSpace(release.TagName) == "" {
		return nil, fmt.Errorf("latest GitHub release has no tag name")
	}
	return &release, nil
}

func getBytes(ctx context.Context, client *http.Client, endpoint string, maxSize int64) ([]byte, error) {
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "voie-updater")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("HTTP %s", response.Status)
	}
	if response.ContentLength > maxSize {
		return nil, fmt.Errorf("response exceeds %d-byte limit", maxSize)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxSize {
		return nil, fmt.Errorf("response exceeds %d-byte limit", maxSize)
	}
	return body, nil
}
