# voie

![voie — free ai api](docs/voie-banner.png)

`voie` is a free AI API for local agents to discover and call supported models through one Go executable. It provides an OpenAI-compatible HTTP API, a local CLI, and an MCP server over stdio. Each interface uses the same provider registry, routing, validation, timeouts, and error handling.

Providers use public or reverse-engineered endpoints. Their availability can change. Duck.ai chat requires Chrome or Chromium on the host; the other modes and providers do not require a browser.

Public provider services receive the prompts sent to them; avoid sending secrets or confidential data unless you have reviewed that service's data practices. Some providers return complete responses rather than streaming tokens, so `voie` emits one final content chunk for those requests.

## Install and run

### Install a release

The installer scripts download the latest public release and verify its SHA-256 checksum before installing. They do not change your `PATH` or shell configuration. See [all releases](https://github.com/benoitpetit/voie/releases) for manual archive downloads.

**Linux** (amd64 or arm64):

```sh
curl -fsSL https://raw.githubusercontent.com/benoitpetit/voie/main/install.sh | sh
```

**macOS** (Intel amd64 or Apple silicon arm64):

```sh
curl -fsSL https://raw.githubusercontent.com/benoitpetit/voie/main/install.sh | sh
```

Both install `~/.local/bin/voie` by default. The installer does not add that directory to `PATH`; add it yourself if needed. To install a specific release or choose another directory:

```sh
curl -fsSLO https://raw.githubusercontent.com/benoitpetit/voie/main/install.sh
sh install.sh --version 0.0.3 --install-dir /your/bin
```

**Windows** (amd64, PowerShell):

```powershell
irm https://raw.githubusercontent.com/benoitpetit/voie/main/install.ps1 | iex
```

This installs `%LOCALAPPDATA%\Programs\voie\voie.exe`. The installer does not add that directory to `PATH`; add it yourself if needed. For an explicit version or directory:

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/benoitpetit/voie/main/install.ps1 -OutFile install.ps1
./install.ps1 -Version 0.0.3 -InstallDir C:\Tools\voie
```

The release archives are named `voie_VERSION_OS_ARCH.tar.gz` for Linux and macOS, and `voie_VERSION_windows_amd64.zip` for Windows.

### Build from source

```bash
go build -o voie .
./voie serve
```

The API listens on `127.0.0.1:8080` by default. See the [CLI guide](docs/cli.md) for commands, and run `./voie models` to discover the current model IDs.

### Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `HOST` | `127.0.0.1` | HTTP bind address |
| `PORT` | `8080` | HTTP bind port |
| `DEBUG` | `false` | Enable debug logging |
| `DEFAULT_PROVIDER` | empty | Provider used for the generic `openai` model in API requests |
| `TIMEOUT` | `120` | End-to-end completion timeout in seconds |
| `API_TOKEN` | empty | Bearer token required for non-loopback binds |
| `ROUTER_MODEL` | empty | Model that classifies ambiguous automatic-routing requests |
| `SYNTHESIS_MODEL` | empty | Model that merges ensemble answers; falls back to `ROUTER_MODEL` |
| `ROUTING_CONFIG_PATH` | `<user-config-dir>/voie/routing.json` | Local model descriptions and task rules |
| `CONVERSATION_DB_PATH` | `<user-config-dir>/voie/conversations.db` | Local SQLite conversation database |
| `CONVERSATION_TTL` | `720h` | Inactivity lifetime for local conversations |
| `FALLBACK_ENABLED` | `true` | Enable retries and fallback on provider failures |
| `FALLBACK_MAX_RETRIES` | `1` | Same-model retries for transient failures (0-3) |
| `FALLBACK_MAX_MODELS` | `1` | Extra models tried after the primary (0-3) |

On Linux, `<user-config-dir>` is typically `~/.config`; macOS and Windows use their standard per-user configuration directories. Set either path variable to choose another location.

The server refuses a non-loopback bind without `API_TOKEN`. Authenticated HTTP requests must include `Authorization: Bearer <token>`.

### Releases

Releases are published when a version tag is pushed. From a checkout of the commit to release, run:

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

The GitHub Actions workflow creates a GitHub Release with archives named `voie_VERSION_OS_ARCH.tar.gz` for Linux and macOS and `voie_VERSION_windows_amd64.zip` for Windows, plus `checksums.txt` (SHA-256). Linux and macOS builds are available for amd64 and arm64; Windows is currently available for amd64. The [v0.0.4 release](https://github.com/benoitpetit/voie/releases/tag/v0.0.4) contains these archives.

## Use the CLI

```bash
./voie --help
./voie version
./voie update
./voie chat --help
./voie completion --help
./voie models --json
./voie providers
./voie chat --model MODEL_ID "Summarize this text"
cat prompt.txt | ./voie chat --model MODEL_ID
```

The Cobra help shows every command and its available flags. Commands are `serve`, `chat`, `conversations`, `models`, `providers`, `mcp`, `completion`, `version`, and `update`; use `voie <command> --help` for command-specific options. `voie completion bash`, `zsh`, `fish`, or `powershell` generates shell completion scripts, including the supported `--task` values. `voie version` and the root aliases `voie --version` / `voie -v` print the build version. `voie update` checks the latest release and installs it when newer; it requires a tagged release build and permission to replace the current executable. Help works without loading provider configuration.

`chat` prints only the completion text to stdout. Errors go to stderr and return a nonzero exit status. Choose an exact ID from `voie models`; unknown IDs are rejected.

Automatic strategies are opt-in. `auto` uses task rules first and asks `ROUTER_MODEL` only when more than one eligible model remains. `ensemble` runs two or three models and synthesizes their answers. `--task` accepts `coding`, `reasoning`, `writing`, `translation`, `summarization`, or `general` when voie selects candidates automatically; the CLI offers these values as completions. Examples: `voie chat --strategy auto --task coding "Review this function"` and `voie chat --strategy ensemble --task reasoning "Compare these approaches"`. An explicit ensemble can use `voie chat --strategy ensemble --models model-a,model-b "Compare these approaches"`. A conversation can be resumed with `--conversation ID`; create/list/show/delete sessions with `voie conversations`. Requests sent to `auto` or `ensemble` providers disclose the prompt to those providers. See [routing and conversations](docs/architecture.md#routing-and-conversations).

## Use MCP

Configure your MCP client to launch the same executable with the `mcp` argument. Example client configuration:

```json
{
  "mcpServers": {
    "voie": {
      "command": "/absolute/path/to/voie",
      "args": ["mcp"]
    }
  }
}
```

The server provides `list_models`, `list_providers`, `chat_completion`, and conversation management tools. `chat_completion` supports classic, automatic, and ensemble strategies. See [docs/mcp.md](docs/mcp.md) for result shapes and setup details.

The portable Agent Skill is in [`skills/voie/SKILL.md`](skills/voie/SKILL.md). Copy the `skills/voie` folder into an agent's supported skills directory to install it.

## Use the HTTP API

```bash
curl http://127.0.0.1:8080/v1/models

curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"MODEL_ID","messages":[{"role":"user","content":"Hello"}]}'
```

Routes, request and response schemas, authentication, and streaming are documented in [api-reference.md](api-reference.md).

Automatic routing and ensembles are optional request strategies; requests that omit `strategy` retain the classic model/provider behavior. Local conversation tracking is opt-in via `conversation_id` and persists on disk with expiry.

`GET /v1/providers` reports `alive` based on whether an HTTP response was received from each provider URL. It does not test model inference or guarantee that a completion will succeed.

## Fallback and retries

Provider failures are classified as transient, unavailable, or permanent. Network errors and HTTP `408`, `429`, or `5xx` responses are transient and eligible for retries and fallback; a missing model is treated as unavailable and moves straight to fallback; other `4xx` errors are permanent and never retried. Retries and fallback happen only before the first streamed content chunk — once a stream has delivered content, a failure ends it.

A retry repeats the same model; fallback then tries other models. `FALLBACK_MAX_RETRIES` bounds same-model retries and `FALLBACK_MAX_MODELS` bounds how many other models are attempted (both default to `1`, with `0`–`3` valid). `0` disables that behavior and `FALLBACK_ENABLED=false` disables both. Per-model candidate lists live in `routing.json` under `fallback.models` and override the environment values:

```json
{
  "fallback": {
    "enabled": true,
    "max_retries": 1,
    "max_fallback_models": 1,
    "models": {
      "model-a": ["model-b", "model-c"]
    }
  }
}
```

Fallback candidates are chosen in this order: the request's explicit `fallback.models`, then the configured per-model list, then, for automatic and ensemble strategies, the other eligible models (limited by `max_fallback_models`). An explicit `provider` constraint is honored when candidates resolve. Ensemble candidates each get one attempt, exclude the synthesizer and router, and the synthesis step reuses any remaining fallback budget. An explicitly empty `fallback.models` list clears every candidate.

Each request can override the policy with a `fallback` object — HTTP and MCP accept `{"enabled": false, "max_retries": 0, "max_fallback_models": 0, "models": ["model-b"]}`, and the CLI exposes the same controls as `--fallback`, `--no-fallback`, `--fallback-retries N`, `--fallback-limit N`, and repeatable `--fallback-model ID`. Bound and ID validation happens before any provider is called, so an invalid override fails fast with an input error.

Responses include `routing.attempts`: one entry per provider call with the model, provider, attempt index, outcome, and duration in milliseconds. Outcomes are `succeeded`, `retryable_failure`, `unavailable`, and `failed`. Interface examples are in [api-reference.md](api-reference.md), [docs/cli.md](docs/cli.md), and [docs/mcp.md](docs/mcp.md).

## Progress and logs

- `voie chat` reports routing and model-call progress on stderr. Interactive terminals show a monochrome spinner; redirected output uses plain lines. Successful answer text remains the only chat output on stdout.
- The HTTP server logs each request, including authentication failures, with a request ID, method, path, status, and duration. Chat logs add the selected route or typed failure category.
- The MCP server logs each tool call to stderr with its tool name, status, and duration. stdout remains reserved for MCP protocol messages.
- Logs omit query values, headers, prompts, answers, request/response bodies, token counts, and client IPs. All logs and progress output are monochrome.

## Project guides

- [Architecture](docs/architecture.md): shared application service and transport adapters.
- [CLI](docs/cli.md): commands, flags, and output behavior.
- [MCP](docs/mcp.md): stdio setup, tools, schemas, and troubleshooting.
- [HTTP API reference](api-reference.md): endpoint contract and error handling.
