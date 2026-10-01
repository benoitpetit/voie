# MCP guide

`voie mcp` runs a local MCP server over stdin/stdout. Before protocol processing begins, it logs the server name, build version, and registered tools (`list_models`, `list_providers`, `chat_completion`, `create_conversation`, `list_conversations`, `get_conversation`, and `delete_conversation`) to stderr. stdout remains reserved for MCP protocol messages.

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

### `list_models`

Takes an empty object. Returns a human-readable list plus structured model entries with `id`, `provider`, and `owned_by`. Use this at runtime; the available catalogue can change.

### `list_providers`

Takes an empty object. Returns provider names, labels, `alive`, default model IDs, and supported model IDs. `alive` reports HTTP URL reachability only, not successful model inference.

### `chat_completion`

Requires `messages`; classic requests require `model`, while automatic and ensemble requests can omit it. Accepts optional `provider`, `strategy`, `task`, `models`, and `conversation_id`. `task` guides automatic candidate selection in `auto` or in `ensemble` when `models` is omitted; supported values are `coding`, `reasoning`, `writing`, `translation`, `summarization`, and `general`.

```json
{
  "model": "MODEL_ID_FROM_LIST_MODELS",
  "provider": "optional-provider-name",
  "messages": [
    {"role": "user", "content": "Hello"}
  ]
}
```

Automatic routing example:

```json
{"strategy":"auto","task":"coding","messages":[{"role":"user","content":"Review this function"}]}
```

Ensemble example with automatic candidate selection and a task hint:

```json
{"strategy":"ensemble","task":"reasoning","messages":[{"role":"user","content":"Compare these designs"}]}
```

When `models` contains explicit ensemble candidates, voie uses that exact list and `task` does not change it.

The result includes assistant text and structured `text`, `model`, and `provider` fields, plus optional `routing` and `conversation_id`. Automatic routing uses `ROUTER_MODEL` only when local task rules do not identify one model. Ensemble calls use `SYNTHESIS_MODEL`, falling back to `ROUTER_MODEL`. Selected providers receive the prompt, and the synthesizer receives successful intermediate answers.

### Conversation tools

- `create_conversation` takes `{}` and returns a new local conversation ID and timestamps.
- `list_conversations` takes `{}` and returns summaries without transcript contents.
- `get_conversation` takes `{"id":"CONVERSATION_ID"}` and returns the transcript.
- `delete_conversation` takes `{"id":"CONVERSATION_ID"}` and deletes the transcript and its expiry marker.

Pass a returned ID as `conversation_id` to `chat_completion` to resume the conversation. The local database defaults to `~/.config/voie/conversations.db`; sessions expire after 720 hours of inactivity unless `CONVERSATION_TTL` changes it. Reads do not refresh expiry. Classic requests without `conversation_id` do not touch local storage.

## Troubleshooting

- If the client cannot start the server, check that `command` points to an executable file and `args` contains `mcp`.
- If a model ID is unknown, call `list_models` and retry with one of its exact IDs.
- If completion fails upstream, call `list_providers` for reachability context and try another listed model.
- Duck.ai chat requires Chrome or Chromium on the machine running `voie`.
- Do not treat `alive: true` as proof that a provider can complete a model request.
