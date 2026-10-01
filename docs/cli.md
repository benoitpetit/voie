# CLI guide

Install a prebuilt release with the system installer in the [README installation guide](../README.md#install-a-release), or download an archive for your operating system and architecture from the [latest release](https://github.com/benoitpetit/voie/releases/latest). The installers verify `checksums.txt` before replacing a binary.

To build the single executable from source, run this from the repository root:

```bash
go build -o voie .
```

The monochrome ASCII wordmark appears in `voie --help`. Command output stays free of branding so chat text and JSON remain easy to pipe into other tools.

## Commands

```text
voie serve [--host HOST] [--port PORT]
voie chat --model MODEL [--provider PROVIDER] [PROMPT...]
voie chat --strategy auto [--task TASK] [--conversation ID] [PROMPT...]
voie chat --strategy ensemble [--models ID,ID] [--task TASK] [--conversation ID] [PROMPT...]
voie conversations create|list|show|delete
voie models [--json]
voie providers [--json]
voie mcp
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
```

The root help includes examples for model discovery, `chat` with an argument or stdin, starting the API, and the MCP client command. The command help also includes focused examples:

```bash
voie models
voie models --json
voie providers
voie providers --json
voie chat --model MODEL_ID "Summarize this text"
cat prompt.txt | voie chat --model MODEL_ID
voie serve --host 127.0.0.1 --port 8080
voie mcp
```

`chat` also accepts `-m` for `--model` and `-p` for `--provider`. Invalid commands and flags return an error and display usage. The command tree contains `serve`, `chat`, `conversations`, `models`, `providers`, `mcp`, `version`, and `update`.

`version`, `--version`, and `-v` print the executable's embedded build version without initializing providers. `update` checks GitHub for the newest stable release, downloads the matching asset and `checksums.txt`, verifies SHA-256, then replaces the current executable. Run it from a release build; development builds tagged `dev` are refused. Windows stages the checked executable and completes replacement after the current process exits. Updating may require write access to the current executable's directory.

`serve` starts the HTTP API using configured `HOST` and `PORT`; flags override them for that run. A non-loopback host still requires `API_TOKEN`. Runtime configuration loads when a command runs, not when help is requested.

`chat` invokes the shared service directly in the current process. Classic requests require a supported model ID with `--model`; auto and ensemble requests can omit it. `--task` provides a task hint, `--models` sets explicit ensemble candidates, and `--conversation` resumes local history. Prompt input can come from arguments or stdin. The command writes answer text only to stdout. Progress appears on stderr: interactive terminals show a monochrome spinner with routing/model stages, while redirected stderr receives plain progress lines. Progress never includes prompt or answer text. Errors are returned to stderr by the executable and use a nonzero exit code.

```bash
voie chat --strategy auto --task coding "Review this patch"
voie chat --strategy ensemble --models MODEL_A,MODEL_B "Compare these designs"
ID=$(voie conversations create)
voie chat --strategy auto --conversation "$ID" "Continue the discussion"
voie conversations list
voie conversations show "$ID"
voie conversations delete "$ID"
```

`conversations list` prints summaries without transcripts. `show` prints the stored transcript as JSON. Sessions are stored locally, expire after `CONVERSATION_TTL` of inactivity (default `720h`), and successful turns refresh expiry.

Explicit `--models` are the ensemble candidates; do not include the configured synthesis model in that list.

`models` lists the runtime catalogue in a table with model, owner, and provider columns, or as JSON with `--json`. `providers` lists provider metadata and performs HTTP URL reachability checks in a table with provider, label, status, and model-count columns; its JSON mode returns an object with `data`, `count`, and `timestamp`. A provider marked `alive` has responded to an HTTP GET; this does not guarantee inference works.

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
| `ROUTER_MODEL` | empty | Model used for ambiguous automatic routing and ensemble selection |
| `SYNTHESIS_MODEL` | empty | Ensemble synthesis model; defaults to `ROUTER_MODEL` |
| `ROUTING_CONFIG_PATH` | `~/.config/voie/routing.json` | Local routing policy file |
| `CONVERSATION_DB_PATH` | `~/.config/voie/conversations.db` | Local conversation database |
| `CONVERSATION_TTL` | `720h` | Conversation inactivity lifetime |

The MCP transport writes protocol messages to stdout. `voie mcp` sends application logs to stderr, including per-tool start, completion/failure, and duration records. Tool arguments, results, and conversation contents are not included in logs. All log output is monochrome.

## Troubleshooting

- If a model ID is rejected, run `voie models` and use an exact current ID.
- If a provider is unavailable, try another model from the runtime catalogue. Provider URL reachability does not confirm inference availability.
- Duck.ai chat requires Chrome or Chromium installed on the host.
- If `serve` rejects a public bind, set a nonempty `API_TOKEN` before starting it.
