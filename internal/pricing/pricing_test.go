package pricing

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLookupFetchesPublicPrices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"foo/bar","pricing":{"prompt":"0.000001","completion":"0.000002"}}]}`))
	}))
	defer server.Close()
	SetSource(server.URL)
	t.Cleanup(func() { SetSource("") })
	price, ok := Lookup("openrouter", "foo/bar")
	if !ok || price.Prompt != "0.000001" || price.Completion != "0.000002" {
		t.Fatalf("price = %+v, %v", price, ok)
	}
	if _, ok := Lookup("groq", "foo/bar"); ok {
		t.Fatal("price lookup must not guess for other providers")
	}
}

func TestLookupDoesNotRefetchOnEveryFailedCall(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	SetSource(server.URL)
	t.Cleanup(func() { SetSource("") })
	for i := 0; i < 16; i++ {
		if _, ok := Lookup("openrouter", "vendor/model"); ok {
			t.Fatal("price unexpectedly found")
		}
	}
	if calls > 2 {
		t.Fatalf("fetches = %d, want at most 2 on repeated failures", calls)
	}
}
