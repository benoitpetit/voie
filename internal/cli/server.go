package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newServeCommand(getRuntime func() (Runtime, error)) *cobra.Command {
	var host, port string
	command := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP API server",
		Long:  "Start the HTTP API server using HOST and PORT from the runtime configuration. The --host and --port flags override those values. A non-loopback bind requires API_TOKEN.",
		Example: `  voie serve
  voie serve --host 127.0.0.1 --port 8080`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			runtime, err := getRuntime()
			if err != nil {
				return err
			}
			return runtime.Serve(command.Context(), host, port)
		},
	}
	command.Flags().StringVar(&host, "host", "", "HTTP bind host (defaults to HOST configuration)")
	command.Flags().StringVar(&port, "port", "", "HTTP bind port (defaults to PORT configuration)")
	return command
}

func newMCPCommand(getRuntime func() (Runtime, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server over stdio",
		Long:  "Run the MCP server over stdio. Runtime configuration is loaded when the command starts. Standard output is reserved for MCP protocol messages; startup diagnostics go to standard error.",
		Example: `  voie mcp

Configure your MCP client with command "/absolute/path/to/voie" and args ["mcp"].`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			runtime, err := getRuntime()
			if err != nil {
				return err
			}
			if err := runtime.MCP(command.Context()); err != nil {
				return fmt.Errorf("run MCP server: %w", err)
			}
			return nil
		},
	}
}
