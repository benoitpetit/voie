# Architecture

`voie` is one Go program with three entry points: an HTTP API, a local CLI, and an MCP stdio server. Each adapter calls the same application service and provider registry.

Every interface forwards the request to a third-party provider; there is no local inference and no local model weight.

## Packages

| Package | Responsibility |
| --- | --- |
| `internal/app` | Provider contract, registry, classic/automatic/ensemble routing, conversation history, validation, completion timeout, typed errors, catalogue, and provider health probes |
| `internal/storage/sqlite` | Lazy local SQLite conversation storage, version checks, quotas, expiry cleanup, and expired-ID tombstones |
| `providers` | Concrete upstream implementations and the default provider registry |
| `internal/runtime` | Configuration validation and construction of the registry, service, HTTP server, and MCP server |
| `internal/transport/httpapi` | OpenAI-compatible JSON/SSE routes, response mapping, CORS, and bearer authentication |
| `internal/cli` | `serve`, `chat`, `conversations`, `models`, `providers`, `mcp`, `connect`, `install`, `completion`, `version`, and `update` command dispatch and formatting |
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

`strategy` is `classic` when omitted. Classic routing remains model/provider based, including the generic `openai` default behavior. `auto` filters unavailable providers and models outside the selected profile, then uses the first eligible model in deterministic order: `preferred_models` first, followed by model ID. A profile can set `selection` to `router` to ask `ROUTER_MODEL` to choose from the eligible pool. The optional `task` request field names a profile; when omitted, `default_profile` applies. Custom profile and capability names are accepted. The legacy `tasks` configuration remains supported for the built-in task names.

`ensemble` runs two to five distinct agents and synthesizes their work with `SYNTHESIS_MODEL` (or `ROUTER_MODEL` as the synthesis fallback). Its configured default mode is `independent`: agents answer in parallel. `candidate_count` controls automatic selection by default and can be overridden per request; request-level `ensemble.models` selects exact agent IDs. In `chain`, each agent reviews the previous draft before synthesis; `candidate_count` or the compatible `chain_agents` option controls the chain length. `debate` starts with parallel drafts, then gives every agent the others' latest drafts to critique and revise; `debate_turns` controls 0–5 review rounds, with zero meaning initial drafts go directly to synthesis. Intermediate answers are not returned or saved in the conversation transcript.

`ROUTING_CONFIG_PATH` points to optional JSON with model descriptors, configurable `profiles`, ensemble defaults, and a `fallback` policy. `debate_turns` accepts `0`–`5`; zero keeps the initial drafts and proceeds directly to synthesis. Example:

```json
{
  "models": {
    "MODEL_ID": {"description": "Good at code review", "capabilities": ["coding", "reasoning"]}
  },
  "default_profile": "general",
  "profiles": {
    "general": {"preferred_models": ["MODEL_ID"], "selection": "local"},
    "code-review": {"required_capabilities": ["coding"], "preferred_models": ["MODEL_ID"], "selection": "router"}
  },
  "ensemble": {
    "mode": "independent",
    "candidate_count": 3,
    "chain_agents": 3,
    "debate_turns": 2
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

Conversation state is opt-in through `conversation_id` and stored locally in `CONVERSATION_DB_PATH` (default `<user-config-dir>/voie/conversations.db`, typically `~/.config/voie/conversations.db` on Linux). macOS and Windows use their standard per-user configuration directories. The default inactivity TTL is 720 hours. Reads do not extend it; successful turns do. Expired transcripts are deleted and an ID-only tombstone is retained for 30 days. Conversations cap at 200 messages and 2 MiB; concurrent stale turns receive a conflict. Classic calls without a conversation ID do not open the database. The local binary uses one local namespace and has no public API key dependency. In a hosted deployment, the Worker supplies an opaque per-key conversation scope; the Core applies it to conversation storage without managing API keys or D1. Project-level rate limiting remains in the Worker. Set `REQUIRE_CONVERSATION_SCOPE=true` to reject `/v1/*` requests without that scope; this setting requires `API_TOKEN`. Existing project-scoped hosted rows are not reassigned to keys, become inaccessible through the hosted API, and expire through normal cleanup.

Every strategy shares the same fallback policy. Failures are classified as transient, unavailable, or permanent: network errors and HTTP `408`, `429`, or `5xx` are transient, a missing model is unavailable, and other `4xx` errors are permanent. Transient failures retry the same model up to `max_retries` and then fall back to other models up to `max_fallback_models`; unavailable failures skip straight to fallback; permanent failures stop immediately. In streaming, retries and fallback only happen before the first content chunk. The configured defaults are `enabled=true`, `max_retries=1`, and `max_fallback_models=1`, each bounded by `0`–`3`. Env vars (`FALLBACK_ENABLED`, `FALLBACK_MAX_RETRIES`, `FALLBACK_MAX_MODELS`) are overridden by `routing.json fallback`, which also defines per-model candidate lists under `models`. A request can override the policy through its transport-specific `fallback` object (fields `enabled`, `max_retries`, `max_fallback_models`, `models`); omitted fields inherit the policy, an empty `models` list clears every candidate, and invalid bounds or IDs are rejected before routing calls.

Fallback candidates resolve deterministically: an explicit request list replaces the configured and inferred pools; otherwise the configured per-model list is used, followed by the profile/provider-filtered pool. Ensemble replacement agents exclude already used models, the synthesizer, and the router. The synthesis step keeps its own configured fallback budget; replacement calls cannot consume it. Debate rounds keep an agent's previous draft when that agent's revision fails. Responses carry an `attempts` array that records every agent, debate, synthesis, retry, and fallback provider call. Ensemble routing also reports `ensemble_mode`, `agents`, `turns`, and `synthesis_model`.

`ROUTER_MODEL`, `SYNTHESIS_MODEL`, `ROUTING_CONFIG_PATH`, `CONVERSATION_DB_PATH`, and `CONVERSATION_TTL` are shared by HTTP, CLI, and MCP. See the API and interface guides for request examples.

The service owns model and provider selection. An explicit model must resolve to a registered ID or alias. A provider/model mismatch is rejected. The generic `openai` ID, or an omitted API model, uses the configured `DEFAULT_PROVIDER`; when none is configured it resolves to Perplexity's declared `turbo` default. A provider specified with the generic ID uses that provider's declared default model.

Each completion shares the caller's context and the configured end-to-end `TIMEOUT`. Providers create context-aware HTTP requests, and Duck.ai passes cancellation to its browser capture and HTTP request.

`GET /v1/providers` and the CLI/MCP provider listing probe provider URLs with HTTP GET. `alive` means that an HTTP response was received, including an error status. It does not test model inference. `/health` reports local registry state and does not probe upstream URLs.

## Security and deployment

The HTTP listener defaults to loopback. Binding to another address requires `API_TOKEN`, and all API routes require its bearer token. The HTTP server caps chat request bodies at 2 MiB and bounds header/body read time and idle connections; it leaves response writes unbounded so long-lived SSE streams can flush. `healthcheck` probes the local health route and is used by the container health check. MCP uses local stdio and does not open a network listener. All three modes are built into the root `voie` executable.

The provider catalogue is runtime data. Use `voie models --json` or `GET /v1/models` rather than copying model counts into other integrations.
