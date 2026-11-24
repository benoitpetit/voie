package cli

import (
	"testing"

	"github.com/benoitpetit/voie/providers"
)

func TestACPBridgeConfigUsesSpecFeedURL(t *testing.T) {
	for _, spec := range providers.ACPProviders {
		cfg := acpBridgeConfig(spec, spec.Binary, "/tmp/scratch", "voie-local")
		if cfg.FreeFeedURL != spec.FeedURL {
			t.Errorf("%s: FreeFeedURL = %q, want %q", spec.Name, cfg.FreeFeedURL, spec.FeedURL)
		}
		if cfg.Model != spec.Model {
			t.Errorf("%s: Model = %q, want %q", spec.Name, cfg.Model, spec.Model)
		}
	}
}
