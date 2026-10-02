package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDuckAIClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "duckai-internal-body"
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, secret, tc.status)
			}))
			defer srv.Close()

			provider := &DuckAI{
				endpoint: srv.URL,
				captureHeaders: func(context.Context) (duckAIHeaders, error) {
					return duckAIHeaders{VQDHash1: "proof", FESignals: "signals", FEVersion: "version", UserAgent: "browser"}, nil
				},
			}
			err := provider.ChatCompletionStream(testProviderContext(),
				[]Message{{Role: "user", Content: "hello"}}, "gpt-5.6-luna", func(string) {})
			classifyFailure(t, err, tc.category, tc.status, secret)
		})
	}
}
