package providers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

func TestJimmyClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "jimmy-internal-body"
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, secret, tc.status)
			}))
			defer srv.Close()

			_, err := (&Jimmy{endpoint: srv.URL}).ChatCompletion(testProviderContext(),
				[]Message{{Role: "user", Content: "hello"}}, "llama3.1-8B")
			classifyFailure(t, err, tc.category, tc.status, secret)
		})
	}
}

func TestJimmyNetworkFailureIsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	_, err := (&Jimmy{endpoint: srv.URL}).ChatCompletion(testProviderContext(),
		[]Message{{Role: "user", Content: "hello"}}, "llama3.1-8B")
	classifyFailure(t, err, app.FailureTransient, 0, "")
}
