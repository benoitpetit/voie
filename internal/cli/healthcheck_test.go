package cli

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type healthcheckRoundTripper func(*http.Request) (*http.Response, error)

func (f healthcheckRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestHealthcheckUsesProtectedLoopbackEndpoint(t *testing.T) {
	t.Setenv("PORT", "8123")
	t.Setenv("API_TOKEN", "origin-secret")
	var checked *http.Request
	client := &http.Client{Transport: healthcheckRoundTripper(func(request *http.Request) (*http.Response, error) {
		checked = request
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header), Request: request}, nil
	})}
	command := newHealthcheckCommand(client)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if checked == nil || checked.URL.String() != "http://127.0.0.1:8123/health" {
		t.Fatalf("health check request = %v", checked)
	}
	if checked.Header.Get("Authorization") != "Bearer origin-secret" {
		t.Fatalf("authorization header = %q", checked.Header.Get("Authorization"))
	}
}

func TestHealthcheckFailsWhenServiceIsNotHealthy(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("API_TOKEN", "origin-secret")
	client := &http.Client{Transport: healthcheckRoundTripper(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: request}, nil
	})}
	if err := newHealthcheckCommand(client).Execute(); err == nil {
		t.Fatal("health check succeeded for an unauthorized health endpoint")
	}
}
