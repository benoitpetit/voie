package providers

import (
	"context"
	"testing"
)

func TestDuckAIClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "duckai-internal-body"
			useProviderResponse(t, tc.status, secret)

			provider := &DuckAI{
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
