# HTTP API reference

The `voie` HTTP transport exposes an OpenAI-compatible chat endpoint and provider/model discovery. For CLI and MCP interfaces, see the [CLI guide](docs/cli.md) and [MCP guide](docs/mcp.md).

Every interface forwards the request to a third-party provider; there is no local inference and no local model weight.

## Base URL and authentication

The default base URL is `http://127.0.0.1:8080`. Configure it with `HOST` and `PORT`.

Loopback binds do not require a token. A non-loopback `HOST` requires `API_TOKEN`; send it on every protected request:

```http
Authorization: Bearer <API_TOKEN>
```

Hosted deployments also set `REQUIRE_CONVERSATION_SCOPE=true`. In that mode every `/v1/*` route requires a valid 64-character lowercase hexadecimal `X-Voie-Conversation-Scope` supplied by the trusted Worker. The Worker derives it from the active API key record and replaces any client-supplied value; hosted API clients should not set this header themselves. Keys attached to the same repository receive different conversation scopes, while rate limiting remains at project level.

Cross-origin browser requests are rejected. Requests without an `Origin` header (including CLI and MCP clients) are unaffected; browser clients should call the API from the same host origin.

## `POST /v1/chat/completions`

Creates a completion. Classic requests use a supported `model` ID or alias; `auto` and `ensemble` choose models from a routing profile. The generic `openai` model or an omitted classic model uses `DEFAULT_PROVIDER` when configured, otherwise Perplexity's `turbo` default. An explicit `provider` must support the requested model or every explicit ensemble candidate.

### Request

```json
{
  "model": "MODEL_ID",
  "provider": "optional-provider-name",
  "strategy": "classic",
  "fallback": {
    "enabled": true,
    "max_retries": 1,
    "max_fallback_models": 1,
    "models": ["MODEL_B"]
  },
  "conversation_id": "optional-local-session-id",
  "messages": [
    {"role": "user", "content": "Hello"}
  ],
  "stream": false
}
```

`messages` is required and must contain objects with a supported `role` (`system`, `developer`, `user`, `assistant`, or `tool`). Messages require `content`, except an assistant message may provide `tool_calls` with empty content. `provider`, `strategy`, `task`, `models`, `ensemble`, `conversation_id`, and `stream` are optional, subject to the selected strategy. For ensembles, prefer `ensemble.models`; root-level `models` remains supported for existing clients. When a model ID is supported by more than one provider, the explicit `provider` selects which one handles the request; discovery reports the default route.

Chat request bodies are limited to 2 MiB. The server limits request read time and idle connections; response writes remain unbounded for SSE streams.

`strategy` defaults to `classic`, preserving model/provider selection. `auto` selects one eligible model from a routing profile. The optional `task` field names that profile; when omitted, `default_profile` from `routing.json` applies. Local selection is deterministic by default: preferred models are considered first, followed by model ID. A profile can set `selection` to `router` to use `ROUTER_MODEL` for a semantic choice within its eligible candidates.

`ensemble` runs two to five agents and synthesizes their work. `ensemble.candidate_count` chooses how many candidates to select automatically for this request. `ensemble.models` supplies an exact list of agent model IDs instead; if both fields are present, the count must match the list length. If neither is supplied, `candidate_count` from `routing.json` applies (default 3). Root-level `models` remains a legacy way to provide that exact list and cannot be combined with `ensemble.models`. `ensemble.mode` is `independent`, `chain`, or `debate`; it defaults to the configured value (initially `independent`). Independent agents answer in parallel. In chain mode, each later agent reviews the previous draft; `candidate_count` sets its length, while `chain_agents` remains supported for compatibility. Debate starts with parallel drafts, then each agent reviews the others' latest drafts; `debate_turns` sets the review rounds (0–5), and zero skips review rounds before synthesis. `SYNTHESIS_MODEL` produces the final answer and falls back to `ROUTER_MODEL`. The `provider` constraint applies to automatic candidates and is validated against every explicit ensemble model.

When supplying `ensemble.models` explicitly, list 2–5 distinct candidates; the synthesis model must be separate. `model` is not used by `auto` or `ensemble`, and strategy-specific fields are rejected when they do not apply.

`fallback` is an optional per-request override of the global fallback policy. Omitted fields inherit the policy; `enabled` (boolean), `max_retries`, and `max_fallback_models` (each `0`–`3`) override the configured bounds, and `models` supplies candidate model IDs in fallback order, replacing the configured per-model list. An empty `models` list clears every candidate. `max_retries: 0` skips same-model retries, `max_fallback_models: 0` skips fallback, and `enabled: false` disables both. Bounds and model IDs are validated before any provider is called, so an invalid override returns `400`. Retries and fallback only happen before the first streamed content chunk.

```json
{"model":"MODEL_ID","fallback":{"enabled":false},"messages":[{"role":"user","content":"Hello"}]}
```

```json
{"model":"MODEL_ID","fallback":{"models":["MODEL_B"]},"messages":[{"role":"user","content":"Hello"}]}
```

An optional `routing` object identifies the strategy, selected profile, per-model results, and the `attempts` array. For ensembles it also reports `ensemble_mode`, `agents`, `turns`, and `synthesis_model`. Each `attempts` entry records one provider call: `model`, `provider`, `attempt` index, `outcome`, and `duration_ms` when known. `conversation_id` appears when the request used local conversation tracking. Streaming responses put available routing and conversation metadata on the first SSE chunk.

Automatic routing:

```json
{"strategy":"auto","task":"coding","messages":[{"role":"user","content":"Review this code"}]}
```

Ensemble with automatic candidate selection and a task hint:

```json
{"strategy":"ensemble","task":"reasoning","ensemble":{"mode":"independent","candidate_count":4},"messages":[{"role":"user","content":"Compare these approaches"}]}
```

Ensemble with an exact list of three agent models:

```json
{"strategy":"ensemble","task":"reasoning","ensemble":{"mode":"independent","candidate_count":3,"models":["MODEL_A","MODEL_B","MODEL_C"]},"messages":[{"role":"user","content":"Compare these approaches"}]}
```

Sequential review chain with a request-level agent count:

```json
{"strategy":"ensemble","task":"code-review","ensemble":{"mode":"chain","candidate_count":4},"messages":[{"role":"user","content":"Review this proposal"}]}
```

Mutual debate with request-level review rounds:

```json
{"strategy":"ensemble","task":"reasoning","ensemble":{"mode":"debate","debate_turns":2},"messages":[{"role":"user","content":"Compare these approaches"}]}
```

To select exact ensemble candidates, provide them in `ensemble.models`; in that case, `task` does not change the explicit list.

Use `GET /v1/models` to discover current IDs. Some providers return complete responses rather than streaming tokens, so a request with `stream: true` may receive the completed answer as one content chunk.

### Non-streaming response

Successful responses keep the OpenAI-compatible `chat.completion` shape. `provider` identifies the selected upstream provider.

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
  "usage": {
    "prompt_tokens": 4,
    "completion_tokens": 2,
    "total_tokens": 6
  },
  "routing": {
    "strategy": "classic",
    "attempts": [
      {"model": "MODEL_ID", "provider": "provider-name", "attempt": 1, "outcome": "retryable_failure", "duration_ms": 812},
      {"model": "MODEL_ID", "provider": "provider-name", "attempt": 2, "outcome": "succeeded", "duration_ms": 703}
    ]
  }
}
```

### Streaming response

Set `stream` to `true`. The server returns `text/event-stream` with OpenAI-compatible `chat.completion.chunk` JSON events followed by `data: [DONE]`.

```bash
curl -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"MODEL_ID","stream":true,"messages":[{"role":"user","content":"Hello"}]}'
```

If an error occurs before the stream starts, the server returns a normal JSON error status. Errors after the first SSE event are sent as an SSE `error` event. Retries and fallback occur only before the first content chunk; once content has been delivered, a subsequent failure ends the stream without substituting another model.

## `GET /v1/models`

Lists the current model catalogue. Do not rely on a fixed count; provider catalogues can change.

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

## `GET /v1/providers`

Lists registered providers and checks reachability with HTTP GET. Checks run concurrently and each has a three-second timeout.

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
      "alive": true,
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

`alive` is `true` when an HTTP response is received, including 4xx and 5xx statuses. It indicates URL reachability only; it does not verify model inference.

## `GET /health`

Returns local registry health without probing provider websites. The response includes `status`, `timestamp`, `version`, `providers_total`, `providers_working`, `working_providers`, and `total_models`.

## Conversations

Conversation storage is SQLite, configured by `CONVERSATION_DB_PATH` (default `<user-config-dir>/voie/conversations.db`; on Linux, typically `~/.config/voie/conversations.db`). macOS and Windows use their standard per-user configuration directories. `CONVERSATION_TTL` defaults to `720h` of inactivity. Successful turns refresh expiry; reads do not. Expired transcripts are deleted, with an ID-only tombstone retained for 30 days. Local binary use keeps the default single-user namespace. In a hosted deployment, the Worker supplies an opaque scope for the active API key, and every conversation operation is isolated to that key, including when multiple keys belong to the same repository. Existing project-scoped hosted rows are not reassigned to keys; they become inaccessible through the hosted API and expire through normal cleanup.

- `POST /v1/conversations` creates a conversation and returns `201` with its ID and timestamps.
- `GET /v1/conversations` returns summary rows without transcript contents.
- `GET /v1/conversations/{id}` returns the transcript.
- `DELETE /v1/conversations/{id}` removes it and returns `204`.

Send `conversation_id` in a completion request to append a turn and include saved turns as context. The ID must already exist — create it with `POST /v1/conversations` first; an unknown ID returns `404`. Only successful completions are saved. Conversations are capped at 200 messages and 2 MiB. A stale concurrent turn returns `409`; expired IDs return `410` during the 30-day tombstone period and then `404`. Requests without `conversation_id` never open the local conversation database.

## `GET /`

Returns API name, version, route names, registered provider count, model count, and the `openai_compatible` flag.

## Errors

### Server request logs

Each HTTP request, including authentication failures, is logged to stderr with a short request ID, method, URL path, final status, and elapsed time. Chat records also include the routing summary or a typed failure category. Logs omit query values, headers, request and response bodies, prompts, answers, token counts, and client IPs. SSE responses continue to flush each chunk as it is produced; logging does not buffer the response.

Errors use this JSON envelope:

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
| `401` | Missing or invalid bearer token, or missing required hosted conversation scope |
| `413` | Chat request body exceeds 2 MiB |
| `404` | Unknown provider, unknown conversation, or unmatched route |
| `408` | Request canceled |
| `503` | Provider disabled |
| `502` | Upstream provider failure |
| `504` | Completion timed out |
| `409` | Conversation changed during a concurrent turn |
| `410` | Conversation expired (tombstone retained) |
| `500` | Local conversation storage failure |

Completions use the configured end-to-end `TIMEOUT` (seconds), defaulting to 120 seconds. Unknown model IDs are rejected instead of being silently sent to another provider.

Routing and conversation configuration: `ROUTER_MODEL` is used only by profiles with `selection: "router"` and as the ensemble synthesis fallback; `SYNTHESIS_MODEL` produces ensemble output. `ROUTING_CONFIG_PATH` defaults to `<user-config-dir>/voie/routing.json`, `CONVERSATION_DB_PATH` defaults to `<user-config-dir>/voie/conversations.db`, and `CONVERSATION_TTL` defaults to `720h`.
