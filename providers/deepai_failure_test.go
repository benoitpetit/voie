package providers

import (
	"testing"
)

func TestDeepAIClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "deepai-internal-body"
			useProviderResponse(t, tc.status, secret)

			err := (&DeepAI{}).ChatCompletionStream(testProviderContext(),
				[]Message{{Role: "user", Content: "hello"}}, "standard", func(string) {})
			classifyFailure(t, err, tc.category, tc.status, secret)
		})
	}
}
