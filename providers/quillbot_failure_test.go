package providers

import (
	"strings"
	"testing"
)

func TestQuillbotClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "quillbot-internal-body"
			useProviderResponse(t, tc.status, secret)

			err := (&Quillbot{}).ChatCompletionStream(testProviderContext(),
				[]Message{{Role: "user", Content: "hello"}}, "quillbot-ai-chat", func(string) {})
			classifyFailure(t, err, tc.category, tc.status, secret)
		})
	}
}

func TestQuillbotIsNotAdvertisedWhileUpstreamIsUnavailable(t *testing.T) {
	info := (&Quillbot{}).GetInfo()
	if info.Working {
		t.Fatal("QuillBot reported working after repeated live HTTP 502 responses")
	}
	if !strings.Contains(info.Description, "HTTP 502") {
		t.Fatalf("description = %q, want the current upstream failure", info.Description)
	}
}
