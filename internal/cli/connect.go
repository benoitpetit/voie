package cli

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/install"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var connectableProviders = []string{"opencode-zen", "opencode-go", "openrouter", "orcarouter", "antigravity", "kiro", "cline"}

var connectInstaller = install.Ensure

func isGatewayProvider(provider string) bool {
	return provider == "antigravity" || provider == "kiro" || provider == "cline"
}

type tokenChecker func(provider, token string) error

func newConnectCommand(check tokenChecker) *cobra.Command {
	if check == nil {
		check = validateToken
	}
	var token string
	var list bool
	command := &cobra.Command{
		Use:   "connect [provider]",
		Short: "Connect a provider with its token",
		Long:  "Connect stores one token per provider in the user config directory (0600). Environment variables override saved tokens. The two opencode providers use the local gateway key, which defaults to voie-local.",
		Example: `  voie connect
  voie connect openrouter --token sk-or-v1-...
  voie connect --list
  voie connect disconnect openrouter`,
		Args: cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			store, err := config.LoadCredentials()
			if err != nil {
				return fmt.Errorf("load credentials: %w", err)
			}
			if list {
				for _, name := range connectableProviders {
					state := "not connected"
					if store.Token(name) != "" {
						state = "connected"
					} else if name == "opencode-zen" || name == "opencode-go" {
						state = "connected (default key)"
					}
					fmt.Fprintf(command.OutOrStdout(), "%-14s %s\n", name, state)
				}
				return nil
			}
			if len(args) == 1 && args[0] == "disconnect" {
				return fmt.Errorf("usage: voie connect disconnect <provider>")
			}
			if len(args) == 2 && args[0] == "disconnect" {
				if !isConnectable(args[1]) {
					return fmt.Errorf("unknown provider %q", args[1])
				}
				if err := store.Delete(args[1]); err != nil {
					return fmt.Errorf("disconnect %s: %w", args[1], err)
				}
				fmt.Fprintf(command.OutOrStdout(), "%s disconnected\n", args[1])
				return nil
			}
			if len(args) > 1 {
				return fmt.Errorf("usage: voie connect [provider]")
			}

			provider, err := selectProvider(command, args, store)
			if err != nil {
				return err
			}
			value := strings.TrimSpace(token)
			if value == "" {
				value, err = readToken(command)
				if err != nil {
					return err
				}
			}
			if value == "" {
				if provider == "opencode-zen" || provider == "opencode-go" {
					value = "voie-local"
				} else {
					return fmt.Errorf("a token is required for %s", provider)
				}
			}
			if isGatewayProvider(provider) {
				if _, err := connectInstaller(command.Context(), provider); err != nil {
					return fmt.Errorf("install %s toolchain: %w", provider, err)
				}
			}
			if check != nil {
				if err := check(provider, value); err != nil {
					return fmt.Errorf("validate %s token: %w", provider, err)
				}
			}
			if err := store.Set(provider, value); err != nil {
				return fmt.Errorf("save %s token: %w", provider, err)
			}
			fmt.Fprintf(command.OutOrStdout(), "%s connected\n", provider)
			return nil
		},
	}
	command.Flags().StringVar(&token, "token", "", "provider token (non-interactive)")
	command.Flags().BoolVar(&list, "list", false, "list connectable providers and their state")
	return command
}

func validateToken(provider, token string) error {
	url := ""
	switch provider {
	case "openrouter":
		url = "https://openrouter.ai/api/v1/key"
	case "orcarouter":
		url = "https://api.orcarouter.ai/v1/models"
	case "opencode-zen", "opencode-go":
		url = "http://127.0.0.1:8090/healthz"
	default:
		return nil
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("provider rejected the token (HTTP %d)", resp.StatusCode)
	}
	return nil
}

func isConnectable(name string) bool {
	for _, provider := range connectableProviders {
		if provider == name {
			return true
		}
	}
	return false
}

func selectProvider(command *cobra.Command, args []string, store *config.Credentials) (string, error) {
	if len(args) == 1 {
		if !isConnectable(args[0]) {
			return "", fmt.Errorf("unknown provider %q", args[0])
		}
		return args[0], nil
	}
	names := append([]string(nil), connectableProviders...)
	sort.Strings(names)
	fmt.Fprintln(command.ErrOrStderr(), "Available providers:")
	for i, name := range names {
		state := "not connected"
		if store.Token(name) != "" {
			state = "connected"
		}
		fmt.Fprintf(command.ErrOrStderr(), "  %d) %-14s %s\n", i+1, name, state)
	}
	fmt.Fprint(command.ErrOrStderr(), "Provider number: ")
	var answer string
	if _, err := fmt.Fscan(command.InOrStdin(), &answer); err != nil {
		return "", fmt.Errorf("read provider selection: %w", err)
	}
	var index int
	if _, err := fmt.Sscanf(answer, "%d", &index); err != nil || index < 1 || index > len(names) {
		return "", fmt.Errorf("invalid provider selection %q", answer)
	}
	return names[index-1], nil
}

func readToken(command *cobra.Command) (string, error) {
	input := command.InOrStdin()
	if file, ok := input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		fmt.Fprint(command.ErrOrStderr(), "Token: ")
		value, err := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(command.ErrOrStderr())
		return strings.TrimSpace(string(value)), err
	}
	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
