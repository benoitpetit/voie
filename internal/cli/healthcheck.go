package cli

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newHealthcheckCommand(client *http.Client) *cobra.Command {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	return &cobra.Command{
		Use:          "healthcheck",
		Short:        "Check the local HTTP service health",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			port := strings.TrimSpace(os.Getenv("PORT"))
			if port == "" {
				port = "8080"
			}
			request, err := http.NewRequestWithContext(command.Context(), http.MethodGet, "http://"+net.JoinHostPort("127.0.0.1", port)+"/health", nil)
			if err != nil {
				return fmt.Errorf("build health check request: %w", err)
			}
			if token := strings.TrimSpace(os.Getenv("API_TOKEN")); token != "" {
				request.Header.Set("Authorization", "Bearer "+token)
			}
			response, err := client.Do(request)
			if err != nil {
				return fmt.Errorf("health check request failed: %w", err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				return fmt.Errorf("health check returned %s", response.Status)
			}
			return nil
		},
	}
}
