package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
)

func TestBearerAuthenticationProtectsAllHTTPRoutes(t *testing.T) {
	service, _ := httpTestService(t)
	handler := NewHandler(service, &config.Config{Host: "0.0.0.0", Port: "8080", Timeout: time.Minute, APIToken: "secret"})
	for _, path := range []string{"/", "/health", "/v1/models", "/v1/providers", "/v1/chat/completions", "/v1/conversations", "/v1/conversations/id"} {
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

func TestConversationScopeCanBeRequiredForHostedAPI(t *testing.T) {
	service, _ := httpTestService(t)
	handler := NewHandler(service, &config.Config{
		Host: "0.0.0.0", Port: "8080", Timeout: time.Minute,
		APIToken: "secret", RequireConversationScope: true,
	})
	for _, test := range []struct {
		name       string
		path       string
		scope      string
		wantStatus int
	}{
		{name: "health is exempt", path: "/health", wantStatus: http.StatusOK},
		{name: "missing scope is rejected", path: "/v1/models", wantStatus: http.StatusUnauthorized},
		{name: "valid scope is accepted", path: "/v1/models", scope: strings.Repeat("a", 64), wantStatus: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request.Header.Set("Authorization", "Bearer secret")
			if test.scope != "" {
				request.Header.Set("X-Voie-Conversation-Scope", test.scope)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}

func TestRequiredConversationScopeStillRequiresBearerToken(t *testing.T) {
	service, _ := httpTestService(t)
	handler := NewHandler(service, &config.Config{
		Host: "127.0.0.1", Port: "8080", Timeout: time.Minute,
		RequireConversationScope: true,
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("X-Voie-Conversation-Scope", strings.Repeat("a", 64))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
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

func TestCORSAllowsConversationDeletionFromSameOrigin(t *testing.T) {
	service, _ := httpTestService(t)
	handler := NewHandler(service, &config.Config{Host: "127.0.0.1", Port: "8080", Timeout: time.Minute})
	request := httptest.NewRequest(http.MethodOptions, "http://127.0.0.1:8080/v1/conversations/id", nil)
	request.Header.Set("Origin", "http://127.0.0.1:8080")
	request.Header.Set("Access-Control-Request-Method", "DELETE")
	request.Header.Set("Access-Control-Request-Headers", "authorization,content-type,x-voie-conversation-scope")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", response.Code)
	}
	if got := response.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "DELETE") {
		t.Fatalf("allow methods = %q, want DELETE", got)
	}
	if got := strings.ToLower(response.Header().Get("Access-Control-Allow-Headers")); !strings.Contains(got, "x-voie-conversation-scope") {
		t.Fatalf("allow headers = %q, want conversation scope", got)
	}
}

func TestScopeHeaderWithoutTokenIsValidatedAndApplied(t *testing.T) {
	var seen string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = app.ConversationScope(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	handler := bearerAuth("", false, next)

	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("X-Voie-Conversation-Scope", "bad")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid scope status = %d, want 400", response.Code)
	}

	for _, scope := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64)} {
		request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		request.Header.Set("X-Voie-Conversation-Scope", scope)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || seen != scope {
			t.Fatalf("scope %q: status %d, applied %q", scope[:4], response.Code, seen[:min(4, len(seen))])
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestBearerSchemeIsCaseInsensitiveAndTokenTrimmed(t *testing.T) {
	service, _ := httpTestService(t)
	handler := NewHandler(service, &config.Config{Host: "127.0.0.1", Port: "8080", Timeout: time.Minute, APIToken: "secret"})
	for _, value := range []string{"bearer secret", "Bearer  secret"} {
		request := httptest.NewRequest(http.MethodGet, "/health", nil)
		request.Header.Set("Authorization", value)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("Authorization %q status = %d, want 200", value, response.Code)
		}
	}
}
