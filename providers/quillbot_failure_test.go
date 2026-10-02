package providers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQuillbotClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "quillbot-internal-body"
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, secret, tc.status)
			}))
			defer srv.Close()
			useServer(t, srv)

			err := (&Quillbot{}).ChatCompletionStream(testProviderContext(),
				[]Message{{Role: "user", Content: "hello"}}, "quillbot-ai-chat", func(string) {})
			classifyFailure(t, err, tc.category, tc.status, secret)
		})
	}
}
