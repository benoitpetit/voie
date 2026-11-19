package providers

import (
	"testing"
)

func TestCohereClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "cohere-internal-body"
			useProviderResponse(t, tc.status, secret)

			_, err := (&CohereCommand{}).sendMessage(testProviderContext(),
				&cohereConversation{conversationID: "conv-1", cookies: map[string]string{}},
				"hello", "command-a-03-2025")
			classifyFailure(t, err, tc.category, tc.status, secret)
		})
	}
}
