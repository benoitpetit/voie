package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/benoitpetit/voie/config"
)

func TestBearerAuthenticationProtectsAllHTTPRoutes(t *testing.T) {
	service, _ := httpTestService(t)
	handler := NewHandler(service, &config.Config{Host: "0.0.0.0", Port: "8080", Timeout: time.Minute, APIToken: "secret"})
	for _, path := range []string{"/", "/health", "/v1/models", "/v1/providers", "/v1/chat/completions"} {
		t.Run(path, func(t *testing.T) {
			r := httptest.NewRecorder()
			handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
			if r.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", r.Code)
			}
		})
	}
}

func TestBearerAuthenticationAcceptsCorrectToken(t *testing.T) {
	service, _ := httpTestService(t)
	handler := NewHandler(service, &config.Config{Host: "0.0.0.0", Port: "8080", Timeout: time.Minute, APIToken: "secret"})
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
}

func TestCORSRejectsCrossOriginRequestsToLocalAPI(t *testing.T) {
	service, _ := httpTestService(t)
	handler := NewHandler(service, &config.Config{Host: "127.0.0.1", Port: "8080", Timeout: time.Minute})
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/v1/chat/completions", nil)
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d, want 403", response.Code)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("cross-origin allow-origin = %q, want empty", got)
	}
}
