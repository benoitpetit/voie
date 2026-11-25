package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/install"
	"github.com/benoitpetit/voie/providers"
	"github.com/spf13/cobra"
)

func newInstallCommand() *cobra.Command {
	var list, verbose bool
	command := &cobra.Command{
		Use:          "install [provider]",
		Short:        "List or install optional provider toolchains and third-party gateways",
		Long:         "Without an argument, lists everything voie can install. Passing a name installs it: coding-agent CLIs are installed via npm, third-party gateways via git clone plus their dependencies. Third-party gateways use reverse-engineered endpoints and carry ToS/account risk.",
		Example:      "  voie install\n  voie install omniroute\n  voie install cline",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			if list || len(args) == 0 {
				return runInstallList(command.OutOrStdout(), verbose)
			}
			if spec, ok := providers.ExternalGatewayByName(args[0]); ok {
				return installExternalGateway(command, spec)
			}
			if !isGatewayProvider(args[0]) {
				return fmt.Errorf("provider %q has no installer; run `voie install` to list them", args[0])
			}
			if args[0] == "cline" {
				path, err := install.Install(command.Context(), args[0], nil)
				if err != nil {
					return fmt.Errorf("%w (Cline CLI requires Node.js 20-24; current Node is unsupported by the npm package)", err)
				}
				fmt.Fprintf(command.OutOrStdout(), "%s installed at %s\n", args[0], path)
				return nil
			}
			path, err := install.Install(command.Context(), args[0], nil)
			if err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "%s installed at %s\n", args[0], path)
			return nil
		},
	}
	command.Flags().BoolVar(&list, "list", false, "list available installers")
	command.Flags().BoolVar(&verbose, "verbose", false, "show install commands and setup hints")
	return command
}

func runInstallList(out io.Writer, verbose bool) error {
	store, err := config.LoadCredentials()
	if err != nil {
		return fmt.Errorf("load credentials: %w", err)
	}
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if verbose {
		if _, err := fmt.Fprintln(table, "NAME\tTYPE\tLABEL\tPORT\tSTATUS\tACCOUNT\tINSTALL\tSETUP REQUIRED"); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintln(table, "NAME\tTYPE\tSTATUS\tACCOUNT\tNEXT"); err != nil {
			return err
		}
	}
	write := func(name, kind, label string, port int, status, account, command, hint string) error {
		if verbose {
			return writeInstallRow(table, name, kind, label, port, status, account, command, hint)
		}
		_, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", name, kind, status, account, installNext(kind, status, account, name))
		return err
	}
	for _, spec := range providers.ExternalGateways {
		installed, command, _ := providers.ExternalGatewayStatus(spec.Name)
		status := "not installed"
		if installed {
			status = "installed"
			if strings.EqualFold(store.State(spec.Name), "connected") {
				status = "connected"
			}
		}
		account, _ := providers.ExternalGatewayAccount(spec.Name)
		if err := write(spec.Name, "gateway", spec.Label, spec.Port, status, account.Required, command, providers.ExternalGatewaySetupHint(spec.Name)); err != nil {
			return err
		}
	}
	for _, name := range []string{"cline", "kiro", "antigravity", "codebuddy", "minimax-code", "devin", "qoder", "copilot-cli", "cursor"} {
		status := cliState(name)
		if status == "installed" && strings.EqualFold(store.State(name), "connected") {
			status = "connected"
		}
		if err := write(name, "cli", name, 0, status, "-", "voie install "+name, "-"); err != nil {
			return err
		}
	}
	names := append([]string{"openrouter", "orcarouter"}, providers.TokenProviderNames()...)
	for _, name := range names {
		status := apiProviderState(store, name)
		if err := write(name, "api", name, 0, status, "-", "voie connect "+name, "-"); err != nil {
			return err
		}
	}
	if err := table.Flush(); err != nil {
		return err
	}
	if !verbose {
		fmt.Fprintln(out, "\nRun `voie install --verbose` for install commands and setup hints.")
	}
	return nil
}

// installNext returns the single action a user still needs to take.
func installNext(kind, status, account, name string) string {
	if status == "connected" || strings.HasPrefix(status, "available") {
		return "NONE"
	}
	// A keyless gateway needs no account once installed: it is callable
	// immediately, so no connect step remains.
	if kind == "gateway" && account == "none" && strings.HasPrefix(status, "installed") {
		return "NONE"
	}
	if kind == "api" || strings.HasPrefix(status, "installed") {
		return "voie connect " + name
	}
	return "voie install " + name
}

func writeInstallRow(table *tabwriter.Writer, name, kind, label string, port int, status, account, command, hint string) error {
	portText := "-"
	if port > 0 {
		portText = fmt.Sprint(port)
	}
	_, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", name, kind, label, portText, status, account, command, hint)
	return err
}

// lookPath is a seam for tests that simulate installed agent CLIs.
var lookPath = exec.LookPath

func cliState(name string) string {
	known := map[string]bool{"cline": true, "kiro": true, "antigravity": true}
	for _, spec := range providers.ACPProviders {
		known[spec.Name] = true
	}
	if !known[name] {
		return "not installed"
	}
	if install.BinaryPath(name) != "" {
		return "installed"
	}
	if _, err := lookPath(cliBinary(name)); err == nil {
		return "installed"
	}
	return "not installed"
}

func cliBinary(name string) string {
	if spec, ok := providers.ACPProviderSpecByName(name); ok {
		return spec.Binary
	}
	switch name {
	case "cline":
		return "cline"
	case "kiro":
		return "kiro-cli"
	}
	return name
}

// preflightInstallStep checks that a step's executable is available. Relative
// paths are generated by earlier steps (for example venv/bin/pip) and must be
// resolved inside the gateway directory, not via the caller's PATH.
func preflightInstallStep(step []string, dir string) error {
	binary := step[0]
	if strings.Contains(binary, string(os.PathSeparator)) {
		candidate := binary
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(dir, candidate)
		}
		if info, err := os.Stat(candidate); err != nil || info.IsDir() {
			return fmt.Errorf("%q is not available after the previous install step", binary)
		}
		return nil
	}
	if _, err := exec.LookPath(binary); err != nil {
		return fmt.Errorf("%q is not available; install this tool first (Node 20-24 via `mise use node@22`, Python via `uv`) and retry", binary)
	}
	return nil
}

func installExternalGateway(command *cobra.Command, spec providers.ExternalGatewaySpec) error {
	installed, _, startCommand := providers.ExternalGatewayStatus(spec.Name)
	if len(spec.Install) > 0 && installedByBinary(spec.Start) {
		installed = true
	}
	if installed {
		if err := providers.ProvisionGatewayEnv(spec); err != nil {
			return fmt.Errorf("prepare %s configuration: %w", spec.Name, err)
		}
		fmt.Fprintf(command.OutOrStdout(), "%s is already installed. Start it with: %s\n", spec.Name, startCommand)
		return printSetupHint(command, spec.Name)
	}
	if spec.ManualInstall != "" {
		fmt.Fprintf(command.OutOrStdout(), "%s is installed manually: %s\n", spec.Name, spec.ManualInstall)
		return printSetupHint(command, spec.Name)
	}
	dir := providers.ExternalGatewayDir(spec.Name)
	_, statErr := os.Stat(dir)
	needsClone := os.IsNotExist(statErr)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("prepare %s directory: %w", spec.Name, err)
	}
	steps := make([][]string, 0, len(spec.Deps)+1)
	if len(spec.Install) > 0 {
		steps = append(steps, spec.Install)
	} else if needsClone {
		steps = append(steps, []string{"git", "clone", spec.Repo, dir})
	} else {
		fmt.Fprintf(command.OutOrStdout(), "%s already cloned; running dependencies only\n", spec.Name)
	}
	depsStart := len(steps)
	steps = append(steps, spec.Deps...)
	// Secrets must exist before the depend steps run (docker compose reads
	// .env), but the directory must still be empty for git clone — so generate
	// them between the clone/install step and the dependency steps.
	provisioned := false
	provision := func() error {
		if provisioned {
			return nil
		}
		provisioned = true
		return providers.ProvisionGatewayEnv(spec)
	}
	for index, step := range steps {
		if index == depsStart {
			if err := provision(); err != nil {
				return fmt.Errorf("prepare %s configuration: %w", spec.Name, err)
			}
		}
		if err := preflightInstallStep(step, dir); err != nil {
			if step[0] == "corepack" {
				fmt.Fprintf(command.OutOrStderr(), "skipping corepack (not provided by the active Node); continuing\n")
				continue
			}
			return fmt.Errorf("install %s: %w", spec.Name, err)
		}
		fmt.Fprintf(command.OutOrStdout(), "+ %v\n", step)
		binary := step[0]
		if !filepath.IsAbs(binary) && strings.Contains(binary, string(os.PathSeparator)) {
			binary = filepath.Join(dir, binary)
		}
		cmd := exec.CommandContext(command.Context(), binary, step[1:]...)
		// Every step runs inside the gateway directory: install scripts that
		// download binaries into the current directory must not pollute the
		// caller's working tree (and git clone accepts an existing empty dir).
		cmd.Dir = dir
		if len(spec.Env) > 0 {
			cmd.Env = append(cmd.Env, os.Environ()...)
			for key, value := range spec.Env {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
		}
		cmd.Stdin = command.InOrStdin()
		cmd.Stdout = command.OutOrStdout()
		cmd.Stderr = command.ErrOrStderr()
		if err := cmd.Run(); err != nil {
			if step[0] == "corepack" {
				fmt.Fprintf(command.OutOrStderr(), "corepack unavailable with the active Node; continuing without it\n")
				continue
			}
			return fmt.Errorf("install %s: %w", spec.Name, err)
		}
	}
	if err := provision(); err != nil {
		return fmt.Errorf("prepare %s configuration: %w", spec.Name, err)
	}
	fmt.Fprintf(command.OutOrStdout(), "%s installed. Start it with: %s\n", spec.Name, startCommand)
	return printSetupHint(command, spec.Name)
}

func installedByBinary(start []string) bool {
	if len(start) == 0 {
		return false
	}
	_, err := exec.LookPath(start[0])
	return err == nil
}

func printSetupHint(command *cobra.Command, name string) error {
	hint := providers.ExternalGatewaySetupHint(name)
	if hint != "" {
		fmt.Fprintf(command.OutOrStdout(), "Before inference: %s\n", hint)
	}
	return nil
}
