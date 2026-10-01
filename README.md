# voie

![voie — free ai api](docs/voie-banner.png)

`voie` is a free AI API for local agents to discover and call supported models through one Go executable. It provides an OpenAI-compatible HTTP API, a local CLI, and an MCP server over stdio. Each interface uses the same provider registry, routing, validation, timeouts, and error handling.

Providers use public or reverse-engineered endpoints. Their availability can change. Duck.ai chat requires Chrome or Chromium on the host; the other modes and providers do not require a browser.

ChatJimmy is available without an account or API key and currently lists `llama3.1-8B`. Its upstream API returns complete responses, so `voie` emits a single final chunk when local streaming is requested. Public provider services receive the prompts sent to them; avoid sending secrets or confidential data unless you have reviewed that service's data practices.

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
sh install.sh --version 0.0.1 --install-dir /your/bin
```

**Windows** (amd64, PowerShell):

```powershell
irm https://raw.githubusercontent.com/benoitpetit/voie/main/install.ps1 | iex
```

This installs `%LOCALAPPDATA%\Programs\voie\voie.exe`. The installer does not add that directory to `PATH`; add it yourself if needed. For an explicit version or directory:

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/benoitpetit/voie/main/install.ps1 -OutFile install.ps1
./install.ps1 -Version 0.0.1 -InstallDir C:\Tools\voie
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

The server refuses a non-loopback bind without `API_TOKEN`. Authenticated HTTP requests must include `Authorization: Bearer <token>`.

### Releases

Releases are published when a version tag is pushed. From a checkout of the commit to release, run:

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

The GitHub Actions workflow creates a GitHub Release with archives named `voie_VERSION_OS_ARCH.tar.gz` for Linux and macOS and `voie_VERSION_windows_amd64.zip` for Windows, plus `checksums.txt` (SHA-256). Linux and macOS builds are available for amd64 and arm64; Windows is currently available for amd64. The [v0.0.1 release](https://github.com/benoitpetit/voie/releases/tag/v0.0.1) contains these archives.

## Use the CLI

```bash
./voie --help
./voie version
./voie update
./voie chat --help
./voie models --json
./voie providers
./voie chat --model MODEL_ID "Summarize this text"
./voie chat --provider jimmy --model llama3.1-8B "Bonjour"
cat prompt.txt | ./voie chat --model MODEL_ID
```

The Cobra help shows every command and its available flags. Commands are `serve`, `chat`, `models`, `providers`, `mcp`, `version`, and `update`; use `voie <command> --help` for command-specific options. `voie version` and the root aliases `voie --version` / `voie -v` print the build version. `voie update` checks the latest release and installs it when newer; it requires a tagged release build and permission to replace the current executable. Help works without loading provider configuration.

`chat` prints only the completion text to stdout. Errors go to stderr and return a nonzero exit status. Choose an exact ID from `voie models`; unknown IDs are rejected.

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

The server provides `list_models`, `list_providers`, and `chat_completion`. The completion tool expects an explicit model ID and a `messages` array. See [docs/mcp.md](docs/mcp.md) for result shapes and setup details.

The portable Agent Skill is in [`skills/voie/SKILL.md`](skills/voie/SKILL.md). Copy the `skills/voie` folder into an agent's supported skills directory to install it.

## Use the HTTP API

```bash
curl http://127.0.0.1:8080/v1/models

curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"MODEL_ID","messages":[{"role":"user","content":"Hello"}]}'
```

Routes, request and response schemas, authentication, and streaming are documented in [api-reference.md](api-reference.md).

`GET /v1/providers` reports `alive` based on whether an HTTP response was received from each provider URL. It does not test model inference or guarantee that a completion will succeed.

## Project guides

- [Architecture](docs/architecture.md): shared application service and transport adapters.
- [CLI](docs/cli.md): commands, flags, and output behavior.
- [MCP](docs/mcp.md): stdio setup, tools, schemas, and troubleshooting.
- [HTTP API reference](api-reference.md): endpoint contract and error handling.
