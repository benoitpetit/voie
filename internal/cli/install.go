package cli

import (
	"fmt"

	"github.com/benoitpetit/voie/internal/install"
	"github.com/spf13/cobra"
)

func newInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:          "install <provider>",
		Short:        "Install the toolchain required by a local gateway provider",
		Long:         "Downloads the external binary or npm package needed by antigravity, kiro, or cline. `voie connect` runs this automatically the first time you connect one of them.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			if !isGatewayProvider(args[0]) {
				return fmt.Errorf("provider %q has no installable toolchain", args[0])
			}
			path, err := install.Install(command.Context(), args[0], nil)
			if err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "%s toolchain installed at %s\n", args[0], path)
			return nil
		},
	}
}
