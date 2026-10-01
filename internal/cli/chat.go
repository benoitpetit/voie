package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/spf13/cobra"
)

func newChatCommand(getService func() (*app.Service, error)) *cobra.Command {
	var model, provider, task, conversation string
	var strategy string
	var models []string
	command := &cobra.Command{
		Use:   "chat [prompt...]",
		Short: "Generate a completion from a prompt",
		Long:  "Send a prompt using classic model selection or opt-in automatic/ensemble routing. Classic requests require --model; auto and ensemble can select models without it. Use --provider to constrain the provider and --conversation to resume local history. If no prompt argument is provided, chat reads it from stdin. Runtime configuration is loaded when chat runs; successful output contains only answer text on stdout.",
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
			service, err := getService()
			if err != nil {
				return err
			}
			stderr := command.ErrOrStderr()
			return runChatWithReporter(command.Context(), args, command.InOrStdin(), command.OutOrStdout(), stderr, isTerminalWriter(stderr), service, model, provider, app.Strategy(strategy), task, models, conversation)
		},
	}
	command.Flags().StringVarP(&model, "model", "m", "", "model ID to use (required)")
	command.Flags().StringVarP(&provider, "provider", "p", "", "provider to use")
	command.Flags().StringVar(&strategy, "strategy", "", "completion strategy: classic, auto, or ensemble")
	command.Flags().StringVar(&task, "task", "", "task category for automatic routing")
	command.Flags().StringSliceVar(&models, "models", nil, "models to include in an ensemble")
	command.Flags().StringVar(&conversation, "conversation", "", "local conversation ID to resume")
	return command
}

func runChat(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, service *app.Service, model, provider string) error {
	return runChatWithOptions(ctx, args, stdin, stdout, service, model, provider, app.StrategyClassic, "", nil, "")
}

func runChatWithOptions(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, service *app.Service, model, provider string, strategy app.Strategy, task string, models []string, conversation string) error {
	return runChatWithReporter(ctx, args, stdin, stdout, io.Discard, false, service, model, provider, strategy, task, models, conversation)
}

func runChatWithReporter(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, isTerminal bool, service *app.Service, model, provider string, strategy app.Strategy, task string, models []string, conversation string) error {
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
	response, err := service.Complete(ctx, app.CompletionRequest{Model: model, Provider: provider, Strategy: strategy, Task: task, Models: models, ConversationID: conversation, Messages: []app.Message{{Role: "user", Content: prompt}}, OnProgress: reporter.Handle})
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
