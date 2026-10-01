# MCP guide

`voie mcp` runs a local MCP server over stdin/stdout. Before protocol processing begins, it logs the server name, build version, and registered tools (`list_models`, `list_providers`, and `chat_completion`) to stderr. stdout remains reserved for MCP protocol messages.

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

Requires `model` and `messages`; accepts optional `provider`.

```json
{
  "model": "MODEL_ID_FROM_LIST_MODELS",
  "provider": "optional-provider-name",
  "messages": [
    {"role": "user", "content": "Hello"}
  ]
}
```

The result includes assistant text and structured `text`, `model`, and `provider` fields. Unknown model IDs, provider mismatches, timeouts, and upstream failures are returned as tool execution errors that the client can inspect and recover from.

## Troubleshooting

- If the client cannot start the server, check that `command` points to an executable file and `args` contains `mcp`.
- If a model ID is unknown, call `list_models` and retry with one of its exact IDs.
- If completion fails upstream, call `list_providers` for reachability context and try another listed model.
- Duck.ai chat requires Chrome or Chromium on the machine running `voie`.
- Do not treat `alive: true` as proof that a provider can complete a model request.
