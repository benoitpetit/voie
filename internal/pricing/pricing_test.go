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

func TestAllReturnsPublicCatalogue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"openai/gpt-5","pricing":{"prompt":"0.000002","completion":"0.000006"}}]}`))
	}))
	defer server.Close()
	SetSource(server.URL)
	t.Cleanup(func() { SetSource("") })
	all := All()
	price, ok := all["openai/gpt-5"]
	if !ok || price.Prompt != "0.000002" {
		t.Fatalf("All() = %v", all)
	}
}

func TestAuthenticatedSourceAndLookup(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor/paid","pricing":{"prompt":"0.00001","completion":"0.00005"}}]}`))
	}))
	defer server.Close()
	RegisterSource("orcarouter", server.URL, func() string { return "tok-orca" })
	t.Cleanup(func() { RegisterSource("orcarouter", "", nil) })
	price, ok := Lookup("orcarouter", "vendor/paid")
	if !ok || price.Prompt != "0.00001" {
		t.Fatalf("price = %+v, %v", price, ok)
	}
	if gotAuth != "Bearer tok-orca" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if len(AllFor("orcarouter")) != 1 {
		t.Fatal("AllFor did not return the authenticated catalogue")
	}
}
