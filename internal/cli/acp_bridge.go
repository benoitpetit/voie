package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/clinebridge"
	"github.com/benoitpetit/voie/internal/install"
	"github.com/benoitpetit/voie/providers"
	"github.com/spf13/cobra"
)

// newACPBridgeCommand serves any ACP provider declared in providers.ACPProviders.
func newACPBridgeCommand() *cobra.Command {
	var providerName string
	command := &cobra.Command{
		Use:          "acp-bridge",
		Short:        "Run a local ACP to OpenAI bridge for a coding agent",
		Hidden:       true,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			spec, ok := providers.ACPProviderSpecByName(providerName)
			if !ok {
				return fmt.Errorf("unknown ACP provider %q", providerName)
			}
			binary := install.BinaryPath(spec.Binary)
			if binary == "" {
				var err error
				binary, err = exec.LookPath(spec.Binary)
				if err != nil {
					return fmt.Errorf("%s not found; run `voie install %s`: %w", spec.Binary, spec.Name, err)
				}
			}
			dir, err := os.UserConfigDir()
			if err != nil {
				return err
			}
			base := filepath.Join(dir, "voie", "gateways", spec.Name)
			scratch := filepath.Join(base, "scratch")
			if err := os.MkdirAll(scratch, 0o700); err != nil {
				return err
			}
			store, _ := config.LoadCredentials()
			key := store.Token(spec.Name)
			if key == "" {
				key = "voie-local"
			}
			return clinebridge.Run(command.Context(), clinebridge.Config{
				Listen:       fmt.Sprintf("127.0.0.1:%d", spec.Port),
				ClinePath:    binary,
				AgentArgs:    spec.AgentArgs,
				StartupDelay: 1500 * time.Millisecond,
				ScratchDir:   scratch,
				HookPath:     filepath.Join(scratch, "hooks.mjs"),
				LocalKey:     key,
				Model:        spec.Model,
			})
		},
	}
	command.Flags().StringVar(&providerName, "provider", "", "ACP provider name")
	_ = command.MarkFlagRequired("provider")
	return command
}
