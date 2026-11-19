package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

// classifyFailure asserts err carries a *app.ProviderFailure with the expected
// category and status, and that the error string never exposes the given
// response body text.
func classifyFailure(t *testing.T, err error, category app.FailureCategory, status int, secret string) {
	t.Helper()
	var pf *app.ProviderFailure
	if !errors.As(err, &pf) {
		t.Fatalf("errors.As(*ProviderFailure) failed on %v", err)
	}
	if pf.Category != category {
		t.Errorf("Category = %v, want %v", pf.Category, category)
	}
	if pf.Status != status {
		t.Errorf("Status = %d, want %d", pf.Status, status)
	}
	if secret != "" && strings.Contains(err.Error(), secret) {
		t.Errorf("error %q leaked response body %q", err.Error(), secret)
	}
}

// statusTable returns the classification matrix asserted by every adapter:
// 429/5xx are transient, 404 is unavailable, 4xx auth is permanent.
func statusTable() []struct {
	name     string
	status   int
	category app.FailureCategory
} {
	return []struct {
		name     string
		status   int
		category app.FailureCategory
	}{
		{"429 transient", http.StatusTooManyRequests, app.FailureTransient},
		{"500 transient", http.StatusInternalServerError, app.FailureTransient},
		{"503 transient", http.StatusServiceUnavailable, app.FailureTransient},
		{"404 unavailable", http.StatusNotFound, app.FailureUnavailable},
		{"401 permanent", http.StatusUnauthorized, app.FailurePermanent},
		{"403 permanent", http.StatusForbidden, app.FailurePermanent},
	}
}

// useProviderHandler keeps adapter tests deterministic without opening sockets.
func useProviderHandler(t *testing.T, handler http.Handler) {
	t.Helper()
	useProviderRoundTripper(t, providerRoundTripper(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return &http.Response{
			StatusCode: recorder.Code,
			Header:     recorder.Header(),
			Body:       io.NopCloser(strings.NewReader(recorder.Body.String())),
			Request:    request,
		}, nil
	}))
}

func useProviderResponse(t *testing.T, status int, body string) {
	t.Helper()
	useProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

func useProviderNetworkFailure(t *testing.T) {
	t.Helper()
	useProviderRoundTripper(t, providerRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	}))
}

func useProviderRoundTripper(t *testing.T, transport http.RoundTripper) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
}

func providerHTTPClient(handler http.Handler) *http.Client {
	return &http.Client{Transport: providerRoundTripper(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return &http.Response{
			StatusCode: recorder.Code,
			Header:     recorder.Header(),
			Body:       io.NopCloser(strings.NewReader(recorder.Body.String())),
			Request:    request,
		}, nil
	})}
}

type providerRoundTripper func(*http.Request) (*http.Response, error)

func (f providerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// testProviderContext is the minimal context used to exercise adapters.
func testProviderContext() context.Context { return context.Background() }
