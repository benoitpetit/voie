package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newChatCommand(getService func() (*app.Service, error)) *cobra.Command {
	var model, provider, task, conversation string
	var strategy string
	var models []string
	var fallbackEnabled, fallbackDisabled bool
	var fallbackRetries, fallbackLimit int
	var fallbackModels []string
	taskCategories := app.SupportedTaskCategories()
	command := &cobra.Command{
		Use:   "chat [prompt...]",
		Short: "Generate a completion from a prompt",
		Long:  fmt.Sprintf("Send a prompt using classic model selection or opt-in automatic/ensemble routing. Classic requests require --model; auto and ensemble can select models without it. --task is an optional hint for auto and ensemble; supported values: %s. Use --provider to constrain the provider and --conversation to resume local history. Fallback behavior for this request can be tuned with --fallback, --no-fallback, --fallback-retries, --fallback-limit, and repeatable --fallback-model. If no prompt argument is provided, chat reads it from stdin. Runtime configuration is loaded when chat runs; successful output contains only answer text on stdout.", strings.Join(taskCategories, ", ")),
		Example: `  voie chat --model MODEL_ID "Summarize this text"
  cat prompt.txt | voie chat --model MODEL_ID
  voie chat --model MODEL_ID --provider PROVIDER "Hello"
  voie chat --strategy auto --task coding "Review this code"
  voie chat --strategy ensemble --models MODEL_A,MODEL_B "Compare these approaches"`,
		Args: cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			effectiveStrategy := strategy
			if effectiveStrategy == "" {
				effectiveStrategy = "classic"
			}
			if strings.TrimSpace(model) == "" && (strategy == "" || strategy == "classic") {
				return fmt.Errorf("chat requires --model MODEL")
			}
			if strategy != "" && strategy != "classic" && strategy != "auto" && strategy != "ensemble" {
				return fmt.Errorf("strategy must be classic, auto, or ensemble")
			}
			if effectiveStrategy != "ensemble" && len(models) > 0 {
				return fmt.Errorf("--models requires --strategy ensemble")
			}
			if effectiveStrategy == "classic" && task != "" {
				return fmt.Errorf("--task requires auto or ensemble")
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
			return runChatWithReporter(command.Context(), args, command.InOrStdin(), command.OutOrStdout(), stderr, isTerminalWriter(stderr), service, model, provider, app.Strategy(strategy), task, models, fallback, conversation)
		},
	}
	command.Flags().StringVarP(&model, "model", "m", "", "model ID to use (required)")
	command.Flags().StringVarP(&provider, "provider", "p", "", "provider to use")
	command.Flags().StringVar(&strategy, "strategy", "", "completion strategy: classic, auto, or ensemble")
	command.Flags().StringVar(&task, "task", "", fmt.Sprintf("task hint for auto or ensemble; supported: %s", strings.Join(taskCategories, ", ")))
	if err := command.RegisterFlagCompletionFunc("task", cobra.FixedCompletions(taskCategories, cobra.ShellCompDirectiveNoFileComp)); err != nil {
		panic(err)
	}
	command.Flags().StringSliceVar(&models, "models", nil, "models to include in an ensemble")
	command.Flags().BoolVar(&fallbackEnabled, "fallback", false, "enable fallback retries and candidates for this request")
	command.Flags().BoolVar(&fallbackDisabled, "no-fallback", false, "disable fallback retries and candidates for this request")
	command.Flags().IntVar(&fallbackRetries, "fallback-retries", 0, "same-model retries for transient provider failures (0-3)")
	command.Flags().IntVar(&fallbackLimit, "fallback-limit", 0, "maximum models tried after the primary (0-3)")
	command.Flags().StringSliceVar(&fallbackModels, "fallback-model", nil, "fallback model IDs to try after the primary (repeatable, replaces the configured list)")
	command.Flags().StringVar(&conversation, "conversation", "", "local conversation ID to resume")
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
	if changed {
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

func runChat(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, service *app.Service, model, provider string) error {
	return runChatWithOptions(ctx, args, stdin, stdout, service, model, provider, app.StrategyClassic, "", nil, nil, "")
}

func runChatWithOptions(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, service *app.Service, model, provider string, strategy app.Strategy, task string, models []string, fallback *app.FallbackOverride, conversation string) error {
	return runChatWithReporter(ctx, args, stdin, stdout, io.Discard, false, service, model, provider, strategy, task, models, fallback, conversation)
}

func runChatWithReporter(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, isTerminal bool, service *app.Service, model, provider string, strategy app.Strategy, task string, models []string, fallback *app.FallbackOverride, conversation string) error {
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
	reporter := newProgressReporter(stderr, isTerminal)
	response, err := service.Complete(ctx, app.CompletionRequest{Model: model, Provider: provider, Strategy: strategy, Task: task, Models: models, Fallback: fallback, ConversationID: conversation, Messages: []app.Message{{Role: "user", Content: prompt}}, OnProgress: reporter.Handle})
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
