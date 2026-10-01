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
  "messages": [
    {"role": "user", "content": "Hello"}
  ],
  "stream": false
}
```

`messages` is required and must contain objects with a supported `role` (`system`, `developer`, `user`, `assistant`, or `tool`). Messages require `content`, except an assistant message may provide `tool_calls` with empty content. `provider` and `stream` are optional. When a model ID is supported by more than one provider, the explicit `provider` selects which one handles the request; discovery reports the default route.

Use `GET /v1/models` to discover current IDs. For example, the ChatJimmy provider currently advertises `llama3.1-8B`; its upstream is non-streaming, so a request with `stream: true` receives the completed answer as one content chunk.

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

## `GET /`

Returns API name, version, route names, registered provider count, model count, and the `openai_compatible` flag.

## Errors

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

Completions use the configured end-to-end `TIMEOUT` (seconds), defaulting to 120 seconds. Unknown model IDs are rejected instead of being silently sent to another provider.
