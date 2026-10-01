package cli

import (
	"context"
	"errors"
	"io"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/internal/brand"
	"github.com/spf13/cobra"
)

type Runtime interface {
	Service() *app.Service
	Serve(ctx context.Context, host, port string) error
	MCP(ctx context.Context) error
}

type RuntimeFactory func() (Runtime, error)

// NewRootCommand builds the voie command tree. RuntimeFactory is called only
// when a command needs application services, so help never loads configuration
// or initializes providers.
func NewRootCommand(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, factory RuntimeFactory) *cobra.Command {
	var showVersion bool
	root := &cobra.Command{
		Use:   brand.Name,
		Short: "A free AI API for discovering and calling models",
		Long:  brand.Banner() + "\n\n" + brand.Name + " provides an OpenAI-compatible HTTP API, a local CLI, and an MCP server over stdio for one shared model catalogue. Commands that run application services load runtime configuration when run; help does not initialize the runtime.",
		Example: `  voie --help
	  voie version
	  voie models
  voie chat --model MODEL_ID "Summarize this text"
  cat prompt.txt | voie chat --model MODEL_ID
  voie serve --host 127.0.0.1 --port 8080
  MCP client: {"command":"/absolute/path/to/voie","args":["mcp"]}`,
		SilenceErrors: true,
		RunE: func(command *cobra.Command, _ []string) error {
			if showVersion {
				return writeVersion(command.OutOrStdout())
			}
			return command.Help()
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if ctx == nil {
		ctx = context.Background()
	}
	root.SetContext(ctx)
	root.Flags().BoolVarP(&showVersion, "version", "v", false, "print the build version")

	var runtime Runtime
	var runtimeLoaded bool
	var runtimeErr error
	getRuntime := func() (Runtime, error) {
		if runtimeLoaded {
			return runtime, runtimeErr
		}
		runtimeLoaded = true
		if factory == nil {
			runtimeErr = errors.New("application runtime factory is required")
			return nil, runtimeErr
		}
		runtime, runtimeErr = factory()
		if runtimeErr == nil && runtime == nil {
			runtimeErr = errors.New("application runtime factory returned nil")
		}
		return runtime, runtimeErr
	}
	service := func() (*app.Service, error) {
		rt, err := getRuntime()
		if err != nil {
			return nil, err
		}
		return rt.Service(), nil
	}

	root.AddCommand(newServeCommand(getRuntime))
	root.AddCommand(newChatCommand(service))
	root.AddCommand(newConversationsCommand(service))
	root.AddCommand(newModelsCommand(service))
	root.AddCommand(newProvidersCommand(service))
	root.AddCommand(newMCPCommand(getRuntime))
	root.AddCommand(newVersionCommand())
	root.AddCommand(newUpdateCommand())
	return root
}

// Execute runs voie with the supplied process arguments and streams.
func Execute(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, factory RuntimeFactory) error {
	root := NewRootCommand(ctx, stdin, stdout, stderr, factory)
	root.SetArgs(args)
	return root.Execute()
}
