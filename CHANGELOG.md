# Changelog

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
