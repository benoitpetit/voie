package providers

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These helpers are deliberately self-contained: the committed keyless tests
// must compile from a clean checkout without relying on uncommitted files.
// Their names differ from the other providers' helpers to avoid collisions.

func useKeylessProviderHandler(t *testing.T, handler http.Handler) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = keylessRoundTripper(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return &http.Response{
			StatusCode: recorder.Code,
			Header:     recorder.Header(),
			Body:       io.NopCloser(strings.NewReader(recorder.Body.String())),
			Request:    request,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
}

func useKeylessProviderResponse(t *testing.T, status int, body string) {
	t.Helper()
	useKeylessProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

func useKeylessProviderNetworkFailure(t *testing.T) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = keylessRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
}

type keylessRoundTripper func(*http.Request) (*http.Response, error)

func (f keylessRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
