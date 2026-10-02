# Architecture

`voie` is one Go program with three entry points: an HTTP API, a local CLI, and an MCP stdio server. Each adapter calls the same application service and provider registry.

## Packages

| Package | Responsibility |
| --- | --- |
| `internal/app` | Provider contract, registry, classic/automatic/ensemble routing, conversation history, validation, completion timeout, typed errors, catalogue, and provider health probes |
| `internal/storage/sqlite` | Lazy local SQLite conversation storage, version checks, quotas, expiry cleanup, and expired-ID tombstones |
| `providers` | Concrete upstream implementations and the default provider registry |
| `internal/runtime` | Configuration validation and construction of the registry, service, HTTP server, and MCP server |
| `internal/transport/httpapi` | OpenAI-compatible JSON/SSE routes, response mapping, CORS, and bearer authentication |
| `internal/cli` | `serve`, `chat`, `conversations`, `models`, `providers`, `mcp`, `completion`, `version`, and `update` command dispatch and formatting |
| `internal/transport/mcp` | MCP tools and stdio transport using the official Go SDK |
| `config` | Environment parsing, defaults, and bind validation |

## Request flow

```text
HTTP / CLI / MCP
       ↓
      app.Service
  validate → route → timeout
       ↓
app.Registry → provider
```

## Routing and conversations

`strategy` is `classic` when omitted. Classic routing remains model/provider based, including the generic `openai` default behavior. `auto` filters disabled providers and policy-ineligible models, ranks preferred models, and uses `ROUTER_MODEL` to choose when task rules do not leave one candidate. The optional `task` hint accepts `coding`, `reasoning`, `writing`, `translation`, `summarization`, or `general`; it guides automatic candidate selection in `auto` and in `ensemble` when explicit model IDs are not supplied. If automatic routing is ambiguous and no router is configured, the request fails with a routing error.

`ensemble` accepts two or three distinct explicit model IDs, or asks the router to select up to three. Candidates run concurrently and at least two must succeed. `SYNTHESIS_MODEL` produces the final answer and falls back to `ROUTER_MODEL`; intermediate answers are not returned or saved in the conversation transcript. Multi-model requests send the prompt to each selected provider and then send successful candidate answers to the synthesizer.

`ROUTING_CONFIG_PATH` points to optional JSON with `models` descriptors, `tasks` rules, and a `fallback` policy. Example:

```json
{
  "models": {
    "MODEL_ID": {"description": "Good at code review", "capabilities": ["coding", "reasoning"]}
  },
  "tasks": {
    "coding": {"required_capabilities": ["coding"], "preferred_models": ["MODEL_ID"]}
  },
  "fallback": {
    "enabled": true,
    "max_retries": 1,
    "max_fallback_models": 1,
    "models": {
      "MODEL_ID": ["OTHER_MODEL"]
    }
  }
}
```

Conversation state is opt-in through `conversation_id` and stored locally in `CONVERSATION_DB_PATH` (default `<user-config-dir>/voie/conversations.db`, typically `~/.config/voie/conversations.db` on Linux). macOS and Windows use their standard per-user configuration directories. The default inactivity TTL is 720 hours. Reads do not extend it; successful turns do. Expired transcripts are deleted and an ID-only tombstone is retained for 30 days. Conversations cap at 200 messages and 2 MiB; concurrent stale turns receive a conflict. Classic calls without a conversation ID do not open the database.

Every strategy shares the same fallback policy. Failures are classified as transient, unavailable, or permanent: network errors and HTTP `408`, `429`, or `5xx` are transient, a missing model is unavailable, and other `4xx` errors are permanent. Transient failures retry the same model up to `max_retries` and then fall back to other models up to `max_fallback_models`; unavailable failures skip straight to fallback; permanent failures stop immediately. In streaming, retries and fallback only happen before the first content chunk. The configured defaults are `enabled=true`, `max_retries=1`, and `max_fallback_models=1`, each bounded by `0`–`3`. Env vars (`FALLBACK_ENABLED`, `FALLBACK_MAX_RETRIES`, `FALLBACK_MAX_MODELS`) are overridden by `routing.json fallback`, which also defines per-model candidate lists under `models`. A request can override the policy through its transport-specific `fallback` object (fields `enabled`, `max_retries`, `max_fallback_models`, `models`); omitted fields inherit the policy, an empty `models` list clears every candidate, and invalid bounds or IDs fail before any provider call.

Fallback candidates resolve deterministically: the request's explicit `models`, then the configured per-model list, then the eligible pool. For automatic routing the pool is the same task/capability/provider-filtered candidates the router used, excluding the primary. For ensembles the pool excludes the used candidates, the synthesizer, and the router, each candidate is attempted once, and the synthesis step reuses the remaining `max_fallback_models` budget. Responses carry an `attempts` array that records each provider call's model, provider, attempt index, outcome (`succeeded`, `retryable_failure`, `unavailable`, `failed`), and duration in milliseconds. See the interface guides for request and response examples.

`ROUTER_MODEL`, `SYNTHESIS_MODEL`, `ROUTING_CONFIG_PATH`, `CONVERSATION_DB_PATH`, and `CONVERSATION_TTL` are shared by HTTP, CLI, and MCP. See the API and interface guides for request examples.

The service owns model and provider selection. An explicit model must resolve to a registered ID or alias. A provider/model mismatch is rejected. The generic `openai` ID, or an omitted API model, uses the configured `DEFAULT_PROVIDER`; when none is configured it resolves to Perplexity's declared `turbo` default. A provider specified with the generic ID uses that provider's declared default model.

Each completion shares the caller's context and the configured end-to-end `TIMEOUT`. Providers create context-aware HTTP requests, and Duck.ai passes cancellation to its browser capture and HTTP request.

`GET /v1/providers` and the CLI/MCP provider listing probe provider URLs with HTTP GET. `alive` means that an HTTP response was received, including an error status. It does not test model inference. `/health` reports local registry state and does not probe upstream URLs.

## Security and deployment

The HTTP listener defaults to loopback. Binding to another address requires `API_TOKEN`, and all API routes require its bearer token. MCP uses local stdio and does not open a network listener. All three modes are built into the root `voie` executable.

The provider catalogue is runtime data. Use `voie models --json` or `GET /v1/models` rather than copying model counts into other integrations.
