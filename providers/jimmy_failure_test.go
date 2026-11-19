package providers

import (
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

func TestJimmyClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "jimmy-internal-body"
			useProviderResponse(t, tc.status, secret)

			_, err := (&Jimmy{}).ChatCompletion(testProviderContext(),
				[]Message{{Role: "user", Content: "hello"}}, "llama3.1-8B")
			classifyFailure(t, err, tc.category, tc.status, secret)
		})
	}
}

func TestJimmyNetworkFailureIsTransient(t *testing.T) {
	useProviderNetworkFailure(t)

	_, err := (&Jimmy{}).ChatCompletion(testProviderContext(),
		[]Message{{Role: "user", Content: "hello"}}, "llama3.1-8B")
	classifyFailure(t, err, app.FailureTransient, 0, "")
}
