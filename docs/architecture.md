# Architecture

`voie` is one Go program with four entry points: an HTTP API, a CLI, an MCP
stdio server, and an A2A endpoint. Every adapter calls the same application
service and provider registry, so routing, fallback, conversations, and error
handling are identical everywhere.

Every interface forwards the request to a third-party provider; there is no local inference and no local model weight.

```text
HTTP / CLI / MCP / A2A
        ↓
   app.Service
validate → route → timeout
        ↓
 app.Registry → provider
```

## Layout

| Package | Responsibility |
| --- | --- |
| `internal/app` | Provider contract, registry, strategies, validation, conversations, typed errors |
| `providers` | Upstream implementations and the default registry |
| `internal/runtime` | Configuration validation and service construction |
| `internal/transport/{httpapi,mcp,a2a}` | Protocol adapters |
| `internal/cli` | Command dispatch and output formatting |
| `internal/storage/sqlite` | Local conversation storage, quotas, and expiry |
| `config` | Environment parsing, defaults, and bind validation |

## Routing

`strategy` is `classic` when omitted. Classic routing uses an explicit model and
provider. `auto` filters by profile and picks the first eligible model in
deterministic order; a profile can set `selection: "router"` to use
`ROUTER_MODEL`. `ensemble` runs two to five agents and synthesizes their work
with `SYNTHESIS_MODEL` (falling back to `ROUTER_MODEL`). Modes are `independent`,
`chain`, and `debate`.

`ROUTING_CONFIG_PATH` points to optional JSON with model descriptors,
`profiles`, ensemble defaults, and a `fallback` policy. See
[configuration](configuration.md) for variables and
[CLI](cli.md#strategies) for request examples.

Every strategy shares one fallback policy. Network errors and `408`, `429`, or
`5xx` responses retry the same model, then fall back; a missing model goes
straight to fallback; other `4xx` responses stop. A request-level list is
validated before routing. An explicit request list replaces the configured and
inferred pools; otherwise the configured per-model list applies, then the
eligible provider pool. Responses record every provider call in `attempts`.

## Conversations

Conversation state is opt-in through `conversation_id` and stored locally in
`CONVERSATION_DB_PATH`. The default TTL is `720h` of inactivity; successful
turns refresh it, reads do not. Expired transcripts are deleted and an ID-only
tombstone remains for 30 days. Conversations cap at 200 messages and 2 MiB, and
a stale concurrent turn returns `409`.

In a hosted deployment the Worker supplies an opaque per-key conversation
scope. `REQUIRE_CONVERSATION_SCOPE=true` rejects `/v1/*` requests without it;
project-level rate limiting stays in the Worker. The local binary needs no API
key or scope.

## Security

The HTTP listener defaults to loopback. Any other bind requires `API_TOKEN`, and
protected routes require its bearer token. Chat bodies are capped at 2 MiB;
response writes stay unbounded for SSE. MCP uses local stdio and opens no
network listener. `healthcheck` probes the local health route.

## A2A

A2A is a thin fourth adapter. `internal/transport/a2a` serves JSON-RPC
`SendMessage`, `SendStreamingMessage`, task methods, and the extended card at
`/a2a`, and publishes `/.well-known/agent-card.json` publicly while the RPC
endpoint keeps the HTTP bearer policy. Inbound tasks map `contextId` to voie
conversations.

Outbound, `providers/a2a.go` turns a discovered card's skills into ordinary
provider models. It propagates the active conversation as `contextId`, so a
remote voie agent resumes the same local conversation. No routing, fallback, or
conversation logic is duplicated.
