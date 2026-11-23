package cli

import (
	"fmt"

	"github.com/benoitpetit/voie/providers"
	"github.com/spf13/cobra"
)

func newKeysCommand() *cobra.Command {
	return &cobra.Command{
		Use:          "keys [provider]",
		Short:        "Show how to obtain a provider's credentials",
		Long:         "Prints the exact steps and URLs to create an API key or sign in for each provider supported by voie. With a provider name, prints only that provider.",
		Example:      "  voie keys\n  voie keys cloudflare-ai",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 1 {
				printKeyHelp(command, args[0])
				return nil
			}
			for _, name := range providers.KeyHelpProviders() {
				printKeyHelp(command, name)
				fmt.Fprintln(command.OutOrStdout())
			}
			return nil
		},
	}
}
