---
name: voie
description: Use when an agent needs to install voie or discover and call a supported model through voie MCP, CLI, or its OpenAI-compatible HTTP API.
---

# voie

Use the `voie` runtime catalogue to choose a model, then call it through the interface available in the current environment.

Some public providers, including ChatJimmy, do not require an account or API key. They send prompts to a third-party service; avoid confidential data unless its handling has been reviewed. Discover model IDs at runtime because upstream catalogues and availability can change.

## Workflow

1. Prefer the configured `voie` MCP server when available. Call `list_models` and select an exact model ID from its current results.
2. Call `chat_completion` with that explicit `model` and a `messages` array. Set `provider` only when the user asks for a provider or routing needs to be constrained.
3. If MCP is unavailable and the local executable is available, run `voie models --json`, then `voie chat --model MODEL_ID "PROMPT"`.
4. If only the HTTP API is available, call `GET /v1/models`, then `POST /v1/chat/completions` with the exact model ID and messages. Send the configured bearer token when the API requires authentication.

## Install the CLI

Treat installation as separate from making a model call. Do not install the CLI just because the MCP server and executable are unavailable; use a configured HTTP API or ask whether the user wants installation help. When installation is requested, use the system installer documented in the [README installation guide](https://github.com/benoitpetit/voie#install-a-release). The scripts install the latest public release and verify its SHA-256 checksum. Linux and macOS support amd64/arm64; Windows supports amd64. They leave `PATH` configuration to the user.

## CLI version and updates

Run `voie version` or `voie --version` to read the embedded version. Run `voie update` from a tagged release build to check for and install a newer release. It refuses development builds, verifies the matching release checksum, and may need write access to the executable's directory. On Windows the replacement completes after the process exits.

## Recover from errors

- For an unknown model, refresh the catalogue and retry with an exact current ID. Do not substitute a guessed alias.
- For a provider/model mismatch, remove the explicit provider or choose a model listed for that provider.
- For a timeout or upstream failure, call `list_providers` for reachability context and retry with another listed model when appropriate.
- `alive: true` means the provider URL returned an HTTP response. It does not confirm model inference works.

Use `models --json` or MCP `list_models` for discovery. Do not keep a separate hard-coded model list in prompts or integrations.
