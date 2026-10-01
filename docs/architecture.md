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
| `internal/cli` | `serve`, `chat`, `conversations`, `models`, `providers`, `mcp`, `version`, and `update` command dispatch and formatting |
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

`strategy` is `classic` when omitted. Classic routing remains model/provider based, including the generic `openai` default behavior. `auto` filters disabled providers and policy-ineligible models, ranks preferred models, and uses `ROUTER_MODEL` to choose when task rules do not leave one candidate. Supported task names are `coding`, `reasoning`, `writing`, `translation`, `summarization`, and `general`. If auto routing is ambiguous and no router is configured, the request fails with a routing error.

`ensemble` accepts two or three distinct explicit model IDs, or asks the router to select up to three. Candidates run concurrently and at least two must succeed. `SYNTHESIS_MODEL` produces the final answer and falls back to `ROUTER_MODEL`; intermediate answers are not returned or saved in the conversation transcript. Multi-model requests send the prompt to each selected provider and then send successful candidate answers to the synthesizer.

`ROUTING_CONFIG_PATH` points to optional JSON with `models` descriptors and `tasks` rules. Example:

```json
{
  "models": {
    "MODEL_ID": {"description": "Good at code review", "capabilities": ["coding", "reasoning"]}
  },
  "tasks": {
    "coding": {"required_capabilities": ["coding"], "preferred_models": ["MODEL_ID"]}
  }
}
```

Conversation state is opt-in through `conversation_id` and stored locally in `CONVERSATION_DB_PATH` (default `~/.config/voie/conversations.db`). The default inactivity TTL is 720 hours. Reads do not extend it; successful turns do. Expired transcripts are deleted and an ID-only tombstone is retained for 30 days. Conversations cap at 200 messages and 2 MiB; concurrent stale turns receive a conflict. Classic calls without a conversation ID do not open the database.

`ROUTER_MODEL`, `SYNTHESIS_MODEL`, `ROUTING_CONFIG_PATH`, `CONVERSATION_DB_PATH`, and `CONVERSATION_TTL` are shared by HTTP, CLI, and MCP. See the API and interface guides for request examples.

The service owns model and provider selection. An explicit model must resolve to a registered ID or alias. A provider/model mismatch is rejected. The generic `openai` ID, or an omitted API model, uses the configured `DEFAULT_PROVIDER`; when none is configured it resolves to Perplexity's declared `turbo` default. A provider specified with the generic ID uses that provider's declared default model.

Each completion shares the caller's context and the configured end-to-end `TIMEOUT`. Providers create context-aware HTTP requests, and Duck.ai passes cancellation to its browser capture and HTTP request.

`GET /v1/providers` and the CLI/MCP provider listing probe provider URLs with HTTP GET. `alive` means that an HTTP response was received, including an error status. It does not test model inference. `/health` reports local registry state and does not probe upstream URLs.

## Security and deployment

The HTTP listener defaults to loopback. Binding to another address requires `API_TOKEN`, and all API routes require its bearer token. MCP uses local stdio and does not open a network listener. All three modes are built into the root `voie` executable.

The provider catalogue is runtime data. Use `voie models --json` or `GET /v1/models` rather than copying model counts into other integrations.
