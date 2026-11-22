# voie

![voie free ai api](docs/voie-banner.png)

`voie` is a free, OpenAI-compatible API relay. One Go executable relays requests to public model providers and exposes the same provider registry through an HTTP API, a CLI, and an MCP server over stdio. Those interfaces share provider selection, routing, validation, timeouts, and error handling.

voie does not run models. Inference stays on the public services it forwards to. The executable downloads and runs nothing else, apart from an optional SQLite conversation database.

Point an OpenAI SDK at `http://127.0.0.1:8080/v1` and call any ID returned by `voie models`. No account and no API key. Model IDs are runtime data; read them with `voie models --json` or `GET /v1/models` against a running server.

The registry has thirteen providers: seven reverse-engineered public endpoints, four keyless OpenAI-compatible APIs, and two relays through an optional local gateway (`oc2a-*` for a free catalogue and `oc2ago-*` for a subscription-backed catalogue). Availability can change without notice, and providers may rate-limit or block automated access. Some log prompts upstream; others state that they do not use data for training. Browser-backed chat needs Chrome or Chromium on the host; nothing else does.

The two gateway-backed providers need a local OpenAI-compatible gateway at `http://127.0.0.1:8090`. Its loopback key is set in the gateway (default `voie-local`) and mirrored in voie's runtime configuration; callers still supply no key. Free IDs are capped by the upstream daily quota. Go IDs (`oc2ago-*`) need a Go API key or a Go OAuth connection in the gateway; see [Local gateway setup](docs/local-gateway.md). Those models are included in a subscription.

Public services receive the prompts you send, so keep secrets out of requests unless you have checked the service's data practices. Some providers return a complete answer instead of streaming tokens; voie emits one final content chunk for those requests.

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
sh install.sh --version 0.1.4 --install-dir /your/bin
```

**Windows** (amd64, PowerShell):

```powershell
irm https://raw.githubusercontent.com/benoitpetit/voie/main/install.ps1 | iex
```

This installs `%LOCALAPPDATA%\Programs\voie\voie.exe`. The installer does not add that directory to `PATH`; add it yourself if needed. For an explicit version or directory:

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/benoitpetit/voie/main/install.ps1 -OutFile install.ps1
./install.ps1 -Version 0.1.4 -InstallDir C:\Tools\voie
```

The release archives are named `voie_VERSION_OS_ARCH.tar.gz` for Linux and macOS, and `voie_VERSION_windows_amd64.zip` for Windows.

### Install with npm

Requires Node.js 18 or newer. The [voie-ai package](https://www.npmjs.com/package/voie-ai) fetches the same release binary for your platform.

Install globally, list the available models, then start the local API:

```sh
npm install -g voie-ai
voie models --json
voie serve
```

To run the CLI without a global install, use `npx`:

```sh
npx voie-ai models --json
npx voie-ai serve
```

The API listens on `127.0.0.1:8080` by default. `npx` runs the package for each command without adding the CLI to your `PATH`.

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
| `REQUIRE_CONVERSATION_SCOPE` | `false` | Require the trusted conversation scope header on `/v1/*`; requires `API_TOKEN` |
| `ROUTER_MODEL` | empty | Optional model used by profiles with `selection: "router"`; also the ensemble synthesizer fallback |
| `SYNTHESIS_MODEL` | empty | Model that merges ensemble answers; falls back to `ROUTER_MODEL` |
| `ROUTING_CONFIG_PATH` | `<user-config-dir>/voie/routing.json` | Local model descriptions, routing profiles, ensemble defaults, and fallback rules |
| `CONVERSATION_DB_PATH` | `<user-config-dir>/voie/conversations.db` | Local SQLite conversation database |
| `CONVERSATION_TTL` | `720h` | Inactivity lifetime for local conversations |
| `FALLBACK_ENABLED` | `true` | Enable retries and fallback on provider failures |
| `FALLBACK_MAX_RETRIES` | `1` | Same-model retries for transient failures (0-3) |
| `FALLBACK_MAX_MODELS` | `1` | Extra models tried after the primary (0-3) |
| `OPENCODE_ZEN_KEY` | `voie-local` | Local opencode gateway key, shared by `opencode-zen` and `opencode-go` |
| `VOIE_OPENROUTER_KEY` | saved token | OpenRouter API key (`sk-or-v1-...`) |
| `VOIE_ORCAROUTER_KEY` | saved token | OrcaRouter API key (`sk-orca-...`) |
| `VOIE_ANTIGRAVITY_KEY` | `voie-local` | Local CLIProxyAPI gateway key |
| `VOIE_KIRO_KEY` | `voie-local` | Local Kiro ACP gateway key |
| `VOIE_CLINE_KEY` | `voie-local` | Local Cline ACP bridge key |
| `VOIE_GROQ_KEY` | saved token | Groq API token |
| `VOIE_GOOGLE_AI_STUDIO_KEY` | saved token | Google AI Studio token |
| `VOIE_NVIDIA_NIM_KEY` | saved token | NVIDIA NIM token |
| `VOIE_ZAI_KEY` | saved token | Z.ai GLM token |
| `VOIE_MISTRAL_KEY` | saved token | Mistral token |
| `VOIE_CLOUDFLARE_AI_KEY` | saved token | Cloudflare Workers AI token |
| `VOIE_CLOUDFLARE_ACCOUNT_ID` | `ACCOUNT_ID` | Cloudflare account used in the Workers AI URL |
| `VOIE_COHERE_TRIAL_KEY` | saved token | Cohere trial API key |
| `VOIE_REQUESTY_KEY` | saved token | Requesty token |
| `VOIE_AIHUBMIX_KEY` | saved token | AIHubMix token |

On Linux, `<user-config-dir>` is typically `~/.config`; macOS and Windows use their standard per-user configuration directories. Set either path variable to choose another location.

The server refuses a non-loopback bind without `API_TOKEN`. Authenticated HTTP requests must include `Authorization: Bearer <token>`. Hosted deployments should set `REQUIRE_CONVERSATION_SCOPE=true`; this makes `/v1/*` reject requests without the opaque conversation scope supplied by the trusted Worker. The setting is off by default, so the local binary does not need a hosted API key or scope.

### Connect a provider

Nine hosted APIs are available by token: Groq, Google AI Studio, NVIDIA NIM, Z.ai, Mistral, Cloudflare Workers AI (10,000 neurons/day free), Cohere trial, Requesty and AIHubMix. Six coding agents are available through their official ACP interface: CodeBuddy, MiniMax Code, Devin, Qoder, GitHub Copilot CLI and Cursor. Eight third-party reverse-engineered gateways (OmniRoute, Qoder2API, WindsurfAPI, Copilot API, Cursor ACP Proxy, CodeArts2API, CLI2API, WorkBuddy2API) are declared as opt-in providers: they are hidden until you install them yourself and carry ToS/account risk. `voie install <provider>` prints the exact install command for those gateways.

`voie models`, `GET /v1/models` and the MCP `list_models` tool group models by provider label, so the catalogue stays readable as it grows.

`voie connect` is the single entry point for providers that accept a token. Run it with no argument for an interactive menu, or `voie connect <provider> --token <value>` for scripts. Tokens are stored in `<user-config-dir>/voie/credentials.json` with `0600` permissions; the environment variables above override the saved value. `voie connect --list` shows which providers are connected, and `voie connect disconnect <provider>` removes one. A provider that needs a token and has none is absent from `voie models` and from automatic routing.

OpenRouter (`orfree`) and OrcaRouter (`orcafree`) use their hosted OpenAI-compatible APIs with a token. The other local providers start their gateway automatically on first use: `opencode-zen`/`opencode-go` on `127.0.0.1:8090`, Antigravity through a user-installed `CLIProxyAPI` on `:8317`, Kiro through the official `kiro-cli` ACP bridge on `:8091`, and Cline through voie's built-in `cline-bridge` on `:8092`. `voie connect antigravity`, `voie connect kiro` and `voie connect cline` install the required toolchain automatically on first use (GitHub release for CLIProxyAPI, npm for the CLIs, git clone of the ACP bridge for Kiro). `voie install <provider>` does the same without connecting. Each still needs a one-time upstream login (`CLIProxyAPI -antigravity-login` / `kiro-cli login`); Cline requires `cline auth`. Their free-model lists rotate, so prefer the stable aliases and check the upstream catalog before relying on a specific ID.

Routing personal Antigravity or Kiro accounts through a third-party gateway may violate those products' terms and has led to account suspensions. Use accounts you can afford to lose and read the provider terms before connecting them.

### Releases

Releases are published when a version tag is pushed. From a checkout of the commit to release, run:

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

The GitHub Actions workflow creates a GitHub Release with archives named `voie_VERSION_OS_ARCH.tar.gz` for Linux and macOS and `voie_VERSION_windows_amd64.zip` for Windows, plus `checksums.txt` (SHA-256). Linux and macOS builds are available for amd64 and arm64; Windows is currently available for amd64. The [v0.1.4 release](https://github.com/benoitpetit/voie/releases/tag/v0.1.4) contains these archives.

## Use the CLI

```bash
./voie --help
./voie version
./voie update
./voie chat --help
./voie completion --help
./voie models --json
./voie providers
./voie healthcheck
./voie chat --model MODEL_ID "Summarize this text"
cat prompt.txt | ./voie chat --model MODEL_ID
```

The Cobra help shows every command and its available flags. Commands are `serve`, `chat`, `conversations`, `models`, `providers`, `healthcheck`, `mcp`, `connect`, `install`, `completion`, `version`, and `update`; use `voie <command> --help` for command-specific options. `voie completion bash`, `zsh`, `fish`, or `powershell` generates shell completion scripts; `--task` completion uses configured routing profile names. `voie version` and the root aliases `voie --version` / `voie -v` print the build version. `voie update` checks the latest release and installs it when newer; it requires a tagged release build and permission to replace the current executable. Help works without loading provider configuration.

`chat` prints only the completion text to stdout. Errors go to stderr and return a nonzero exit status. Choose an exact ID from `voie models`; unknown IDs are rejected.

Automatic strategies are opt-in. `auto` picks the first eligible model from a routing profile in deterministic order; a profile can opt into `ROUTER_MODEL`. Profiles and capability labels are defined in `routing.json`. `ensemble` supports independent answers, a sequential review chain, and a multi-round debate before synthesis. Configure defaults in `routing.json`; per request, `--candidate-count` chooses 2–5 agents and repeatable `--ensemble-model` flags select their exact models. Examples: `voie chat --strategy auto --task coding "Review this function"`, `voie chat --strategy ensemble --ensemble-mode chain --candidate-count 3 "Review this proposal"`, and `voie chat --strategy ensemble --ensemble-mode debate --debate-turns 2 "Compare these approaches"`. A conversation can be resumed with `--conversation ID`; create/list/show/delete sessions with `voie conversations`. Requests sent to `auto` or `ensemble` providers disclose the prompt to those providers. See [routing and conversations](docs/architecture.md#routing-and-conversations).

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

Provider failures are classified as transient, unavailable, or permanent. Network errors and HTTP `408`, `429`, or `5xx` responses are transient and eligible for retries and fallback; a missing model is treated as unavailable and moves straight to fallback; other `4xx` errors are permanent and never retried. Retries and fallback happen only before the first streamed content chunk. Once a stream has delivered content, a failure ends it.

A retry repeats the same model; fallback then tries other models. `FALLBACK_MAX_RETRIES` bounds same-model retries and `FALLBACK_MAX_MODELS` bounds how many other models are attempted (both default to `1`, with values from 0 to 3). `0` disables that behavior and `FALLBACK_ENABLED=false` disables both. Per-model candidate lists live in `routing.json` under `fallback.models` and override the environment values:

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

Fallback candidates are chosen in this order: the request's explicit `fallback.models`, then the configured per-model list, then the other eligible models (limited by `max_fallback_models`). An explicit `provider` constraint is honored when candidates resolve. Failed ensemble candidates can be replaced; the synthesis step reuses any remaining fallback budget. An explicitly empty `fallback.models` list clears every candidate.

Each request can override the policy with a `fallback` object. HTTP and MCP accept `{"enabled": false, "max_retries": 0, "max_fallback_models": 0, "models": ["model-b"]}`, and the CLI exposes the same controls as `--fallback`, `--no-fallback`, `--fallback-retries N`, `--fallback-limit N`, and repeatable `--fallback-model ID`. Bound and ID validation happens before any provider is called, so an invalid override fails fast with an input error.

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
- [Model catalogue sync](scripts/sync-models): compare the keyless model maps with the maintained free-model config; run `scripts/sync-models --help`.
