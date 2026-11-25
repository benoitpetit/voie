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
	var jsonOutput, freeOnly, paidOnly, all bool
	var provider string
	command := &cobra.Command{
		Use:   "models",
		Short: "List supported model IDs",
		Long:  "List callable model IDs with their provider, tier and upstream price. Use --free, --paid or --provider to narrow the catalogue; --json writes machine-readable output.",
		Example: `  voie models
  voie models --free
  voie models --paid --provider openrouter
  voie models --json`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if freeOnly && paidOnly || all && (freeOnly || paidOnly) {
				return fmt.Errorf("use only one of --free, --paid, or --all")
			}
			tier := app.ModelTier("")
			if freeOnly {
				tier = app.TierFree
			}
			if paidOnly {
				tier = app.TierPaid
			}
			service, err := getService()
			if err != nil {
				return err
			}
			if provider != "" && !service.HasProvider(provider) {
				return fmt.Errorf("unknown provider %q", provider)
			}
			return runModels(command.OutOrStdout(), service, jsonOutput, provider, tier)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "write JSON")
	command.Flags().BoolVar(&freeOnly, "free", false, "show only verified free models")
	command.Flags().BoolVar(&paidOnly, "paid", false, "show only paid models")
	command.Flags().BoolVar(&all, "all", false, "show all models (default)")
	command.Flags().StringVar(&provider, "provider", "", "show only models from this provider")
	return command
}

func runModels(stdout io.Writer, service *app.Service, jsonOutput bool, provider string, tier app.ModelTier) error {
	models := service.ListModelsFiltered(provider, tier)
	if jsonOutput {
		return json.NewEncoder(stdout).Encode(map[string]interface{}{"object": "list", "data": models})
	}
	table := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "PROVIDER\tMODEL\tTIER\tPRICE"); err != nil {
		return err
	}
	for _, model := range models {
		name, _ := model.Meta["provider"].(string)
		price := "-"
		if model.Price != nil && model.Price.PromptPerToken != "" {
			price = model.Price.Currency + " " + model.Price.PromptPerToken + "/" + model.Price.CompletionPerToken
		} else if model.Tier == app.TierPaid {
			price = "price unknown"
		}
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", name, model.ID, model.Tier, price); err != nil {
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
		if provider.Ready {
			state = "ready"
		} else if provider.Alive {
			state = "alive"
		}
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%d\n", provider.Name, provider.Label, state, len(provider.SupportedModels)); err != nil {
			return err
		}
	}
	return table.Flush()
}
