package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"

	"github.com/benoitpetit/voie/internal/app"
)

func bearerAuth(token string, requireConversationScope bool, next http.Handler) http.Handler {
	return bearerAuthExcept(token, requireConversationScope, next)
}

// bearerAuthExcept protects every path except the listed public ones. The A2A
// Agent Card must be discoverable before a client has a token.
func bearerAuthExcept(token string, requireConversationScope bool, next http.Handler, publicPaths ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, path := range publicPaths {
			if r.URL.Path == path {
				next.ServeHTTP(w, r)
				return
			}
		}
		if requireConversationScope && strings.TrimSpace(token) == "" {
			writeError(w, http.StatusUnauthorized, "valid bearer token required")
			return
		}
		provided := ""
		if value := r.Header.Get("Authorization"); len(value) >= 7 && strings.EqualFold(value[:7], "Bearer ") {
			provided = strings.TrimSpace(value[7:])
		}
		if strings.TrimSpace(token) != "" {
			expectedHash := sha256.Sum256([]byte(token))
			providedHash := sha256.Sum256([]byte(provided))
			if subtle.ConstantTimeCompare(expectedHash[:], providedHash[:]) != 1 {
				writeError(w, http.StatusUnauthorized, "valid bearer token required")
				return
			}
		}
		scope := r.Header.Get("X-Voie-Conversation-Scope")
		if scope != "" && !validConversationScope(scope) {
			writeError(w, http.StatusBadRequest, "invalid conversation scope")
			return
		}
		if requireConversationScope && strings.HasPrefix(r.URL.Path, "/v1/") && scope == "" {
			writeError(w, http.StatusUnauthorized, "conversation scope required")
			return
		}
		next.ServeHTTP(w, r.WithContext(app.WithConversationScope(r.Context(), scope)))
	})
}

func validConversationScope(scope string) bool {
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
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, X-Voie-Conversation-Scope")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}
		if r.Method == http.MethodOptions {
			requested := strings.ToUpper(strings.TrimSpace(r.Header.Get("Access-Control-Request-Method")))
			allowed := requested == "GET" || requested == "POST" || requested == "DELETE" || requested == "OPTIONS"
			if origin == "" || !allowed {
				next.ServeHTTP(w, r)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
