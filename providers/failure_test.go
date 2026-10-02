package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// useServer redirects every outbound request through the given test server by
// swapping http.DefaultTransport; the previous transport is restored on
// cleanup.
func useServer(t *testing.T, srv *httptest.Server) {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	prev := http.DefaultTransport
	http.DefaultTransport = redirectTo{target: u, base: prev}
	t.Cleanup(func() { http.DefaultTransport = prev })
}

type redirectTo struct {
	target *url.URL
	base   http.RoundTripper
}

func (r redirectTo) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	routed := *req.URL
	routed.Scheme = r.target.Scheme
	routed.Host = r.target.Host
	clone.URL = &routed
	return r.base.RoundTrip(clone)
}

// testProviderContext is the minimal context used to exercise adapters.
func testProviderContext() context.Context { return context.Background() }
