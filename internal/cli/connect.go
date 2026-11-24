package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
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
	for _, spec := range providers.ExternalGateways {
		names = append(names, spec.Name)
	}
	return names
}

var installExternalGatewayFn = installExternalGateway
var capturePerplexityCookie = providers.CapturePerplexityCookie
var startExternalGateway = providers.StartExternalGateway

var uninstallRunner install.Runner

// confirmRemoveApp asks before deleting local CLI packages or cloned gateways.
// Without a terminal it defaults to keeping the files.
var confirmRemoveApp = func(command *cobra.Command, provider string) bool {
	file, ok := command.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return false
	}
	answer, err := readLine(command, bufio.NewReader(command.InOrStdin()), "Also remove the local CLI/gateway files for "+provider+"? [y/N]: ", false)
	return err == nil && (strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"))
}

var connectInstaller = install.Ensure

var startClineBridge = providers.StartClineBridge
var startACPBridge = providers.StartACPBridge

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
	var removeApp, keepApp bool
	command := &cobra.Command{
		Use:   "connect [provider]",
		Short: "Connect a provider with its token",
		Long:  "Connect stores one token per provider in the user config directory (0600). Environment variables override saved tokens. Disconnecting removes the saved key, and can also remove local CLI/gateway files after confirmation. The two opencode providers use the local gateway key, which defaults to voie-local.",
		Example: `  voie connect
  voie connect openrouter --token sk-or-v1-...
  voie connect --list
  voie connect disconnect openrouter`,
		Args: cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) (err error) {
			store, err := config.LoadCredentials()
			if err != nil {
				return fmt.Errorf("load credentials: %w", err)
			}
			if list {
				if len(args) != 0 {
					return fmt.Errorf("voie connect --list takes no provider")
				}
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
				if err := store.SetState(args[1], ""); err != nil {
					return fmt.Errorf("disconnect %s state: %w", args[1], err)
				}
				fmt.Fprintf(command.OutOrStdout(), "%s disconnected; the saved key has been removed.\n", args[1])
				if hasLocalApp(args[1]) && (removeApp || (!keepApp && confirmRemoveApp(command, args[1]))) {
					if err := removeProviderFiles(args[1]); err != nil {
						return fmt.Errorf("remove %s local files: %w", args[1], err)
					}
					fmt.Fprintf(command.OutOrStdout(), "%s local CLI/gateway files removed\n", args[1])
				}
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
			if spec, ok := providers.ExternalGatewayByName(provider); ok {
				return connectExternalGateway(command, spec)
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
				supplied[field.Key] = value
			}
			if check != nil {
				if err := check(provider, supplied[provider]); err != nil {
					return fmt.Errorf("validate %s token: %w", provider, err)
				}
			}
			// Save only once every field is known: a cancellation on the second
			// field must not leave a half-configured provider behind.
			previous := make(map[string]string, len(supplied))
			for key := range supplied {
				previous[key] = store.Raw(key)
			}
			saved := false
			bridgeError := false
			defer func() {
				if err == nil {
					if store.State(provider) == "bridge error" {
						_ = store.SetState(provider, "")
					}
					return
				}
				if !saved {
					return
				}
				if bridgeError {
					// The token may still be valid; only the local bridge failed.
					_ = store.SetState(provider, "bridge error")
					return
				}
				for key, value := range previous {
					if value == "" {
						_ = store.Delete(key)
					} else {
						_ = store.Set(key, value)
					}
				}
			}()
			for key, value := range supplied {
				if err := store.Set(key, value); err != nil {
					return fmt.Errorf("save %s credential: %w", provider, err)
				}
			}
			saved = true
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
					if err := startACPBridge(command.Context(), provider); err != nil {
						bridgeError = true
						return fmt.Errorf("start %s ACP bridge: %w", provider, err)
					}
				} else if provider == "cline" {
					if err := startClineBridge(command.Context()); err != nil {
						bridgeError = true
						return fmt.Errorf("start cline ACP bridge (did you run `cline auth`?): %w", err)
					}
				}
			}
			if isGatewayProvider(provider) {
				if err := store.SetState(provider, "connected"); err != nil {
					return fmt.Errorf("record %s connection: %w", provider, err)
				}
			}
			fmt.Fprintf(command.OutOrStdout(), "%s connected\n", provider)
			return nil
		},
	}
	command.Flags().StringVar(&token, "token", "", "provider token (non-interactive)")
	command.Flags().StringArrayVar(&fields, "field", nil, "additional credential as key=value (repeatable)")
	command.Flags().BoolVar(&list, "list", false, "list connectable providers and their state")
	command.Flags().BoolVar(&removeApp, "remove-app", false, "also remove local CLI/gateway files (no prompt)")
	command.Flags().BoolVar(&keepApp, "keep-app", false, "keep local CLI/gateway files (default)")
	return command
}

func hasLocalApp(provider string) bool {
	if _, ok := providers.ExternalGatewayByName(provider); ok {
		return true
	}
	if _, ok := providers.ACPProviderSpecByName(provider); ok {
		return true
	}
	return provider == "antigravity" || provider == "kiro" || provider == "cline"
}

func removeProviderFiles(provider string) error {
	if dir, command := providers.ExternalGatewayUninstall(provider); dir != "" {
		if len(command) > 0 {
			if uninstallRunner != nil {
				if err := uninstallRunner(command[0], command[1:]...); err != nil {
					return err
				}
			} else {
				cmd := exec.Command(command[0], command[1:]...)
				cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
				if err := cmd.Run(); err != nil {
					return err
				}
			}
		}
		return os.RemoveAll(dir)
	}
	return install.Uninstall(context.Background(), provider, uninstallRunner)
}

// gatewayLoopbackKey is the bearer token voie sends to a local gateway.
func gatewayLoopbackKey(name string) string {
	store, err := config.LoadCredentials()
	if err == nil {
		if key := store.Token(name); key != "" {
			return key
		}
	}
	return "voie-local"
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
		// The gateway is external; its absence must not block saving the key.
		return nil
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
func connectExternalGateway(command *cobra.Command, spec providers.ExternalGatewaySpec) error {
	printKeyHelp(command, spec.Name)
	installed, _, _ := providers.ExternalGatewayStatus(spec.Name)
	if !installed {
		if err := installExternalGatewayFn(command, spec); err != nil {
			return err
		}
	}
	if err := providers.ProvisionGatewayEnv(spec); err != nil {
		return fmt.Errorf("prepare %s configuration: %w", spec.Name, err)
	}
	if len(spec.SetupCommand) > 0 {
		setup := strings.Join(spec.SetupCommand, " ")
		if file, ok := command.InOrStdin().(*os.File); ok && term.IsTerminal(int(file.Fd())) {
			answer, err := readLine(command, bufio.NewReader(command.InOrStdin()), "Run "+setup+" to set up an account? [Y/n]: ", false)
			if err != nil {
				return err
			}
			if strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes") || answer == "" {
				fmt.Fprintf(command.OutOrStdout(), "+ %s\n", setup)
				login := exec.CommandContext(command.Context(), spec.SetupCommand[0], spec.SetupCommand[1:]...)
				login.Stdin = command.InOrStdin()
				login.Stdout = command.OutOrStdout()
				login.Stderr = command.ErrOrStderr()
				if err := login.Run(); err != nil {
					return fmt.Errorf("account setup for %s: %w", spec.Name, err)
				}
			}
		} else {
			fmt.Fprintf(command.OutOrStdout(), "Set up an account first with: %s\n", setup)
		}
	}
	if spec.Name == "pplx-proxy" {
		if err := providers.SetGatewayEnvValue(spec.Name, "PPLX_PROXY_API_KEY", gatewayLoopbackKey(spec.Name)); err != nil {
			return fmt.Errorf("configure %s loopback key: %w", spec.Name, err)
		}
		fmt.Fprintln(command.OutOrStdout(), "Sign in to Perplexity in the browser window that opens; voie will capture the session cookie.")
		cookie, err := capturePerplexityCookie(command.Context())
		if err != nil {
			return fmt.Errorf("capture Perplexity cookie: %w", err)
		}
		if err := providers.SetGatewayEnvValue(spec.Name, "PPLX_COOKIE", cookie); err != nil {
			return fmt.Errorf("save Perplexity cookie: %w", err)
		}
	}
	if err := startExternalGateway(spec.Name); err != nil {
		return fmt.Errorf("start %s gateway: %w", spec.Name, err)
	}
	if err := providers.ProvisionGatewayKey(spec); err != nil {
		return fmt.Errorf("provision %s key: %w", spec.Name, err)
	}
	providers.RefreshGatewayModels(spec.Name)
	if spec.DashboardURL != "" {
		fmt.Fprintf(command.OutOrStdout(), "Dashboard for account setup: %s\n", spec.DashboardURL)
		if code := providers.SetupCodeFromLogs(spec); code != "" {
			fmt.Fprintf(command.OutOrStdout(), "First-run setup code: %s (enter it in the dashboard when asked)\n", code)
		}
		if file, ok := command.InOrStdin().(*os.File); ok && term.IsTerminal(int(file.Fd())) {
			fmt.Fprintln(command.OutOrStdout(), "Create or link your account there, then validate here to stop the gateway.")
			if _, err := readLine(command, bufio.NewReader(command.InOrStdin()), "Press Enter when done: ", false); err != nil {
				return err
			}
			providers.ShutdownGateways()
			fmt.Fprintln(command.OutOrStdout(), "Gateway stopped.")
		} else {
			fmt.Fprintln(command.OutOrStdout(), "Non-interactive session: the gateway will stop when this command exits.")
		}
	}
	store, err := config.LoadCredentials()
	if err != nil {
		return fmt.Errorf("load credentials: %w", err)
	}
	if err := store.SetState(spec.Name, "connected"); err != nil {
		return fmt.Errorf("record %s connection: %w", spec.Name, err)
	}
	account, _ := providers.ExternalGatewayAccount(spec.Name)
	fmt.Fprintf(command.OutOrStdout(), "%s ready (account: %s, model: %s)\n", spec.Name, account.Required, spec.Alias)
	return nil
}

// connectPicker prints the same flat provider list for `connect --list` and
// for the interactive selector. The selector adds a leading number column.
func connectPicker(table *tabwriter.Writer, store *config.Credentials, numbered, showCancel bool) []string {
	typeByName := make(map[string]string)
	typeByName["openrouter"] = "token"
	typeByName["orcarouter"] = "token"
	for _, name := range providers.TokenProviderNames() {
		typeByName[name] = "token"
	}
	for _, name := range []string{"opencode-zen", "opencode-go", "antigravity", "kiro", "cline", "codebuddy", "minimax-code", "devin", "qoder", "copilot-cli", "cursor"} {
		typeByName[name] = "cli"
	}
	for _, spec := range providers.ACPProviders {
		typeByName[spec.Name] = "cli"
	}
	for _, spec := range providers.ExternalGateways {
		typeByName[spec.Name] = providers.ExternalGatewayType(spec)
	}

	names := append([]string(nil), connectableProviders...)
	sort.Strings(names)
	if numbered {
		fmt.Fprintln(table, "#\tNAME\tTYPE\tSTATE")
	} else {
		fmt.Fprintln(table, "NAME\tTYPE\tSTATE")
	}
	for index, name := range names {
		state := "not connected"
		if strings.EqualFold(store.State(name), "connected") {
			state = "connected"
		} else if store.Token(name) != "" {
			state = "connected"
			if _, ok := providers.ExternalGatewayByName(name); ok {
				account, _ := providers.ExternalGatewayAccount(name)
				if account.Required != "none" && !strings.EqualFold(store.State(name), "connected") {
					state = "installed — account: " + account.Required
				}
			}
			if marked := store.State(name); marked != "" && marked != "connected" {
				state = marked
			}
		} else if name == "opencode-zen" || name == "opencode-go" {
			state = "available (anonymous / local key)"
		} else if name == "vlm-run" {
			state = "available (anonymous bearer)"
		} else if _, ok := providers.ExternalGatewayByName(name); ok {
			installed, _, _ := providers.ExternalGatewayStatus(name)
			account, _ := providers.ExternalGatewayAccount(name)
			state = "not installed (voie install " + name + ")"
			if installed {
				state = "installed — account: " + account.Required
			}
		}
		if numbered {
			fmt.Fprintf(table, "%d\t%s\t%s\t%s\n", index+1, name, typeByName[name], state)
		} else {
			fmt.Fprintf(table, "%s\t%s\t%s\n", name, typeByName[name], state)
		}
	}
	if showCancel {
		fmt.Fprintln(table, "0\tCancel\t\t")
	}
	return names
}

func runConnectList(out io.Writer, store *config.Credentials) error {
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	connectPicker(table, store, true, false)
	return table.Flush()
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
		if file, ok := command.InOrStdin().(*os.File); !ok || !term.IsTerminal(int(file.Fd())) {
			return fmt.Errorf("interactive login required: run `%s %s` in a terminal first", filepath.Base(binary), strings.Join(login, " "))
		}
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
	fmt.Fprintln(command.ErrOrStderr(), "Available providers:")
	table := tabwriter.NewWriter(command.ErrOrStderr(), 0, 0, 2, ' ', 0)
	names := connectPicker(table, store, true, true)
	table.Flush()
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
