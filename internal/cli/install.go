package cli

import (
	"fmt"

	"github.com/benoitpetit/voie/internal/install"
	"github.com/benoitpetit/voie/providers"
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
			if spec, ok := providers.ExternalGatewayByName(args[0]); ok {
				gateway := providers.NewExternalGateway(spec)
				return fmt.Errorf("third-party gateway must be installed manually: %s", gateway.InstallCommand())
			}
			if !isGatewayProvider(args[0]) {
				return fmt.Errorf("provider %q has no installable toolchain", args[0])
			}
			path, err := install.Install(command.Context(), args[0], nil)
			if err != nil {
				if args[0] == "cline" {
					return fmt.Errorf("%w (Cline CLI requires Node.js 20-24; current Node is unsupported by the npm package. Install Node 22, then run `npm install -g cline` and `cline auth` manually)", err)
				}
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "%s toolchain installed at %s\n", args[0], path)
			return nil
		},
	}
}
