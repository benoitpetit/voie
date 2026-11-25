# MCP guide

`voie mcp` runs a local MCP server over stdin/stdout. Before protocol processing begins, it logs the server name, build version, and registered tools (`list_models`, `list_providers`, `chat_completion`, `create_conversation`, `list_conversations`, `get_conversation`, and `delete_conversation`) to stderr. stdout remains reserved for MCP protocol messages.

Every interface forwards the request to a third-party provider; there is no local inference and no local model weight.

Each tool call also logs a short request ID, tool name, start, success or failure, and elapsed time to stderr. Logs are monochrome and never include tool arguments, structured results, prompts, answers, or conversation contents.

## Client setup

Install a prebuilt executable by downloading the archive for your operating system and architecture from the [latest release](https://github.com/benoitpetit/voie/releases/latest), or build it from source as shown below. See the [README installation guide](../README.md#install-a-release) for supported platforms. Use the executable's absolute path in your MCP client's configuration:

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

Build it with `go build -o voie .`. The same executable also provides the CLI and HTTP API. No HTTP server is needed for local MCP calls.

## Tools

### The structured `list_models` result includes `models[]` with `id`, `provider`, `owned_by` and optional `group`, plus a `groups[]` grouping array.

`list_models`

Takes an empty object. Returns a human-readable list plus structured model entries with `id`, `provider`, and `owned_by`. Use this at runtime; the available catalogue can change.

### `list_providers`

Takes an empty object. Returns provider names, labels, `alive`, default model IDs, and supported model IDs. `alive` reports HTTP URL reachability only, not successful model inference.

### `chat_completion`

Requires `messages`; classic requests require `model`, while automatic and ensemble requests can omit it. Accepts optional `provider`, `strategy`, `task`, `models`, `ensemble`, `fallback`, and `conversation_id`. For ensembles, prefer `ensemble.candidate_count` and `ensemble.models`; root-level `models` remains supported for compatibility. `task` names a routing profile from `routing.json`; omitted values use `default_profile`. Profile selection is local and deterministic unless the profile sets `selection: "router"`.

```json
{
  "model": "MODEL_ID_FROM_LIST_MODELS",
  "provider": "optional-provider-name",
  "messages": [
    {"role": "user", "content": "Hello"}
  ]
}
```

The `fallback` argument overrides the global fallback policy for this call. Omitted fields inherit the policy; `enabled`, `max_retries` and `max_fallback_models` (each `0`–`3`) override bounds, and `models` supplies candidate model IDs in fallback order. An empty `models` list clears every candidate, `max_retries: 0` skips retries, `max_fallback_models: 0` skips fallback, and `enabled: false` disables both:

```json
{
  "model": "MODEL_ID_FROM_LIST_MODELS",
  "fallback": {"enabled": false},
  "messages": [{"role": "user", "content": "Hello"}]
}
```

```json
{
  "model": "MODEL_ID_FROM_LIST_MODELS",
  "fallback": {"models": ["MODEL_B"]},
  "messages": [{"role": "user", "content": "Hello"}]
}
```

Automatic routing example:

```json
{"strategy":"auto","task":"coding","messages":[{"role":"user","content":"Review this function"}]}
```

Ensemble example with four automatically selected candidates and a task hint:

```json
{"strategy":"ensemble","task":"reasoning","ensemble":{"mode":"independent","candidate_count":4},"messages":[{"role":"user","content":"Compare these designs"}]}
```

Ensemble example with an exact list of three agent models:

```json
{"strategy":"ensemble","task":"reasoning","ensemble":{"mode":"independent","candidate_count":3,"models":["MODEL_A","MODEL_B","MODEL_C"]},"messages":[{"role":"user","content":"Compare these designs"}]}
```

Chain and debate requests can set their agent count or turn count:

```json
{"strategy":"ensemble","ensemble":{"mode":"chain","candidate_count":3},"messages":[{"role":"user","content":"Review this proposal"}]}
```

```json
{"strategy":"ensemble","ensemble":{"mode":"debate","debate_turns":2},"messages":[{"role":"user","content":"Compare these designs"}]}
```

When `ensemble.models` contains explicit candidates, voie uses that exact list and `task` does not change it. If `candidate_count` is included, it must match the list length. `debate_turns` accepts 0–5; zero skips review rounds and goes from initial drafts to synthesis.

The result includes assistant text and structured `text`, `model`, and `provider` fields, plus optional `routing` and `conversation_id`. Ensemble routing reports its algorithm, agent count, debate turns, synthesis model, and each provider call in `attempts`. Ensemble calls use `SYNTHESIS_MODEL`, falling back to `ROUTER_MODEL`; the synthesizer receives successful agent drafts.

### Conversation tools

- `create_conversation` takes `{}` and returns a new local conversation ID with `created_at`, `updated_at`, `expires_at`, `version`, and `turns`.
- `list_conversations` takes `{}` and returns summaries without transcript contents.
- `get_conversation` takes `{"id":"CONVERSATION_ID"}` and returns the transcript.
- `delete_conversation` takes `{"id":"CONVERSATION_ID"}` and deletes the transcript and its expiry marker.

Pass a returned ID as `conversation_id` to `chat_completion` to resume the conversation; an unknown ID fails the tool call. The local database defaults to `<user-config-dir>/voie/conversations.db` (typically `~/.config/voie/conversations.db` on Linux); macOS and Windows use their standard per-user configuration directories. Sessions expire after 720 hours of inactivity unless `CONVERSATION_TTL` changes it. Reads do not refresh expiry. Classic requests without `conversation_id` do not touch local storage.

## Troubleshooting

- If the client cannot start the server, check that `command` points to an executable file and `args` contains `mcp`.
- If a model ID is unknown, call `list_models` and retry with one of its exact IDs.
- If completion fails upstream, call `list_providers` for reachability context and try another listed model.
- Duck.ai chat requires Chrome or Chromium on the machine running `voie`.
- Do not treat `alive: true` as proof that a provider can complete a model request.


`list_models` accepts optional `tier` (`free` or `paid`) and `provider` arguments. Each model result carries `tier` and, for paid models, a `price` string with public per-token prices when available or `price unknown` when not. A grouped text summary is also returned.

## Discovery metadata

`list_models` results carry `tier` and, when a public price is known, `price`; results can be filtered with `tier` and `provider`. `list_providers` returns the full provider metadata: `working`, `ready`, `alive`, `free_models`, `supported_models`, `default_model`, `url`, `supports_stream`, `needs_auth`, `description` and `reverse_engineering`. `free_models` is always present: a non-empty array lists verified free IDs, an empty array means none are free.

Every `free` model exposes `USD 0/0` as its price (zero cost to the caller); public upstream prices replace that value when available. Paid models with no public price show `price unknown`.

OpenRouter also exposes its public catalogue of subscription models as `paid`, with real per-token USD prices from its public API; verified `:free` aliases remain `free` with `USD 0/0`.
