package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

const chatHelpIntro = `Send a prompt using one of three strategies. Classic calls one explicit model; auto chooses a model from a routing profile; ensemble runs several agents and synthesizes their answers. All three share the same retry and fallback policy. Prompt input can come from arguments or stdin, and successful output contains only the answer on stdout. --task names a routing profile; built-in task hints are coding, reasoning, writing, translation, summarization, and general, and custom profile names are supported.`

const chatHelpFallback = `Fallback is tuned with --fallback, --no-fallback, --fallback-retries (0-3), --fallback-limit (0-3), and repeatable --fallback-model, which replaces the configured candidate list for every strategy. Runtime configuration is loaded when the command runs.`

type chatHelpSection struct {
	long     string
	examples string
}

var chatHelpSections = map[string]chatHelpSection{
	"classic": {
		long: `Classic requires --model (and accepts --provider to constrain selection). Use it when you know exactly which model or alias to call. Without --model, the generic openai model or the configured DEFAULT_PROVIDER is used.`,
		examples: `  voie chat --model MODEL_ID "Summarize this text"
  cat prompt.txt | voie chat --model MODEL_ID
  voie chat --model MODEL_ID --provider PROVIDER "Hello"
  voie chat --model MODEL_ID --fallback-model FALLBACK_ID "Retry on another model"`,
	},
	"auto": {
		long: `Auto selects one eligible model from a routing profile (--task, or the configured default). Local selection is deterministic by default; a profile can opt into ROUTER_MODEL. --model and --ensemble-model are not accepted.`,
		examples: `  voie chat --strategy auto --task coding "Review this code"
  voie chat --strategy auto --task general --conversation ID "Continue the discussion"`,
	},
	"ensemble": {
		long: `Ensemble runs two to five agents and synthesizes their answers with SYNTHESIS_MODEL (or ROUTER_MODEL). Modes are independent (parallel drafts), chain (each agent reviews the previous draft), and debate (agents revise peers' drafts for --debate-turns rounds, 0-5). Use --candidate-count or repeatable --ensemble-model to choose agents; --chain-agents is accepted for compatibility with chain requests.`,
		examples: `  voie chat --strategy ensemble --candidate-count 3 "Compare these designs"
  voie chat --strategy ensemble --ensemble-mode chain --candidate-count 3 "Review this proposal"
  voie chat --strategy ensemble --ensemble-mode debate --debate-turns 2 --ensemble-model MODEL_A --ensemble-model MODEL_B "Compare these designs"`,
	},
}

// configureChatHelp makes `voie chat --help` describe all three strategies and
// `voie chat --strategy <name> --help` show only the matching section.
func configureChatHelp(command *cobra.Command, strategy *string) {
	defaultHelp := command.HelpFunc()
	command.SetHelpFunc(func(c *cobra.Command, args []string) {
		strategyName := strings.ToLower(strings.TrimSpace(*strategy))
		selected := chatHelpSections[strategyName]
		long := chatHelpIntro
		examples := ""
		if selected.long != "" {
			long += "\n\n" + strings.ToUpper(strategyName[:1]) + strategyName[1:] + ": " + selected.long
			examples = selected.examples
		} else {
			for _, name := range []string{"classic", "auto", "ensemble"} {
				section := chatHelpSections[name]
				long += "\n\n" + strings.ToUpper(name[:1]) + name[1:] + ": " + section.long
				if examples != "" {
					examples += "\n\n"
				}
				examples += section.examples
			}
		}
		c.Long = long + "\n\n" + chatHelpFallback
		c.Example = examples
		defaultHelp(c, args)
	})
}
