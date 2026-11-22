package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/clinebridge"
	"github.com/spf13/cobra"
)

func newClineBridgeCommand() *cobra.Command {
	var listen, model string
	command := &cobra.Command{
		Use:          "cline-bridge",
		Short:        "Run the local Cline ACP to OpenAI bridge",
		Long:         "Starts a loopback OpenAI-compatible endpoint that drives `cline --acp`. Tool calls are denied so completions cannot modify the workspace. Normally started automatically by the Cline provider.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			clinePath, err := exec.LookPath("cline")
			if err != nil {
				return fmt.Errorf("cline executable not found: %w", err)
			}
			dir, err := os.UserConfigDir()
			if err != nil {
				return err
			}
			base := filepath.Join(dir, "voie", "gateways", "cline")
			if err := os.MkdirAll(base, 0o700); err != nil {
				return err
			}
			store, _ := config.LoadCredentials()
			key := store.Token("cline")
			if key == "" {
				key = "voie-local"
			}
			if model == "" {
				model = "cline-free/nemotron-3.5-super-vl"
			}
			return clinebridge.Run(command.Context(), clinebridge.Config{
				Listen:     listen,
				ClinePath:  clinePath,
				ScratchDir: filepath.Join(base, "scratch"),
				HookPath:   filepath.Join(base, "hooks.json"),
				LocalKey:   key,
				Model:      model,
			})
		},
	}
	command.Flags().StringVar(&listen, "listen", "127.0.0.1:8092", "bridge listen address")
	command.Flags().StringVar(&model, "model", "", "default Cline free model")
	return command
}
