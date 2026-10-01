package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestListProvidersChecksHTTPReachabilityOnly(t *testing.T) {
	var requests int
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("not found")), Request: r}, nil
	})}
	registry := NewRegistry()
	provider := &testProvider{info: ProviderInfo{Name: "test", Label: "Test", URL: "https://provider.invalid", Working: true, DefaultModel: "test", SupportedModels: []string{"test"}}}
	registry.Register("test", provider)
	service, err := NewService(registry, ServiceOptions{HealthClient: client})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.ListProviders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Alive {
		t.Fatalf("providers = %+v, want reachable provider", got)
	}
	if requests != 1 {
		t.Fatalf("HTTP requests = %d, want exactly one", requests)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
