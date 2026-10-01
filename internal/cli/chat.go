package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/spf13/cobra"
)

func newChatCommand(getService func() (*app.Service, error)) *cobra.Command {
	var model, provider string
	command := &cobra.Command{
		Use:   "chat [prompt...]",
		Short: "Generate a completion from a prompt",
		Long:  "Send a prompt to a supported model selected by the required --model ID. Use --provider to select a provider explicitly. If no prompt argument is provided, chat reads it from stdin. Runtime configuration is loaded when chat runs; successful output contains only the answer text on stdout.",
		Example: `  voie chat --model MODEL_ID "Summarize this text"
  cat prompt.txt | voie chat --model MODEL_ID
  voie chat --model MODEL_ID --provider PROVIDER "Hello"`,
		Args: cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if strings.TrimSpace(model) == "" {
				return fmt.Errorf("chat requires --model MODEL")
			}
			service, err := getService()
			if err != nil {
				return err
			}
			return runChat(command.Context(), args, command.InOrStdin(), command.OutOrStdout(), service, model, provider)
		},
	}
	command.Flags().StringVarP(&model, "model", "m", "", "model ID to use (required)")
	command.Flags().StringVarP(&provider, "provider", "p", "", "provider to use")
	return command
}

func runChat(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, service *app.Service, model, provider string) error {
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
	response, err := service.Complete(ctx, app.CompletionRequest{Model: model, Provider: provider, Messages: []app.Message{{Role: "user", Content: prompt}}})
	if err != nil {
		return err
	}
	if len(response.Choices) == 0 {
		return app.ErrUpstream
	}
	_, err = fmt.Fprintln(stdout, response.Choices[0].Message.Content)
	return err
}
