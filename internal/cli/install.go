package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"text/tabwriter"

	"github.com/benoitpetit/voie/internal/install"
	"github.com/benoitpetit/voie/providers"
	"github.com/spf13/cobra"
)

func newInstallCommand() *cobra.Command {
	var list bool
	command := &cobra.Command{
		Use:          "install [provider]",
		Short:        "List or install optional provider toolchains and third-party gateways",
		Long:         "Without an argument, lists everything voie can install. Passing a name installs it: coding-agent CLIs are installed via npm, third-party gateways via git clone plus their dependencies. Third-party gateways use reverse-engineered endpoints and carry ToS/account risk.",
		Example:      "  voie install\n  voie install omniroute\n  voie install cline",
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			if list || len(args) == 0 {
				return runInstallList(command.OutOrStdout())
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
	return command
}

func runInstallList(out io.Writer) error {
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "NAME\tTYPE\tLABEL\tPORT\tSTATUS\tACCOUNT\tINSTALL\tSETUP REQUIRED"); err != nil {
		return err
	}
	for _, spec := range providers.ExternalGateways {
		installed, command, _ := providers.ExternalGatewayStatus(spec.Name)
		state := "not installed"
		if installed {
			state = "installed"
		}
		hint := providers.ExternalGatewaySetupHint(spec.Name)
		account, _ := providers.ExternalGatewayAccount(spec.Name)
		if _, err := fmt.Fprintf(table, "%s\tgateway\t%s\t%d\t%s\t%s\t%s\t%s\n", spec.Name, spec.Label, spec.Port, state, account.Required, command, hint); err != nil {
			return err
		}
	}
	for _, name := range []string{"cline", "kiro", "antigravity", "codebuddy", "minimax-code", "devin", "qoder", "copilot-cli", "cursor"} {
		if _, err := fmt.Fprintf(table, "%s\tcli\t%s\t-\t%s\t-\tvoie install %s\t-\n", name, name, cliState(name), name); err != nil {
			return err
		}
	}
	if err := table.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(out, "\nAPI token providers are not installed: run `voie keys` for their key URLs, then `voie connect <name>`.")
	return nil
}

func cliState(name string) string {
	switch name {
	case "cline", "kiro", "devin", "minimax-code", "copilot-cli":
		if _, err := exec.LookPath(cliBinary(name)); err == nil {
			return "installed"
		}
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
		if _, err := exec.LookPath(step[0]); err != nil {
			if step[0] == "corepack" {
				fmt.Fprintf(command.OutOrStderr(), "skipping corepack (not provided by the active Node); continuing\n")
				continue
			}
			return fmt.Errorf("install %s: %q is not available; install this tool first (Node 20-24 via `mise use node@22`, Python via `uv`) and retry", spec.Name, step[0])
		}
		fmt.Fprintf(command.OutOrStdout(), "+ %v\n", step)
		cmd := exec.CommandContext(command.Context(), step[0], step[1:]...)
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
