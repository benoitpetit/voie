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
