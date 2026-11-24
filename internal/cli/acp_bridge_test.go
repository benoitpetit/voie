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

func TestACPBridgeDoesNotInjectClineFlags(t *testing.T) {
	for _, spec := range providers.ACPProviders {
		args := acpBridgeArgs(spec)
		if len(args) < len(spec.AgentArgs) {
			t.Fatalf("%s: args = %v, want at least %v", spec.Name, args, spec.AgentArgs)
		}
		for i, want := range spec.AgentArgs {
			if args[i] != want {
				t.Errorf("%s: arg %d = %q, want %q", spec.Name, i, args[i], want)
			}
		}
		for _, forbidden := range []string{"--auto-approve", "--cwd", "--hooks-dir"} {
			for _, arg := range args {
				if arg == forbidden {
					t.Errorf("%s: unexpected Cline-only flag %q in %v", spec.Name, forbidden, args)
				}
			}
		}
	}
}
