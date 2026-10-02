package providers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

func TestPerplexityClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "perplexity-internal-body"
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, secret, tc.status)
			}))
			defer srv.Close()
			useServer(t, srv)

			err := (&Perplexity{}).ChatCompletionStream(testProviderContext(),
				[]Message{{Role: "user", Content: "hello"}}, "turbo", func(string) {})
			classifyFailure(t, err, tc.category, tc.status, secret)
		})
	}
}

func TestPerplexityNetworkFailureIsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	useServer(t, srv)
	srv.Close()

	err := (&Perplexity{}).ChatCompletionStream(testProviderContext(),
		[]Message{{Role: "user", Content: "hello"}}, "turbo", func(string) {})
	classifyFailure(t, err, app.FailureTransient, 0, "")
}
