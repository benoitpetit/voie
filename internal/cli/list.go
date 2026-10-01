package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/spf13/cobra"
)

func newModelsCommand(getService func() (*app.Service, error)) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "models",
		Short: "List supported model IDs",
		Long:  "List supported model IDs and their owners and providers. Runtime configuration is loaded when the command runs; use --json for machine-readable output.",
		Example: `  voie models
  voie models --json`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			service, err := getService()
			if err != nil {
				return err
			}
			return runModels(command.OutOrStdout(), service, jsonOutput)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "write JSON")
	return command
}

func runModels(stdout io.Writer, service *app.Service, jsonOutput bool) error {
	models := service.ListModels()
	if jsonOutput {
		return json.NewEncoder(stdout).Encode(map[string]interface{}{"object": "list", "data": models})
	}
	table := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "MODEL\tOWNER\tPROVIDER"); err != nil {
		return err
	}
	for _, model := range models {
		provider, _ := model.Meta["provider"].(string)
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\n", model.ID, model.OwnedBy, provider); err != nil {
			return err
		}
	}
	return table.Flush()
}

func newProvidersCommand(getService func() (*app.Service, error)) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "providers",
		Short: "List providers and check HTTP reachability",
		Long:  "List provider metadata and check whether each provider URL responds over HTTP. Reachability does not test model inference or guarantee that a completion will succeed. Runtime configuration is loaded when the command runs; use --json for machine-readable output.",
		Example: `  voie providers
  voie providers --json`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			service, err := getService()
			if err != nil {
				return err
			}
			return runProviders(command.Context(), command.OutOrStdout(), service, jsonOutput)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "write JSON")
	return command
}

func runProviders(ctx context.Context, stdout io.Writer, service *app.Service, jsonOutput bool) error {
	providers, err := service.ListProviders(ctx)
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(stdout).Encode(map[string]interface{}{"object": "list", "data": providers, "count": len(providers), "timestamp": time.Now().Unix()})
	}
	table := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "PROVIDER\tLABEL\tSTATUS\tMODELS"); err != nil {
		return err
	}
	for _, provider := range providers {
		state := "offline"
		if provider.Alive {
			state = "alive"
		}
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%d\n", provider.Name, provider.Label, state, len(provider.SupportedModels)); err != nil {
			return err
		}
	}
	return table.Flush()
}
