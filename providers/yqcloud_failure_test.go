package providers

import (
	"testing"
)

func TestYqcloudClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "yqcloud-internal-body"
			useProviderResponse(t, tc.status, secret)

			err := (&Yqcloud{}).ChatCompletionStream(testProviderContext(),
				[]Message{{Role: "user", Content: "hello"}}, "gpt-4", func(string) {})
			classifyFailure(t, err, tc.category, tc.status, secret)
		})
	}
}
