package providers

import "testing"

func TestObsoletePerplexityProviderIsNotRegistered(t *testing.T) {
	if NewRegistry().Get("perplexity") != nil {
		t.Fatal("obsolete anonymous perplexity provider is still registered")
	}
}
