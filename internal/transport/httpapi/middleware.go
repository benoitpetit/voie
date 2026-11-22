package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"

	"github.com/benoitpetit/voie/internal/app"
)

func bearerAuth(token string, requireProjectScope bool, next http.Handler) http.Handler {
	if strings.TrimSpace(token) == "" {
		if requireProjectScope {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeError(w, http.StatusUnauthorized, "valid bearer token required")
			})
		}
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := ""
		if value := r.Header.Get("Authorization"); strings.HasPrefix(value, "Bearer ") {
			provided = strings.TrimPrefix(value, "Bearer ")
		}
		expectedHash := sha256.Sum256([]byte(token))
		providedHash := sha256.Sum256([]byte(provided))
		if subtle.ConstantTimeCompare(expectedHash[:], providedHash[:]) != 1 {
			writeError(w, http.StatusUnauthorized, "valid bearer token required")
			return
		}
		scope := r.Header.Get("X-Voie-Project-ID")
		if scope != "" && !validProjectScope(scope) {
			writeError(w, http.StatusBadRequest, "invalid project scope")
			return
		}
		if requireProjectScope && strings.HasPrefix(r.URL.Path, "/v1/") && scope == "" {
			writeError(w, http.StatusUnauthorized, "project scope required")
			return
		}
		next.ServeHTTP(w, r.WithContext(app.WithConversationScope(r.Context(), scope)))
	})
}

func validProjectScope(scope string) bool {
	if len(scope) != 64 {
		return false
	}
	for _, character := range scope {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || !strings.EqualFold(parsed.Host, r.Host) {
				writeError(w, http.StatusForbidden, "cross-origin requests are not allowed")
				return
			}
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, X-Voie-Project-ID")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
