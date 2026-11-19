# Changelog

## v0.1.1 - 2026-11-20

### Added

- Four keyless OpenAI-compatible providers: Kilo Code, OVHcloud AI Endpoints, BlockRun and Pollinations.AI.
- A reusable keyless OpenAI-compatible client with SSE streaming, shared failure classification and support for optional local-gateway headers.
- An `opencode2api` provider that relays OpenCode Zen free models through an optional local gateway (`oc2a-*` model IDs, `OPENCODE2API_KEY`).
- Kilo's catalogue extended from the maintained `awesome-free-ai-coding` config (glyph cluster, beam, Ernie and LFM models).
- `scripts/sync-models`, a read-only drift checker between the maintained free-model config and the Go provider maps.

### Changed

- Registered providers: 7 → 12. Resolved model IDs: 56 → 90.
- `README.md` and `.env.example` document the new providers, the local gateway and its key.
- CI runs `go test -cover ./...`; provider test helpers are self-contained.
- Model IDs from the new providers are namespaced (`kilo-`, `ovh-`, `blockrun-`, `pollinations-`, `oc2a-`) to avoid routing collisions.

### Fixed

- Perplexity non-streaming completions now report the resolved model ID in responses.
- Providers no longer declare a static `Alive=true`; reachability is probed at runtime by `GET /v1/providers`.

## v0.1.0 - 2026-11-05

- Align Duck.ai browser capture with the CLI (rotation, stealth flags, redirect safety).
