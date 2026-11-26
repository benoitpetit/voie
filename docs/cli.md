# CLI guide

Install a prebuilt release with the system installer in the [README installation guide](../README.md#install-a-release), or download an archive for your operating system and architecture from the [latest release](https://github.com/benoitpetit/voie/releases/latest). The installers verify `checksums.txt` before replacing a binary.

To build the single executable from source, run this from the repository root:

```bash
go build -o voie .
```

The monochrome Unicode wordmark appears in `voie --help`. Command output stays free of branding so chat text and JSON remain easy to pipe into other tools.

## Commands

```text
voie serve [--host HOST] [--port PORT]
voie connect [provider] [--token TOKEN] [--field KEY=VALUE] [--list]
voie install [provider] [--list] [--verbose]
voie keys [provider]
voie chat --model MODEL [--provider PROVIDER] [--fallback-model ID]... [PROMPT...]
voie chat --strategy auto [--task TASK] [--conversation ID] [--fallback-model ID]... [PROMPT...]
voie chat --strategy ensemble [--candidate-count N] [--ensemble-model ID]... [--task PROFILE] [--ensemble-mode MODE] [--chain-agents N] [--debate-turns N] [--conversation ID] [PROMPT...]
voie conversations create|list|show|delete
voie models [--json] [--free|--paid|--all] [--provider NAME]
voie providers [--json]
voie healthcheck
voie mcp
voie completion SHELL
voie version
voie [--version|-v]
voie update
```

Cobra provides root and per-command help without loading configuration or initializing providers:

```bash
voie --help
voie help chat
voie chat --help
voie serve --help
voie completion --help
```

The root help includes examples for model discovery, `chat` with an argument or stdin, starting the API, and the MCP client command. The command help also includes focused examples:

```bash
voie models
voie models --json
voie providers
voie providers --json
voie healthcheck
voie chat --model MODEL_ID "Summarize this text"
cat prompt.txt | voie chat --model MODEL_ID
voie serve --host 127.0.0.1 --port 8080
voie mcp
```

`chat` also accepts `-m` for `--model` and `-p` for `--provider`. Invalid commands and flags return an error and display usage. The command tree contains `serve`, `chat`, `conversations`, `models`, `providers`, `mcp`, `healthcheck`, `connect`, `install`, `keys`, `completion`, `version`, and `update`.

`version`, `--version`, and `-v` print the executable's embedded build version without initializing providers. `update` checks GitHub for the newest stable release, downloads the matching asset and `checksums.txt`, verifies SHA-256, then replaces the current executable. Run it from a release build; development builds tagged `dev` are refused. Windows stages the checked executable and completes replacement after the current process exits. Updating may require write access to the current executable's directory.

`completion` generates shell completion scripts with Cobra's built-in command. Run `voie completion bash`, `voie completion zsh`, `voie completion fish`, or `voie completion powershell`; generated scripts invoke the CLI's hidden completion endpoint, so install or source the output using your shell's normal completion setup.

`serve` starts the HTTP API using configured `HOST` and `PORT`; flags override them for that run. A non-loopback host still requires `API_TOKEN`. Runtime configuration loads when a command runs, not when help is requested.

`healthcheck` probes the local `/health` endpoint on `127.0.0.1:$PORT`, includes `API_TOKEN` when configured, and exits nonzero unless the server returns `200`. The Docker image uses it for its container health check.

`--strategy` selects `classic` (explicit `--model`), `auto` (routing profile) or `ensemble` (`independent`, `chain`, `debate`). `voie chat --help` documents all three strategies with examples; `voie chat --strategy <name> --help` shows only the matching section. `chat` invokes the shared service directly in the current process. Classic requests require a supported model ID with `--model`; auto and ensemble requests can omit it. `--task` names a routing profile from `routing.json`; shell completion reads configured profile names. Local selection is deterministic by default, and a profile can opt into `ROUTER_MODEL`. `--candidate-count` selects 2–5 ensemble agents automatically. Repeat `--ensemble-model ID` to supply an exact list; when both flags are present, the count must match the number of IDs. The old `--models ID,ID` form remains supported. `--ensemble-mode` selects `independent`, `chain`, or `debate`; `--chain-agents` remains available for chain requests, and `--debate-turns` sets 0–5 review rounds (zero means no review rounds). `--conversation` resumes local history. Prompt input can come from arguments or stdin. The command writes answer text only to stdout. Progress appears on stderr: interactive terminals show a monochrome spinner with routing/model stages, while redirected stderr receives plain progress lines. Progress never includes prompt or answer text. Errors are returned to stderr by the executable and use a nonzero exit code.

Fallback can be tuned per request. `--fallback` enables retries and fallback with default budgets when the configured budgets are both zero, `--no-fallback` disables both, `--fallback-retries N` sets same-model retries and `--fallback-limit N` sets the maximum extra models tried after the primary (each `N` in `0`–`3`), and repeatable `--fallback-model ID` replaces the configured candidate list in order for every strategy, including auto and ensemble. When no fallback flag is present the request inherits the global policy. `--fallback` and `--no-fallback` conflict, and bounds are validated before the runtime loads; `--no-fallback` performs a single attempt.

```bash
voie chat --model MODEL_ID --no-fallback "Do not retry this"
voie chat --model MODEL_ID --fallback-retries 0 "Skip the retry, go straight to fallback"
voie chat --model MODEL_ID --fallback-model MODEL_B --fallback-model MODEL_C "Try these models in order"
voie chat --strategy auto --task coding "Review this patch"
voie chat --strategy ensemble --ensemble-mode chain --candidate-count 3 "Review this proposal"
voie chat --strategy ensemble --ensemble-mode independent --candidate-count 3 --ensemble-model MODEL_A --ensemble-model MODEL_B --ensemble-model MODEL_C "Compare these designs"
voie chat --strategy ensemble --ensemble-mode debate --debate-turns 2 "Compare these designs"
ID=$(voie conversations create)
voie chat --strategy auto --conversation "$ID" "Continue the discussion"
voie conversations list
voie conversations show "$ID"
voie conversations delete "$ID"
```

`conversations list` prints summaries without transcripts. `show` prints the stored transcript as JSON. Sessions are stored locally, expire after `CONVERSATION_TTL` of inactivity (default `720h`), and successful turns refresh expiry.

Explicit `--ensemble-model` values (or legacy `--models`) are the ensemble candidates; do not include the configured synthesis model in that list.

`models` lists the callable catalogue in a table with `PROVIDER`, `MODEL`, `TIER` and `PRICE` columns, or as JSON with `--json`. `--free` and `--paid` filter by tier (they conflict), `--all` is the default, and `--provider NAME` limits the output to one provider. Paid models are callable; the upstream decides whether the request succeeds. `providers` lists provider metadata and performs HTTP URL reachability checks in a table with provider, label, status, and model-count columns; its JSON mode returns an object with `data`, `count`, and `timestamp`. A provider marked `alive` has responded to an HTTP GET; this does not guarantee inference works.

`mcp` runs the MCP stdio server. Use this command in an MCP client configuration rather than starting it as an interactive shell command.

## Configuration

The commands use the same environment variables as the HTTP runtime:

| Variable | Default | Description |
| --- | --- | --- |
| `HOST` | `127.0.0.1` | HTTP bind host |
| `PORT` | `8080` | HTTP bind port |
| `DEBUG` | `false` | Debug logging |
| `DEFAULT_PROVIDER` | empty | Provider used for the API's generic `openai` model |
| `TIMEOUT` | `120` | End-to-end timeout in seconds |
| `API_TOKEN` | empty | Required for non-loopback HTTP binds |
| `REQUIRE_CONVERSATION_SCOPE` | `false` | Require a trusted conversation scope on hosted `/v1/*`; requires `API_TOKEN` |
| `ROUTER_MODEL` | empty | Model selected by profiles using router selection; fallback synthesizer |
| `SYNTHESIS_MODEL` | empty | Ensemble synthesis model; defaults to `ROUTER_MODEL` |
| `ROUTING_CONFIG_PATH` | `<user-config-dir>/voie/routing.json` | Local routing policy file |
| `CONVERSATION_DB_PATH` | `<user-config-dir>/voie/conversations.db` | Local conversation database |
| `CONVERSATION_TTL` | `720h` | Conversation inactivity lifetime |
| `FALLBACK_ENABLED` | `true` | Enable retries and fallback on provider failures |
| `FALLBACK_MAX_RETRIES` | `1` | Same-model retries for transient failures (0-3) |
| `FALLBACK_MAX_MODELS` | `1` | Extra models tried after the primary (0-3) |
| `OPENCODE_ZEN_KEY` | `voie-local` | Local gateway key for `opencode-zen` and `opencode-go` |
| `VOIE_OPENROUTER_KEY` | saved token | OpenRouter API key |
| `VOIE_ORCAROUTER_KEY` | saved token | OrcaRouter API key |
| `VOIE_ANTIGRAVITY_KEY` | `voie-local` | Local CLIProxyAPI key |
| `VOIE_KIRO_KEY` | `voie-local` | Local Kiro ACP gateway key |
| `VOIE_CLINE_KEY` | `voie-local` | Local Cline ACP bridge key |

The `VOIE_*_KEY` overrides for the other token, ACP and gateway providers follow the same pattern (`VOIE_<PROVIDER>_KEY`); see the README configuration table for the full list. `VOIE_OPENCODE_GATEWAY_URL`, `KIRO_GATEWAY_DIR` and `VOIE_CLINE_FREE_FEED_URL` override local gateway locations.

On Linux, `<user-config-dir>` is typically `~/.config`; macOS and Windows use their standard per-user configuration directories.

The MCP transport writes protocol messages to stdout. `voie mcp` sends application logs to stderr, including per-tool start, completion/failure, and duration records. Tool arguments, results, and conversation contents are not included in logs. All log output is monochrome.

## Troubleshooting

- If a model ID is rejected, run `voie models` and use an exact current ID.
- If a provider is unavailable, try another model from the runtime catalogue. Provider URL reachability does not confirm inference availability.
- Duck.ai chat requires Chrome or Chromium installed on the host.
- If `serve` rejects a public bind, set a nonempty `API_TOKEN` before starting it.


`voie connect pplx-proxy` installs the optional pplx-proxy gateway, opens a browser for Perplexity sign-in, captures the session cookie into `~/.config/voie/gateways/pplx-proxy/.env` (0600) and starts the gateway. Perplexity models then appear in `voie models`. An expired or invalid cookie produces an error asking to rerun `voie connect pplx-proxy`; the cookie value is never logged.


`voie install` without arguments prints a compact `NAME / TYPE / STATUS / ACCOUNT / NEXT` table. `STATUS` is `not installed`, `installed`, `connected`, `not connected` or `error` (a failed bridge/session that needs reconnecting). `ACCOUNT` is `subscription` for CLI agents, `token` or `none` for APIs, and `none` / `upstream` / `dashboard+upstream` for gateways: `NEXT` is the single command to run (`voie install <name>`, `voie connect <name>`, or `NONE` when nothing remains to do (already connected, or a keyless gateway that is installed)). Long install commands, ports, labels and setup hints are available with `voie install --verbose` and are always printed when actually installing a provider.

`voie connect disconnect <provider>` removes the saved key and its connection state, so the provider's models disappear from `voie models`. For CLI agents and local gateways, voie asks whether to also delete the installed npm/uv package or the cloned gateway directory; answering no keeps the files. `--remove-app` removes them without a prompt (for scripts), and `--keep-app` keeps them without prompting.

Only the `auto` lane of pplx-proxy is classified `free`; the subscription models it advertises are `paid` with `price unknown`. A `403` from Perplexity means the connected account tier cannot use that model — the upstream error message is surfaced verbatim.

Every `free` model exposes `USD 0/0` as its price (zero cost to the caller); public upstream prices replace that value when available. Paid models with no public price show `price unknown`.

OpenRouter also exposes its public catalogue of paid models as `paid`, with real per-token USD prices from its public API; verified `:free` aliases remain `free` with `USD 0/0`.

ACP-backed CLI providers publish the models advertised by the agent itself (cached for 24 h after a connection) alongside their `*-auto` alias; they run on your own subscription and show `subscribe` in the PRICE column. OrcaRouter publishes its authenticated model catalogue as `paid` with real per-token USD prices; unpriced router aliases stay `free`.

## A2A agents

`voie connect a2a:<name> --url https://agent.example [--token TOKEN]` discovers and caches the remote `/.well-known/agent-card.json`, and exposes its skills as `a2a-<name>-<skill>` model IDs. `voie models --provider a2a:<name>` lists them. `voie chat --model a2a-<name>-<skill> --stream PROMPT` streams chunks; the same messages are sent to the remote agent with JSON-RPC `SendMessage` (or `SendStreamingMessage`). The local HTTP server also accepts A2A calls at `POST /a2a` and publishes its own card at `/.well-known/agent-card.json`.

`voie conversations list` accepts `--limit` and `--offset` for paginated local history; it defaults to `100, 0` like the HTTP and MCP surfaces.

