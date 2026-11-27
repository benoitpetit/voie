# MCP guide

`voie mcp` runs an MCP server over stdin/stdout. stdout carries only protocol
messages; logs go to stderr. The same executable also provides the CLI and HTTP
API, and no HTTP server is needed for local MCP calls.

Every interface forwards the request to a third-party provider; there is no local inference and no local model weight.

## Client setup

Use the executable's absolute path:

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

Install a release with the [installation guide](install.md) or build with
`go build -o voie .`.

## Tools

### `list_models`

Takes optional `tier` (`free` or `paid`) and `provider` arguments. Results
include `models[]` with `id`, `provider`, `owned_by`, `tier`, optional `group`,
and an optional `price` object; a `groups[]` array groups the catalogue. Paid
models without a public price report `price unknown`.

Every `free` model exposes `USD 0/0`; ACP-backed CLI providers run on your
subscription and show `subscribe`. Public prices come from OpenRouter when
available. Use runtime discovery instead of a fixed list.

### `list_providers`

Takes an empty object. Returns names, labels, `alive`, `default_model`,
`free_models`, `supported_models`, and other metadata. `free_models` is always
present: a non-empty array lists verified free IDs, an empty array means none
are free. `alive` means the provider URL returned an HTTP response; it does not
confirm inference.

### `chat_completion`

Requires `messages`. Classic requests require `model`; `auto` and `ensemble` can
omit it. Optional fields: `provider`, `strategy`, `task`, `models`, `ensemble`,
`fallback`, `conversation_id`, and `stream`.

```json
{
  "model": "MODEL_ID_FROM_LIST_MODELS",
  "messages": [{"role": "user", "content": "Hello"}]
}
```

`auto` selects one model from a routing profile. `ensemble` combines agents:

```json
{"strategy":"ensemble","task":"reasoning","ensemble":{"mode":"independent","candidate_count":4},"messages":[{"role":"user","content":"Compare these designs"}]}
```

Use `ensemble.models` for an exact list; if `candidate_count` is present it must
match the list length. Modes are `independent`, `chain`, and `debate`;
`debate_turns` accepts 0–5. The synthesizer is `SYNTHESIS_MODEL`, falling back
to `ROUTER_MODEL`.

`fallback` overrides the global policy for this call: `enabled`, `max_retries`,
`max_fallback_models` (each 0–3), and `models`. Omitted fields inherit; an empty
`models` list clears every candidate.

```json
{"model":"MODEL_ID","fallback":{"enabled":false},"messages":[{"role":"user","content":"Hello"}]}
```

The result contains `text`, `model`, `provider`, optional `routing`, and
`conversation_id`. `routing.attempts` records each provider call.

### Conversation tools

- `create_conversation` returns an ID with `created_at`, `updated_at`,
  `expires_at`, `version`, and `turns`.
- `list_conversations` accepts optional `limit` and `offset` (default `100, 0`)
  and returns summaries without transcripts.
- `get_conversation` returns the transcript.
- `delete_conversation` removes the transcript and its expiry marker.

Pass the ID as `conversation_id` to resume. Unknown IDs fail the call. Reads do
not refresh expiry; successful turns do. The default TTL is `720h`.

### `health`

Reports local readiness, provider count, and model count without probing
upstream inference.

## Surface differences

OpenAI request fields are HTTP-only. `chat_completion` without `stream` is
non-streaming; `stream: true` collects chunks into one result. Classic calls
that omit `model` use the shared service default provider. When a client sends
`_meta.progressToken`, streaming calls emit MCP progress notifications for
routing and model stages.

## Errors

Errors carry a stable `type` and `code`. For an unknown model, call
`list_models` and retry with an exact ID. For upstream failures, check
`routing.attempts` before retrying. An A2A agent is reachable through the same
registry as `a2a-<name>-<skill>` models; the local card is at
`/.well-known/agent-card.json` and JSON-RPC `SendMessage` mirrors
`chat_completion`.

## Configuration

`voie mcp` uses the same environment variables as the HTTP server and CLI. See
[configuration](configuration.md). Tool arguments, results, prompts, answers,
and conversation contents are never logged.
