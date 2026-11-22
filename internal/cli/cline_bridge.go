package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/clinebridge"
	"github.com/benoitpetit/voie/internal/install"
	"github.com/spf13/cobra"
)

func newClineBridgeCommand() *cobra.Command {
	var listen, model string
	command := &cobra.Command{
		Use:          "cline-bridge",
		Short:        "Run the local Cline ACP to OpenAI bridge",
		Hidden:       true,
		Long:         "Starts a loopback OpenAI-compatible endpoint that drives `cline --acp`. Tool calls are denied so completions cannot modify the workspace. Normally started automatically by the Cline provider.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			clinePath := install.BinaryPath("cline")
			if clinePath == "" {
				var err error
				clinePath, err = exec.LookPath("cline")
				if err != nil {
					return fmt.Errorf("cline executable not found; run `voie install cline`: %w", err)
				}
			}
			dir, err := os.UserConfigDir()
			if err != nil {
				return err
			}
			base := filepath.Join(dir, "voie", "gateways", "cline")
			scratch := filepath.Join(base, "scratch")
			if err := os.MkdirAll(scratch, 0o700); err != nil {
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
			args := []string{"--acp", "--auto-approve", "false", "--cwd", scratch, "--hooks-dir", scratch}
			if model != "" && model != "auto" {
				args = append(args, "--model", model)
			}
			return clinebridge.Run(command.Context(), clinebridge.Config{
				Listen:     listen,
				ClinePath:  clinePath,
				AgentArgs:  args,
				ScratchDir: scratch,
				HookPath:   filepath.Join(scratch, "hooks.mjs"),
				LocalKey:   key,
				Model:      model,
			})
		},
	}
	command.Flags().StringVar(&listen, "listen", "127.0.0.1:8092", "bridge listen address")
	command.Flags().StringVar(&model, "model", "", "default Cline free model")
	return command
}
