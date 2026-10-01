package cli

import (
	"fmt"
	"io"

	"github.com/benoitpetit/voie/internal/brand"
	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the build version",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return writeVersion(command.OutOrStdout())
		},
	}
}

func writeVersion(output io.Writer) error {
	_, err := fmt.Fprintf(output, "%s %s\n", brand.Name, brand.Version)
	return err
}
