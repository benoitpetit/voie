# Changelog

## Unreleased

### Added

- Optional `pplx-proxy` gateway for Perplexity with automatic session-cookie capture during `voie connect` and an actionable error when the cookie expires.

- Free/paid model tiers: `voie models` shows `TIER` and `PRICE` columns with `--free`, `--paid`, `--all` and `--provider` filters; `GET /v1/models?tier=&provider=` and MCP `list_models` carry the same fields.
- Hidden paid models from gateway allowlists, the free-model catalogue and Perplexity are now callable and listed as `paid`. Public per-token prices are fetched from OpenRouter when available; otherwise paid models show `price unknown`.

### Fixed

- HTTP: unmatched routes return 404, root/health reject non-GET, and the scope header is validated and applied in every auth mode.
- Streaming: the resolved upstream model and the final `routing.attempts` are now emitted; `stream_options.include_usage` adds an estimated usage chunk.
- ACP bridges: each agent uses its own catalogue, no Cline-only flags, per-request model pinning, trailing chunks are kept, loopback ports are identity-checked, and cancellation is prompt.
- Gateway refresh, npm/CLIProxyAPI installer checksums, model ownership/prefix collisions, Docker token default, and CLI usage/error streams.
- Docs contract now derives provider counts, env vars and CI requirements from code; chain/debate, conversation CRUD, store, config and updater paths gained regression tests.

### Added

- 16 token-based OpenAI-compatible providers (OpenRouter, OrcaRouter, Groq, Google AI Studio, NVIDIA NIM, Z.ai, Mistral, Cloudflare Workers AI, Cohere trial, Requesty, AIHubMix, AnyAPI, BeatAPI, iFlytek Spark, VLM Run, Runcrate) declared in one catalogue.
- Six ACP providers over official coding-agent CLIs (CodeBuddy, MiniMax Code, Devin, Qoder, Copilot CLI, Cursor) using the generalized in-process bridge, plus declarative specs for 17 opt-in third-party gateways.
- Model IDs carry a provider prefix and list their provider in `voie models`, `GET /v1/models` and MCP `list_models`.

- `voie connect` stores provider tokens in a `0600` credential file with environment overrides; unconfigured providers are hidden from the catalogue and routing. Connecting Antigravity, Kiro or Cline installs their missing toolchain automatically; `voie install <provider>` does the same explicitly.
- OpenRouter and OrcaRouter providers with stable free-model aliases (`orfree`, `orcafree`).
- Local gateway providers for Antigravity (CLIProxyAPI), Kiro (official-CLI ACP bridge), and Cline (built-in ACP bridge that denies agent tools). Gateways start automatically and stop with the runtime.

### Changed

- Removed the Cohere provider: its HuggingFace Space now requires sign-in, so the keyless endpoint can no longer serve requests.
- Gateway installers now use `uv` instead of `pip` for Python packages, run entirely inside the gateway directory (install scripts can no longer overwrite files in the caller's repository), and generate secrets before dependency steps that need them.
- Fixed a deadlock that made `voie connect` hang forever when a gateway never became healthy; it now reports the timeout and exits.
- Fixed startup paths for `free-llm-router` (local binary plus explicit port), `windsurfapi` (no root needed), `freellmapi` (generated `ENCRYPTION_KEY`), `workbuddy2api` (uv CLI) and Ollama's default alias (model present on the machine).
- Only verified free models are listed: gateways whose lane is keyless/local keep their live discovery, while Qoder/Trae expose only their documented free `Auto` mode and 9routerx stays limited to its working free alias. Renamed `free-llm-router` models to `flr-` and `trae2api` models to `te-`.
- Reversed Perplexity's current web API: the ask payload is now wrapped in `params` with a new request reason and client hints, and answers arrive in nested `text_payload` workflow blocks. Only the anonymous `turbo` model is advertised; an optional `VOIE_PERPLEXITY_COOKIES` makes it reliable when anonymous traffic is throttled.
- `voie connect` now keeps dashboard gateways running until the user validates in the terminal: it prints the local dashboard URL, waits for Enter, then stops the gateway. Gateway specs declare their dashboard URL. Copilot API and Cursor ACP Proxy offer their CLI login (`auth` / `login`) during connect instead.
- FreeLLMAPI's first-run setup code is extracted from the container logs and printed during `voie connect`, so the first account can be created from a browser outside the container.
- `voie connect --list` and the interactive `voie connect` menu share the same flat `NAME / TYPE / STATE` layout (the menu adds a leading `#` column for selection). Types: `token`, `cli`, `keyless gateway`, `dashboard gateway`, `byok gateway`, `local runtime`. `voie providers` reports installed gateways as `ready` even while their loopback listener is stopped, since they start on demand.
- `voie models` now prints `PROVIDER` before `MODEL`.

## v0.1.4 - 2026-11-22

### Changed

- Hosted conversation history is isolated by the individual API key that created it. Keys for the same repository do not share transcripts; edge rate limiting remains repository-scoped.
- Hosted `/v1/*` requests require the trusted conversation scope supplied by the Worker. Local binary conversations keep their single-user namespace and do not require a hosted key.
- Conversations created under the former shared project scope are not reassigned to API keys. They are inaccessible through the hosted API and expire through normal cleanup.

## v0.1.3 - 2026-11-21

### Added

- Configurable ensemble strategies, including independent answers, review chains and multi-round debates.
- Optional Coolify deployment and the `voie-ai` npm installer for the CLI binary.

## v0.1.2 - 2026-11-20

### Changed

- README and website now describe voie as an API relay with HTTP, CLI and MCP surfaces, without centering the presentation on agents.
- Provider and model names stay out of READMEs, release notes and the website.

## v0.1.1 - 2026-11-20

### Added

- Four keyless OpenAI-compatible providers, plus a shared client for anonymous upstreams with SSE, classified failures and optional local-gateway headers.
- Two local-gateway relays: one for a free catalogue and one for a subscription-backed catalogue. Both need no key from callers; the subscription relay requires a credential (API key or device OAuth) configured in the gateway.
- A read-only drift checker between the maintained free-model configuration and the Go provider maps.
- Extended free-model catalogue synchronised from the maintained upstream configuration.

### Changed

- Registered providers: 7 → 13. Resolved model IDs: 56 → 110.
- Documentation explains the local gateway, its loopback key and the two credential options; READMEs keep describing providers generically without naming them.
- CI runs `go test ./...` with coverage; provider test helpers are self-contained.
- Model IDs from the new providers are namespaced to avoid routing collisions.

### Fixed

- One non-streaming adapter now reports the resolved model ID in responses.
- Providers no longer declare a static `Alive=true`; reachability is probed at runtime by `GET /v1/providers`.

## v0.1.0 - 2026-11-05

- Align browser-backed request capture with the CLI (rotation, stealth flags, redirect safety).
