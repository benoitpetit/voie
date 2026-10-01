package cli

import (
	"fmt"
	"os"
	runtimeinfo "runtime"
	"strings"

	"github.com/benoitpetit/voie/internal/brand"
	updater "github.com/benoitpetit/voie/internal/update"
	"github.com/spf13/cobra"
)

func newUpdateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update voie to the latest release",
		Long:  "Download the latest release for this system, verify its SHA-256 checksum, and replace the current executable.",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if strings.EqualFold(brand.Version, "dev") {
				return updater.ErrDevelopmentBuild
			}
			executable, err := os.Executable()
			if err != nil {
				return fmt.Errorf("locate current executable: %w", err)
			}
			result, err := updater.Run(command.Context(), updater.Options{
				Owner: "benoitpetit", Repository: "voie", CurrentVersion: brand.Version,
				GOOS: runtimeinfo.GOOS, GOARCH: runtimeinfo.GOARCH, ExecutablePath: executable,
			})
			if err != nil {
				return err
			}
			switch {
			case result.AlreadyCurrent:
				_, err = fmt.Fprintf(command.OutOrStdout(), "voie is already up to date (v%s)\n", result.LatestVersion)
			case result.Pending:
				_, err = fmt.Fprintf(command.OutOrStdout(), "Update to v%s is staged; replacement completes after this process exits.\n", result.LatestVersion)
			case result.Updated:
				_, err = fmt.Fprintf(command.OutOrStdout(), "Updated voie to v%s.\n", result.LatestVersion)
			}
			return err
		},
	}
}
