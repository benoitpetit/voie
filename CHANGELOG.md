# Changelog

## Unreleased

### Added

- MCP progress notifications with `progressToken`, shared `request_id` correlation across surfaces, and outbound A2A `contextId` propagation via the request context.

- A2A 1.0 server and client: public agent card with voie skills, JSON-RPC chat/task methods and streaming; named outbound agents exposed as `a2a-<agent>-<skill>` models; CLI `--stream`, MCP `stream`, shared model/error shapes; docs and skill updated.

- Anticipatory tests for classic, auto and ensemble (independent/chain/debate) across service streaming, CLI, HTTP and MCP, plus docs contract coverage for strategies and fallback.

- OrcaRouter exposes its authenticated model catalogue as `paid` with real per-token prices; unpriced aliases stay free. ACP subscription models now show `subscribe` instead of `price unknown`.

- ACP bridges discover the agent's real model catalogue during the handshake, cache it, and expose it in `voie models`; model switching falls back to `session/set_config_option` when needed.

- OpenRouter paid models from the public catalogue, with real per-token prices; free aliases keep `USD 0/0`.

- Optional `pplx-proxy` gateway for Perplexity with automatic session-cookie capture during `voie connect` and an actionable error when the cookie expires.

- Free/paid model tiers: `voie models` shows `TIER` and `PRICE` columns with `--free`, `--paid`, `--all` and `--provider` filters; `GET /v1/models?tier=&provider=` and MCP `list_models` carry the same fields.
- Hidden paid models from gateway allowlists, the free-model catalogue and Perplexity are now callable and listed as `paid`. Public per-token prices are fetched from OpenRouter when available; otherwise paid models show `price unknown`.

### Fixed

- Final A2A/surface alignment: stale agent cards refresh after 24 h, non-text parts get an explicit error, tasks are owner-scoped with cursor pagination, MCP gains a health tool and conversation pagination, and `ModelInfo.provider` is shared across CLI/HTTP/MCP.

- Final audit follow-ups: fallback docs describe explicit lists as replacements, CLI rejects invalid strategies and single legacy ensemble models early, and typed upstream failures carry a static reason without leaking response bodies.

- Execution audit: fallback flags always take effect, explicit fallback lists win in auto/ensemble, typed deadlines and malformed responses are classified coherently, router failures fall back to local selection, ensemble synthesis keeps its own budget, and provider pins are enforced in both loops.

- `voie install` fills the `ACCOUNT` column for CLI agents (`subscription`) and APIs (`token`, or `none` for anonymous lanes) instead of leaving it blank.

- `voie install` reports a failed saved session as `error` with `voie connect <name>` as the next step, instead of hiding it behind `connected`/`installed`.

- `voie install` marks installed keyless gateways and npm-installed agent CLIs as ready (`NONE`) instead of asking for a needless connect/install step.

- Free models from authenticated catalogues (OrcaRouter) display `USD 0/0` even when upstream spells zero as `0.0000000000` or leaves it empty.

- ACP bridges resolve npm-installed CLIs from `node_modules/.bin` and surface the agent child process diagnostics when a handshake fails.

- `voie install` reports anonymous lanes (vlm-run, opencode) as available, matching `voie connect --list`.

- Every free model displays `USD 0/0`; public upstream prices replace it when available.

- Catalogue coherence: prices are looked up by upstream ID with failure backoff; public prefixes now select the owning provider; empty vs absent free lists, case-insensitive allowlists and subscription lanes are classified correctly; failed gateway probes keep the last known catalogue; provider metadata uses public model IDs on all surfaces; MCP `list_providers` exposes full metadata; declared aliases keep their names; `--all` conflicts and unknown provider filters are validated.

- pplx-proxy: only the `auto` lane is marked free; subscription models are paid and upstream 403 messages are surfaced.

- FreeLLM Pool listings now expose only routing aliases: concrete upstream IDs advertised by the gateway were frequently stale (HTTP 404 or cooldown). Mixed-case gateway model IDs also resolve correctly.

- HTTP: unmatched routes return 404, root/health reject non-GET, and the scope header is validated and applied in every auth mode.
- Streaming: the resolved upstream model and the final `routing.attempts` are now emitted; `stream_options.include_usage` adds an estimated usage chunk.
- ACP bridges: each agent uses its own catalogue, no Cline-only flags, per-request model pinning, trailing chunks are kept, loopback ports are identity-checked, and cancellation is prompt.
- Gateway refresh, npm/CLIProxyAPI installer checksums, model ownership/prefix collisions, Docker token default, and CLI usage/error streams.
- Docs contract now derives provider counts, env vars and CI requirements from code; chain/debate, conversation CRUD, store, config and updater paths gained regression tests.

### Added

- 16 token-based OpenAI-compatible providers (OpenRouter, OrcaRouter, Groq, Google AI Studio, NVIDIA NIM, Z.ai, Mistral, Cloudflare Workers AI, Cohere trial, Requesty, AIHubMix, AnyAPI, BeatAPI, iFlytek Spark, VLM Run, Runcrate) declared in one catalogue.
- Six ACP providers over official coding-agent CLIs (CodeBuddy, MiniMax Code, Devin, Qoder, Copilot CLI, Cursor) using the generalized in-process bridge, plus declarative specs for 16 opt-in third-party gateways.
- Model IDs carry a provider prefix and list their provider in `voie models`, `GET /v1/models` and MCP `list_models`.

- `voie connect` stores provider tokens in a `0600` credential file with environment overrides; unconfigured providers are hidden from the catalogue and routing. Connecting Antigravity, Kiro or Cline installs their missing toolchain automatically; `voie install <provider>` does the same explicitly.
- OpenRouter and OrcaRouter providers with stable free-model aliases (`orfree`, `orcafree`).
- Local gateway providers for Antigravity (CLIProxyAPI), Kiro (official-CLI ACP bridge), and Cline (built-in ACP bridge that denies agent tools). Gateways start automatically and stop with the runtime.

### Changed

- `voie chat --help` now documents classic, auto and ensemble with dedicated examples, and `--strategy <name> --help` shows only that strategy.

- Compact `voie install` list shows `NONE` in `NEXT` for providers that are already connected.

- Removed the Cohere provider: its HuggingFace Space now requires sign-in, so the keyless endpoint can no longer serve requests.
- Gateway installers now use `uv` instead of `pip` for Python packages, run entirely inside the gateway directory (install scripts can no longer overwrite files in the caller's repository), and generate secrets before dependency steps that need them.
- Fixed a deadlock that made `voie connect` hang forever when a gateway never became healthy; it now reports the timeout and exits.
- Fixed startup paths for `free-llm-router` (local binary plus explicit port), `windsurfapi` (no root needed), `freellmapi` (generated `ENCRYPTION_KEY`), `workbuddy2api` (uv CLI) and Ollama's default alias (model present on the machine).
- Only verified free models are listed: gateways whose lane is keyless/local keep their live discovery, while Qoder exposes only its documented free `Auto` mode and 9routerx stays limited to its working free alias. Renamed `free-llm-router` models to `flr-`.
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

### Removed

- The `trae2api` reverse-proxy gateway and all Trae-specific model IDs; Trae is no longer a voie provider.
