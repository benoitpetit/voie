package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newChatCommand(getService func() (*app.Service, error)) *cobra.Command {
	var model, provider, task, conversation, ensembleMode string
	var strategy string
	var models, ensembleModels []string
	var candidateCount, chainAgents, debateTurns int
	var fallbackEnabled, fallbackDisabled bool
	var fallbackRetries, fallbackLimit int
	var fallbackModels []string
	var stream bool
	command := &cobra.Command{
		Use:   "chat [prompt...]",
		Short: "Generate a completion from a prompt",
		Long:  "Send a prompt using classic model selection or automatic/ensemble routing. Classic requests require --model; auto and ensemble select models from a configured routing profile. --task names that profile; built-in task hints are coding, reasoning, writing, translation, summarization, and general, and custom profile names are supported. Ensemble algorithms are independent, chain, or debate; --candidate-count selects 2-5 agents, repeatable --ensemble-model selects exact agent models, and --debate-turns sets 0-5 debate review rounds. Use --provider to constrain candidates and --conversation to resume local history. Fallback behavior can be tuned with --fallback, --no-fallback, --fallback-retries, --fallback-limit, and repeatable --fallback-model. If no prompt argument is provided, chat reads it from stdin. Runtime configuration is loaded when chat runs; successful output contains only answer text on stdout.",
		Example: `  voie chat --model MODEL_ID "Summarize this text"
  cat prompt.txt | voie chat --model MODEL_ID
  voie chat --model MODEL_ID --provider PROVIDER "Hello"
  voie chat --strategy auto --task coding "Review this code"
  voie chat --strategy ensemble --candidate-count 3 --ensemble-model MODEL_A --ensemble-model MODEL_B --ensemble-model MODEL_C "Compare these approaches"`,
		Args: cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			effectiveStrategy := strategy
			if effectiveStrategy == "" {
				effectiveStrategy = "classic"
			}
			if strategy != "" && strategy != "classic" && strategy != "auto" && strategy != "ensemble" {
				return fmt.Errorf("strategy must be classic, auto, or ensemble")
			}
			if strings.TrimSpace(model) == "" && (strategy == "" || strategy == "classic") {
				return fmt.Errorf("chat requires --model MODEL")
			}
			if effectiveStrategy != "classic" && strings.TrimSpace(model) != "" {
				return fmt.Errorf("--model is only valid with --strategy classic")
			}
			if effectiveStrategy == "ensemble" && (len(models) == 1 || (len(models) == 0 && len(ensembleModels) == 1)) {
				return fmt.Errorf("ensemble requires at least two models")
			}
			if effectiveStrategy != "ensemble" && (len(models) > 0 || len(ensembleModels) > 0) {
				return fmt.Errorf("--models and --ensemble-model require --strategy ensemble")
			}
			if len(models) > 0 && len(ensembleModels) > 0 {
				return fmt.Errorf("use either legacy --models or repeatable --ensemble-model, not both")
			}
			if effectiveStrategy == "classic" && task != "" {
				return fmt.Errorf("--task requires auto or ensemble")
			}
			ensembleFlagsSet := command.Flags().Changed("ensemble-mode") || command.Flags().Changed("candidate-count") || command.Flags().Changed("ensemble-model") || command.Flags().Changed("chain-agents") || command.Flags().Changed("debate-turns")
			if effectiveStrategy != "ensemble" && ensembleFlagsSet {
				return fmt.Errorf("--ensemble-mode, --candidate-count, --ensemble-model, --chain-agents, and --debate-turns require --strategy ensemble")
			}
			var ensemble *app.EnsembleRequestOptions
			if ensembleFlagsSet {
				ensemble = &app.EnsembleRequestOptions{}
				if command.Flags().Changed("ensemble-mode") {
					ensemble.Mode = app.EnsembleMode(ensembleMode)
				}
				if command.Flags().Changed("candidate-count") {
					ensemble.CandidateCount = &candidateCount
				}
				if command.Flags().Changed("ensemble-model") {
					ensemble.Models = ensembleModels
				}
				if command.Flags().Changed("chain-agents") {
					ensemble.ChainAgents = &chainAgents
				}
				if command.Flags().Changed("debate-turns") {
					ensemble.DebateTurns = &debateTurns
				}
			}
			fallback, err := buildFallbackOverride(command.Flags(), fallbackEnabled, fallbackDisabled, fallbackRetries, fallbackLimit, fallbackModels)
			if err != nil {
				return err
			}
			service, err := getService()
			if err != nil {
				return err
			}
			stderr := command.ErrOrStderr()
			return runChatWithReporter(command.Context(), args, command.InOrStdin(), command.OutOrStdout(), stderr, isTerminalWriter(stderr), service, model, provider, app.Strategy(strategy), task, models, ensemble, fallback, conversation, stream)
		},
	}
	command.Flags().StringVarP(&model, "model", "m", "", "model ID to use (required for classic)")
	command.Flags().StringVarP(&provider, "provider", "p", "", "provider to use")
	command.Flags().StringVar(&strategy, "strategy", "", "completion strategy: classic (explicit model), auto (routing profile), or ensemble (multi-agent)")
	command.Flags().StringVar(&task, "task", "", "routing profile for auto or automatically selected ensemble candidates")
	if err := command.RegisterFlagCompletionFunc("task", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		if service, err := getService(); err == nil {
			return service.RoutingProfiles(), cobra.ShellCompDirectiveNoFileComp
		}
		return app.SupportedTaskCategories(), cobra.ShellCompDirectiveNoFileComp
	}); err != nil {
		panic(err)
	}
	command.Flags().StringSliceVar(&models, "models", nil, "legacy exact ensemble model list; prefer repeatable --ensemble-model")
	command.Flags().StringSliceVar(&ensembleModels, "ensemble-model", nil, "exact agent model ID for an ensemble (repeatable, at least two)")
	command.Flags().IntVar(&candidateCount, "candidate-count", 0, "number of ensemble agents to select automatically (2-5)")
	command.Flags().StringVar(&ensembleMode, "ensemble-mode", "", "ensemble algorithm: independent, chain, or debate")
	if err := command.RegisterFlagCompletionFunc("ensemble-mode", cobra.FixedCompletions([]string{"independent", "chain", "debate"}, cobra.ShellCompDirectiveNoFileComp)); err != nil {
		panic(err)
	}
	command.Flags().IntVar(&chainAgents, "chain-agents", 0, "number of sequential reviewers in chain mode (2-5)")
	command.Flags().IntVar(&debateTurns, "debate-turns", 0, "number of mutual review rounds in debate mode (0-5)")
	command.Flags().BoolVar(&fallbackEnabled, "fallback", false, "enable fallback retries and candidates for this request")
	command.Flags().BoolVar(&fallbackDisabled, "no-fallback", false, "disable fallback retries and candidates for this request")
	command.Flags().IntVar(&fallbackRetries, "fallback-retries", 0, "same-model retries for transient provider failures (0-3)")
	command.Flags().IntVar(&fallbackLimit, "fallback-limit", 0, "maximum models tried after the primary (0-3)")
	command.Flags().StringSliceVar(&fallbackModels, "fallback-model", nil, "fallback model IDs to try after the primary (repeatable, replaces the configured list)")
	command.Flags().StringVar(&conversation, "conversation", "", "local conversation ID to resume")
	command.Flags().BoolVar(&stream, "stream", false, "stream model chunks to stdout as they are produced")
	configureChatHelp(command, &strategy)
	return command
}

// buildFallbackOverride maps the chat fallback flags to a request override. It
// returns nil when no fallback flag is present so the request inherits the
// global policy untouched, and validates the retry and limit bounds (0-3) plus
// the --fallback/--no-fallback conflict before the runtime is loaded.
func buildFallbackOverride(flags *pflag.FlagSet, enabled, disabled bool, retries, limit int, models []string) (*app.FallbackOverride, error) {
	changed := flags.Changed("fallback")
	disabledChanged := flags.Changed("no-fallback")
	retriesChanged := flags.Changed("fallback-retries")
	limitChanged := flags.Changed("fallback-limit")
	modelsChanged := flags.Changed("fallback-model")
	if !changed && !disabledChanged && !retriesChanged && !limitChanged && !modelsChanged {
		return nil, nil
	}
	if changed && disabledChanged {
		return nil, fmt.Errorf("--fallback and --no-fallback cannot be used together")
	}
	if retriesChanged && (retries < 0 || retries > 3) {
		return nil, fmt.Errorf("--fallback-retries must be between 0 and 3, got %d", retries)
	}
	if limitChanged && (limit < 0 || limit > 3) {
		return nil, fmt.Errorf("--fallback-limit must be between 0 and 3, got %d", limit)
	}
	override := &app.FallbackOverride{}
	if changed || (!disabledChanged && (retriesChanged || limitChanged || modelsChanged)) {
		enabled := true
		override.Enabled = &enabled
	}
	if disabledChanged {
		disabled := false
		override.Enabled = &disabled
	}
	if retriesChanged {
		override.MaxRetries = &retries
	}
	if limitChanged {
		override.MaxFallbackModels = &limit
	}
	if modelsChanged {
		override.Models = models
	}
	return override, nil
}

func runChatWithReporter(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, isTerminal bool, service *app.Service, model, provider string, strategy app.Strategy, task string, models []string, ensemble *app.EnsembleRequestOptions, fallback *app.FallbackOverride, conversation string, stream bool) error {
	prompt := strings.TrimSpace(strings.Join(args, " "))
	if prompt == "" {
		if stdin == nil {
			return fmt.Errorf("chat needs a prompt argument or input on stdin")
		}
		input, err := io.ReadAll(stdin)
		if err != nil {
			return fmt.Errorf("read prompt from stdin: %w", err)
		}
		prompt = strings.TrimSpace(string(input))
	}
	if prompt == "" {
		return fmt.Errorf("chat prompt must not be empty")
	}
	requestID := app.RequestID(ctx)
	if requestID == "" {
		requestID = utils.GenerateID()[:8]
		ctx = app.WithRequestID(ctx, requestID)
	}
	reporter := &requestIDReporter{base: newProgressReporter(stderr, isTerminal), id: requestID}
	request := app.CompletionRequest{Model: model, Provider: provider, Strategy: strategy, Task: task, Models: models, Ensemble: ensemble, Fallback: fallback, ConversationID: conversation, Messages: []app.Message{{Role: "user", Content: prompt}}, OnProgress: reporter.Handle}
	if stream {
		var text string
		err := service.CompleteStreamWithInfo(ctx, request, func(chunk string, _ *app.RoutingInfo) {
			text += chunk
			_, _ = fmt.Fprint(stdout, chunk)
		})
		reporter.Finish(err)
		if err != nil {
			return err
		}
		if text == "" {
			return app.ErrUpstream
		}
		_, err = fmt.Fprintln(stdout)
		return err
	}
	response, err := service.Complete(ctx, request)
	reporter.Finish(err)
	if err != nil {
		return err
	}
	if len(response.Choices) == 0 {
		return app.ErrUpstream
	}
	_, err = fmt.Fprintln(stdout, response.Choices[0].Message.Content)
	return err
}

func isTerminalWriter(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
