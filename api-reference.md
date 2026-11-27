# HTTP API reference

The HTTP transport exposes an OpenAI-compatible chat endpoint and
provider/model discovery. Start it with `voie serve`; the default base URL is
`http://127.0.0.1:8080`.

Every interface forwards the request to a third-party provider; there is no local inference and no local model weight. For the CLI and MCP surfaces, see the [CLI guide](docs/cli.md) and [MCP guide](docs/mcp.md).

## Authentication

Loopback binds need no token. A non-loopback `HOST` requires `API_TOKEN`; send
it on every protected request:

```http
Authorization: Bearer <API_TOKEN>
```

Hosted deployments set `REQUIRE_CONVERSATION_SCOPE=true`. Every `/v1/*` route
then requires a 64-character lowercase hexadecimal
`X-Voie-Conversation-Scope` header supplied by the trusted Worker. The Worker
derives it from the active API key and replaces any client-supplied value;
hosted clients should not set it. Keys for the same repository get separate
scopes while rate limiting stays at project level.

Cross-origin browser requests are rejected. Requests without an `Origin`
header, including CLI and MCP clients, are unaffected.

## `POST /v1/chat/completions`

```json
{
  "model": "MODEL_ID",
  "messages": [{"role": "user", "content": "Hello"}],
  "stream": false
}
```

`messages` is required; roles are `system`, `developer`, `user`, `assistant`,
and `tool`. Messages require `content`, except an assistant message may provide
`tool_calls` with empty content. `model` is required for classic requests.

Optional fields: `provider`, `strategy`, `task`, `ensemble`, `fallback`,
`conversation_id`, and `stream`. `provider` must support the requested model or
every explicit ensemble candidate.

Chat bodies are capped at 2 MiB. Request reads and idle connections are bounded;
SSE response writes are not.

### Strategies

`strategy` defaults to `classic`, which uses `model` and `provider`. The generic
`openai` model, or an omitted classic model, uses `DEFAULT_PROVIDER` or the
first provider that reports itself working.

`auto` selects one eligible model from a routing profile:

```json
{"strategy":"auto","task":"coding","messages":[{"role":"user","content":"Review this code"}]}
```

`task` names a profile from `routing.json`; omitted, `default_profile` applies.
Selection is deterministic unless the profile sets `selection: "router"`.

`ensemble` runs two to five agents and synthesizes their work:

```json
{"strategy":"ensemble","task":"reasoning","ensemble":{"mode":"independent","candidate_count":4},"messages":[{"role":"user","content":"Compare these approaches"}]}
```

`ensemble.mode` is `independent`, `chain`, or `debate`; it defaults to the
configured value. `candidate_count` selects agents automatically, while
`ensemble.models` supplies an exact list. If both are present, the count must
match the list length. `debate_turns` accepts 0–5. `SYNTHESIS_MODEL` produces the
final answer, falling back to `ROUTER_MODEL`. Root-level `models` remains a
legacy alias for the explicit list and cannot be combined with
`ensemble.models`. Explicit candidates must be 2–5 distinct models and must not
include the synthesizer.

### Fallback

`fallback` overrides the global policy for one request. Omitted fields inherit;
`enabled`, `max_retries`, and `max_fallback_models` (each 0–3) override bounds;
`models` replaces the configured candidate list.

```json
{"model":"MODEL_ID","fallback":{"enabled":false},"messages":[{"role":"user","content":"Hello"}]}
```

An empty `models` list clears every candidate. Invalid bounds or IDs return
`400` before any provider call. Retries and fallback happen only before the
first streamed content chunk.

### Non-streaming response

```json
{
  "id": "chatcmpl-example",
  "object": "chat.completion",
  "created": 1780000000,
  "model": "MODEL_ID",
  "provider": "provider-name",
  "choices": [
    {
      "index": 0,
      "message": {"role": "assistant", "content": "Hello."},
      "finish_reason": "stop"
    }
  ],
  "usage": {"prompt_tokens": 4, "completion_tokens": 2, "total_tokens": 6},
  "routing": {
    "strategy": "classic",
    "attempts": [
      {"model": "MODEL_ID", "provider": "provider-name", "attempt": 1, "outcome": "retryable_failure", "duration_ms": 812},
      {"model": "MODEL_ID", "provider": "provider-name", "attempt": 2, "outcome": "succeeded", "duration_ms": 703}
    ]
  }
}
```

`routing` identifies the strategy and records each provider call. For ensembles
it also reports `ensemble_mode`, `agents`, `turns`, and `synthesis_model`.
`conversation_id` appears when conversation tracking is used.

### Streaming response

Set `stream` to `true` to receive `text/event-stream` with
`chat.completion.chunk` events followed by `data: [DONE]`.

```bash
curl -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"MODEL_ID","stream":true,"messages":[{"role":"user","content":"Hello"}]}'
```

Errors before the stream starts use a normal JSON status; later errors arrive
as an SSE `error` event. Some providers return a complete answer, so a streamed
request may receive one content chunk. With `stream_options.include_usage`, a
final chunk carries estimated usage.

## `GET /v1/models`

Lists the current catalogue. Provider catalogues change, so discover at
runtime instead of pinning a count.

```json
{
  "object": "list",
  "data": [
    {
      "id": "MODEL_ID",
      "object": "model",
      "created": 1780000000,
      "owned_by": "Provider label",
      "permission": [],
      "root": "MODEL_ID",
      "parent": null,
      "meta": {"provider": "provider-name", "url": "https://provider.example"}
    }
  ]
}
```

Each item has a `tier` (`free` or `paid`), an optional `price` object
(`currency`, `prompt_per_token`, `completion_per_token`), and `group`. Filter
with `?tier=free|paid` and `?provider=NAME`; an invalid tier or unknown provider
returns `400` or `404`. Free models show `USD 0/0`; paid models without a public
price show `price unknown`. ACP-backed CLI providers show `subscribe`.
OrcaRouter publishes its authenticated catalogue as `paid`; OpenRouter supplies
public per-token prices where available.

## `GET /v1/providers`

Lists providers and checks reachability with concurrent HTTP GETs (three-second
timeout).

```json
{
  "object": "list",
  "count": 1,
  "timestamp": 1780000000,
  "data": [
    {
      "name": "provider-name",
      "label": "Provider label",
      "url": "https://provider.example",
      "working": true,
      "ready": true,
      "alive": true,
      "free_models": ["MODEL_ID"],
      "supported_models": ["MODEL_ID"],
      "default_model": "MODEL_ID",
      "supports_stream": true,
      "needs_auth": false,
      "description": "Provider description",
      "reverse_engineering": true
    }
  ]
}
```

`alive` is true when any HTTP response is received, including 4xx and 5xx. It
means URL reachability only, not working inference. `free_models` is always
present: non-empty lists verified free IDs; empty means none are free.

## `GET /health`

Returns local readiness without probing upstreams: `status`, `timestamp`,
`version`, `providers_total`, `providers_working`, `working_providers`, and
`total_models`.

## Conversations

`CONVERSATION_DB_PATH` selects the SQLite file; the default is
`<user-config-dir>/voie/conversations.db` (`~/.config/voie/conversations.db` on
Linux). `CONVERSATION_TTL` defaults to `720h` of inactivity. Successful turns
refresh expiry; reads do not. Expired transcripts are deleted, with an ID-only
tombstone kept for 30 days. Hosted keys receive isolated scopes.

- `POST /v1/conversations` creates one and returns `201` with timestamps.
- `GET /v1/conversations` lists summaries without transcripts.
- `GET /v1/conversations/{id}` returns the transcript.
- `DELETE /v1/conversations/{id}` returns `204`.

Send `conversation_id` in a completion to append a turn. Create it first; an
unknown ID returns `404`. Only successful completions are saved. Conversations
cap at 200 messages and 2 MiB. A stale concurrent turn returns `409`; expired
IDs return `410` during the tombstone period, then `404`. Requests without
`conversation_id` do not open the database.

## `GET /`

Returns the API name, version, route names, provider count, model count, and the
`openai_compatible` flag.

## Errors

Each request is logged to stderr with a short request ID, method, path, status,
and duration. Chat logs add the routing summary or a typed failure category.
Logs omit query values, headers, bodies, prompts, answers, token counts, and
client IPs.

```json
{
  "error": {
    "message": "model \"unknown\" is not supported",
    "type": "error",
    "code": "400"
  }
}
```

| HTTP status | Meaning |
| --- | --- |
| `400` | Invalid input, unknown model, provider/model mismatch, or invalid fallback bounds/IDs |
| `401` | Missing or invalid bearer token, or missing hosted conversation scope |
| `403` | Origin host or port does not match the request host |
| `404` | Unknown provider, conversation, or route |
| `405` | Unsupported method on a route that checks methods |
| `408` | Request canceled |
| `409` | Conversation changed during a concurrent turn |
| `410` | Conversation expired (tombstone retained) |
| `413` | Chat body exceeds 2 MiB |
| `500` | Local conversation storage failure |
| `502` | Upstream provider failure |
| `503` | Provider disabled |
| `504` | Completion timed out |

Completions use the configured end-to-end `TIMEOUT` (default 120 seconds).
Unknown model IDs are rejected instead of being sent to another provider.

## Supported OpenAI subset

The endpoint accepts `model`, `messages`, `stream`, `provider`, `strategy`,
`task`, `ensemble`, `fallback`, and `conversation_id`. It forwards
`temperature`, `max_tokens`, `top_p`, `stop`, `tools`, `tool_choice`, and
`stream_options` when the selected provider is OpenAI-compatible; providers
that do not implement a field ignore it. Only one completion choice is supported:
`n` greater than 1 returns `400`.

Non-streaming responses include `message.tool_calls` when the upstream returns
them. Streamed responses emit `delta.tool_calls` chunks before `finish_reason`.
Token counts are estimates unless the upstream reports them.

## A2A endpoints

`GET /.well-known/agent-card.json` returns the public Agent Card without a
bearer token. `POST /a2a` accepts JSON-RPC 2.0 `SendMessage`,
`SendStreamingMessage` (SSE), `GetTask`, `ListTasks`, `CancelTask`,
`SubscribeToTask`, and `GetExtendedAgentCard`; legacy method aliases are
accepted. `SendMessage` takes text parts and optional `metadata` (`strategy`,
`task`, `model`, `skill`, `ensemble`) and returns a Task with a text artifact.
Connected remote agents appear as `a2a-<name>-<skill>` model IDs and work
through the standard chat endpoint.
