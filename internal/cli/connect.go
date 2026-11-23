package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/install"
	"github.com/benoitpetit/voie/providers"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var connectableProviders = buildConnectableProviders()

func buildConnectableProviders() []string {
	names := []string{"opencode-zen", "opencode-go", "openrouter", "orcarouter", "antigravity", "kiro", "cline"}
	names = append(names, providers.TokenProviderNames()...)
	for _, spec := range providers.ACPProviders {
		names = append(names, spec.Name)
	}
	return names
}

var connectInstaller = install.Ensure

var errConnectCanceled = errors.New("connect canceled")

func isGatewayProvider(provider string) bool {
	if provider == "antigravity" || provider == "kiro" || provider == "cline" {
		return true
	}
	_, ok := providers.ACPProviderSpecByName(provider)
	return ok
}

type tokenChecker func(provider, token string) error

func newConnectCommand(check tokenChecker) *cobra.Command {
	if check == nil {
		check = validateToken
	}
	var token string
	var fields []string
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
				return runConnectList(command.OutOrStdout(), store)
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

			input := bufio.NewReader(command.InOrStdin())
			provider, err := selectProvider(command, args, store, input)
			if errors.Is(err, errConnectCanceled) {
				fmt.Fprintln(command.OutOrStdout(), "Canceled.")
				return nil
			}
			if err != nil {
				return err
			}
			printKeyHelp(command, provider)
			required := providers.ConnectFields(provider)
			supplied := make(map[string]string)
			for _, item := range fields {
				key, value, ok := strings.Cut(item, "=")
				if !ok {
					return fmt.Errorf("--field must be key=value, got %q", item)
				}
				supplied[strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
			if first := strings.TrimSpace(token); first != "" && len(required) > 0 {
				supplied[required[0].Key] = first
			}
			for _, field := range required {
				value := supplied[field.Key]
				if value == "" {
					if field.Secret {
						value, err = readTokenWithLabel(command, field.Label, input)
					} else {
						value, err = readValueWithLabel(command, field.Label, input)
					}
					if errors.Is(err, errConnectCanceled) {
						fmt.Fprintln(command.OutOrStdout(), "Canceled.")
						return nil
					}
					if err != nil {
						return err
					}
				}
				if value == "" {
					value = field.Default
				}
				if value == "" {
					return fmt.Errorf("%s is required for %s", field.Label, provider)
				}
				if err := store.Set(field.Key, value); err != nil {
					return fmt.Errorf("save %s credential: %w", provider, err)
				}
				supplied[field.Key] = value
			}
			if isGatewayProvider(provider) {
				binary, err := connectInstaller(command.Context(), provider)
				if err != nil {
					return fmt.Errorf("install %s toolchain: %w", provider, err)
				}
				if provider == "cline" && token == "" {
					if err := ensureClineAuth(command, binary); err != nil {
						return fmt.Errorf("cline auth: %w", err)
					}
				}
				if spec, ok := providers.ACPProviderSpecByName(provider); ok {
					if err := ensureACPAuth(command, binary, spec.AuthCheck, spec.AuthLogin, input); err != nil {
						return fmt.Errorf("%s auth: %w", provider, err)
					}
					if err := providers.StartACPBridge(provider); err != nil {
						return fmt.Errorf("start %s ACP bridge: %w", provider, err)
					}
				} else if provider == "cline" {
					if err := providers.StartClineBridge(); err != nil {
						return fmt.Errorf("start cline ACP bridge (did you run `cline auth`?): %w", err)
					}
				}
			}
			if check != nil {
				if err := check(provider, supplied[provider]); err != nil {
					return fmt.Errorf("validate %s token: %w", provider, err)
				}
			}
			fmt.Fprintf(command.OutOrStdout(), "%s connected\n", provider)
			return nil
		},
	}
	command.Flags().StringVar(&token, "token", "", "provider token (non-interactive)")
	command.Flags().StringArrayVar(&fields, "field", nil, "additional credential as key=value (repeatable)")
	command.Flags().BoolVar(&list, "list", false, "list connectable providers and their state")
	return command
}

func validateToken(provider, token string) error {
	// Cloudflare Workers AI has no documented GET /ai/v1/models endpoint;
	// validating there returns 405 even with valid credentials. The first real
	// completion reports authentication problems clearly instead.
	if provider == "cloudflare-ai" {
		return nil
	}
	url := ""
	switch provider {
	case "openrouter":
		url = "https://openrouter.ai/api/v1/key"
	case "orcarouter":
		url = "https://api.orcarouter.ai/v1/models"
	case "opencode-zen", "opencode-go":
		url = "http://127.0.0.1:8090/healthz"
	default:
		url = providers.TokenProviderModelsURL(provider)
		if url == "" {
			return nil
		}
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

// ensureClineAuth launches the official interactive login once, attached to
// the user's terminal. Auth stays inside the Cline CLI; voie never sees it.
func runConnectList(out io.Writer, store *config.Credentials) error {
	tokenProviders := append([]string{"openrouter", "orcarouter"}, providers.TokenProviderNames()...)
	cliProviders := []string{
		"opencode-zen", "opencode-go", "antigravity", "kiro", "cline",
		"codebuddy", "minimax-code", "devin", "qoder", "copilot-cli", "cursor",
	}
	sections := []struct {
		title string
		names []string
	}{
		{"API TOKEN (clé à créer chez le provider)", tokenProviders},
		{"CLI / GATEWAY LOCAL (login dans l'outil officiel)", cliProviders},
	}
	for _, section := range sections {
		if _, err := fmt.Fprintf(out, "\n%s\n", section.title); err != nil {
			return err
		}
		for _, name := range section.names {
			state := "not connected"
			if store.Token(name) != "" {
				state = "connected"
			} else if name == "opencode-zen" || name == "opencode-go" {
				state = "available (anonymous / local key)"
			}
			if _, err := fmt.Fprintf(out, "  %-18s %s\n", name, state); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintln(out, "\nREVERSE-ENGINEERED GATEWAYS (risque ToS, opt-in)"); err != nil {
		return err
	}
	for _, spec := range providers.ExternalGateways {
		installed, _, _ := providers.ExternalGatewayStatus(spec.Name)
		state := "not installed (voie install " + spec.Name + ")"
		if installed {
			state = "installed"
		}
		if _, err := fmt.Fprintf(out, "  %-18s %s\n", spec.Name, state); err != nil {
			return err
		}
	}
	return nil
}

func printKeyHelp(command *cobra.Command, provider string) {
	fmt.Fprintf(command.ErrOrStderr(), "How to get %s credentials:\n", provider)
	for _, step := range providers.KeyHelp(provider) {
		fmt.Fprintf(command.ErrOrStderr(), "  - %s\n", step)
	}
}

func ensureACPAuth(command *cobra.Command, binary string, check, login []string, input *bufio.Reader) error {
	if len(login) == 0 {
		return nil
	}
	if len(check) == 0 {
		answer, err := readLine(command, input, "Launch "+filepath.Base(binary)+" login? [Y/n]: ", false)
		if err != nil {
			return err
		}
		if strings.EqualFold(answer, "n") || strings.EqualFold(answer, "no") {
			return nil
		}
	}
	if len(check) > 0 {
		status := exec.Command(binary, check...)
		output, err := status.CombinedOutput()
		if err == nil && !strings.Contains(strings.ToLower(string(output)), "not logged in") {
			return nil
		}
	}
	fmt.Fprintf(command.ErrOrStderr(), "%s is not authenticated; launching its login command.\n", filepath.Base(binary))
	auth := exec.Command(binary, login...)
	auth.Stdin = command.InOrStdin()
	auth.Stdout = command.OutOrStdout()
	auth.Stderr = command.ErrOrStderr()
	return auth.Run()
}

func ensureClineAuth(command *cobra.Command, clinePath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	credentials := filepath.Join(home, ".cline", "data", "settings", "providers.json")
	if _, err := os.Stat(credentials); err == nil {
		return nil
	}
	fmt.Fprintln(command.ErrOrStderr(), "Cline is not authenticated; launching `cline auth` in your terminal.")
	auth := exec.Command(clinePath, "auth")
	auth.Stdin = command.InOrStdin()
	auth.Stdout = command.OutOrStdout()
	auth.Stderr = command.ErrOrStderr()
	return auth.Run()
}

func isConnectable(name string) bool {
	for _, provider := range connectableProviders {
		if provider == name {
			return true
		}
	}
	return false
}

func selectProvider(command *cobra.Command, args []string, store *config.Credentials, input *bufio.Reader) (string, error) {
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
	fmt.Fprintln(command.ErrOrStderr(), "  0) Cancel")
	answer, err := readLine(command, input, "Provider number: ", false)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(answer) {
	case "", "0", "q", "quit", "exit":
		return "", errConnectCanceled
	}
	var index int
	if _, err := fmt.Sscanf(answer, "%d", &index); err != nil || index < 1 || index > len(names) {
		return "", fmt.Errorf("invalid provider selection %q", answer)
	}
	return names[index-1], nil
}

type lineResult struct {
	text string
	err  error
}

// readLine is cancellable: Ctrl+C is intercepted by main.go as a context
// cancellation, so a blocking stdin read must observe command.Context().
func readLine(command *cobra.Command, input *bufio.Reader, prompt string, secret bool) (string, error) {
	if prompt != "" {
		fmt.Fprint(command.ErrOrStderr(), prompt)
	}
	result := make(chan lineResult, 1)
	go func() {
		if secret {
			if file, ok := command.InOrStdin().(*os.File); ok && term.IsTerminal(int(file.Fd())) {
				value, err := term.ReadPassword(int(file.Fd()))
				fmt.Fprintln(command.ErrOrStderr())
				result <- lineResult{text: string(value), err: err}
				return
			}
		}
		line, err := input.ReadString('\n')
		result <- lineResult{text: line, err: err}
	}()
	select {
	case <-command.Context().Done():
		fmt.Fprintln(command.ErrOrStderr())
		return "", errConnectCanceled
	case res := <-result:
		if res.err != nil && res.err != io.EOF {
			return "", res.err
		}
		return strings.TrimSpace(res.text), nil
	}
}

func readValueWithLabel(command *cobra.Command, label string, input *bufio.Reader) (string, error) {
	return readLine(command, input, label+": ", false)
}

func readTokenWithLabel(command *cobra.Command, label string, input *bufio.Reader) (string, error) {
	return readLine(command, input, label+": ", true)
}
