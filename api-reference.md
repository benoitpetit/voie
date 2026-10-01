# HTTP API reference

The `voie` HTTP transport exposes an OpenAI-compatible chat endpoint and provider/model discovery. For CLI and MCP interfaces, see the [CLI guide](docs/cli.md) and [MCP guide](docs/mcp.md).

## Base URL and authentication

The default base URL is `http://127.0.0.1:8080`. Configure it with `HOST` and `PORT`.

Loopback binds do not require a token. A non-loopback `HOST` requires `API_TOKEN`; send it on every protected request:

```http
Authorization: Bearer <API_TOKEN>
```

Cross-origin browser requests are rejected. Requests without an `Origin` header (including CLI and MCP clients) are unaffected; browser clients should call the API from the same host origin.

## `POST /v1/chat/completions`

Creates a completion. The `model` must be a supported model ID or alias. The generic `openai` model or an omitted model uses `DEFAULT_PROVIDER` when configured, otherwise Perplexity's `turbo` default. An explicit `provider` must support the requested model.

### Request

```json
{
  "model": "MODEL_ID",
  "provider": "optional-provider-name",
  "strategy": "classic",
  "task": "coding",
  "models": ["MODEL_A", "MODEL_B"],
  "conversation_id": "optional-local-session-id",
  "messages": [
    {"role": "user", "content": "Hello"}
  ],
  "stream": false
}
```

`messages` is required and must contain objects with a supported `role` (`system`, `developer`, `user`, `assistant`, or `tool`). Messages require `content`, except an assistant message may provide `tool_calls` with empty content. `provider`, `strategy`, `task`, `models`, `conversation_id`, and `stream` are optional. When a model ID is supported by more than one provider, the explicit `provider` selects which one handles the request; discovery reports the default route.

`strategy` defaults to `classic`, preserving existing model/provider selection. `auto` selects one eligible model. The optional `task` hint accepts `coding`, `reasoning`, `writing`, `translation`, `summarization`, or `general` and guides automatic candidate selection in `auto` and in `ensemble` when `models` is omitted. `ROUTER_MODEL` is required when automatic selection has more than one eligible candidate; a unique task-rule match needs no router call. `ensemble` runs 2 or 3 distinct models concurrently and synthesizes after at least 2 succeed. `models` may explicitly supply ensemble candidates; otherwise `ROUTER_MODEL` selects them. `SYNTHESIS_MODEL` is used for the final answer and falls back to `ROUTER_MODEL`. Multi-model requests send the prompt to each selected provider and send successful intermediate answers to the synthesizer.

When supplying `models` explicitly, list only ensemble candidates; the synthesis model must be separate.

An optional `routing` object in responses identifies the strategy, inferred task, and per-model success status. `conversation_id` appears in responses when the request used local conversation tracking. Streaming responses put available routing and conversation metadata on the first SSE chunk.

Automatic routing:

```json
{"strategy":"auto","task":"coding","messages":[{"role":"user","content":"Review this code"}]}
```

Ensemble with automatic candidate selection and a task hint:

```json
{"strategy":"ensemble","task":"reasoning","messages":[{"role":"user","content":"Compare these approaches"}]}
```

To select exact ensemble candidates, provide them in `models`; in that case, `task` does not change the explicit list.

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

If an error occurs before the stream starts, the server returns a normal JSON error status. Errors after the first SSE event are sent as an SSE `error` event.

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

## Local conversations

Conversation storage is local SQLite, configured by `CONVERSATION_DB_PATH` (default `~/.config/voie/conversations.db`). `CONVERSATION_TTL` defaults to `720h` of inactivity. Successful turns refresh expiry; reads do not. Expired transcripts are deleted, with an ID-only tombstone retained for 30 days.

- `POST /v1/conversations` creates a conversation and returns `201` with its ID and timestamps.
- `GET /v1/conversations` returns summary rows without transcript contents.
- `GET /v1/conversations/{id}` returns the transcript.
- `DELETE /v1/conversations/{id}` removes it and returns `204`.

Send `conversation_id` in a completion request to append a turn and include saved turns as context. Only successful completions are saved. Conversations are capped at 200 messages and 2 MiB. A stale concurrent turn returns `409`; expired IDs return `410` during the 30-day tombstone period and then `404`. Requests without `conversation_id` never open the local conversation database.

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
| `400` | Invalid input, unknown model, or provider/model mismatch |
| `401` | Missing or invalid bearer token |
| `404` | Unknown provider |
| `408` | Request canceled |
| `503` | Provider disabled |
| `502` | Upstream provider failure |
| `504` | Completion timed out |
| `409` | Conversation changed during a concurrent turn |
| `410` | Conversation expired (tombstone retained) |
| `500` | Local conversation storage failure |

Completions use the configured end-to-end `TIMEOUT` (seconds), defaulting to 120 seconds. Unknown model IDs are rejected instead of being silently sent to another provider.

Routing and conversation configuration: `ROUTER_MODEL` chooses among eligible models, `SYNTHESIS_MODEL` produces ensemble output (fallback: router), `ROUTING_CONFIG_PATH` defaults to `~/.config/voie/routing.json`, `CONVERSATION_DB_PATH` defaults to `~/.config/voie/conversations.db`, and `CONVERSATION_TTL` defaults to `720h`.
