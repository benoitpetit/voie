package providers

import (
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

func TestPerplexityClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "perplexity-internal-body"
			useProviderResponse(t, tc.status, secret)

			err := (&Perplexity{}).ChatCompletionStream(testProviderContext(),
				[]Message{{Role: "user", Content: "hello"}}, "turbo", func(string) {})
			classifyFailure(t, err, tc.category, tc.status, secret)
		})
	}
}

func TestPerplexityNetworkFailureIsTransient(t *testing.T) {
	useProviderNetworkFailure(t)

	err := (&Perplexity{}).ChatCompletionStream(testProviderContext(),
		[]Message{{Role: "user", Content: "hello"}}, "turbo", func(string) {})
	classifyFailure(t, err, app.FailureTransient, 0, "")
}
