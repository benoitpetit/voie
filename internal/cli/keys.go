package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

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
				known := false
				for _, name := range providers.KeyHelpProviders() {
					if name == args[0] {
						known = true
					}
				}
				if !known {
					return fmt.Errorf("unknown provider %q; run `voie keys` to list them", args[0])
				}
				return runKeysForProvider(command.OutOrStdout(), args[0])
			}
			return runKeysList(command.OutOrStdout())
		},
	}
}

func runKeysForProvider(out io.Writer, name string) error {
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintf(table, "PROVIDER\tSTEP\tINSTRUCTION\n"); err != nil {
		return err
	}
	for index, step := range providers.KeyHelp(name) {
		if _, err := fmt.Fprintf(table, "%s\t%d\t%s\n", name, index+1, step); err != nil {
			return err
		}
	}
	return table.Flush()
}

func runKeysList(out io.Writer) error {
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "PROVIDER\tSTEPS\tFIRST INSTRUCTION"); err != nil {
		return err
	}
	for _, name := range providers.KeyHelpProviders() {
		steps := providers.KeyHelp(name)
		first := ""
		if len(steps) > 0 {
			first = steps[0]
		}
		if _, err := fmt.Fprintf(table, "%s\t%d\t%s\n", name, len(steps), first); err != nil {
			return err
		}
	}
	return table.Flush()
}
