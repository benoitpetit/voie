package providers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCohereClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "cohere-internal-body"
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, secret, tc.status)
			}))
			defer srv.Close()
			useServer(t, srv)

			_, err := (&CohereCommand{}).sendMessage(testProviderContext(),
				&cohereConversation{conversationID: "conv-1", cookies: map[string]string{}},
				"hello", "command-a-03-2025")
			classifyFailure(t, err, tc.category, tc.status, secret)
		})
	}
}
