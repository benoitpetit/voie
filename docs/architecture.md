# Architecture

`voie` is one Go program with three entry points: an HTTP API, a local CLI, and an MCP stdio server. Each adapter calls the same application service and provider registry.

## Packages

| Package | Responsibility |
| --- | --- |
| `internal/app` | Provider contract, registry, model routing, validation, completion timeout, typed errors, catalogue, and provider health probes |
| `providers` | Concrete upstream implementations and the default provider registry |
| `internal/runtime` | Configuration validation and construction of the registry, service, HTTP server, and MCP server |
| `internal/transport/httpapi` | OpenAI-compatible JSON/SSE routes, response mapping, CORS, and bearer authentication |
| `internal/cli` | `serve`, `chat`, `models`, `providers`, `mcp`, `version`, and `update` command dispatch and formatting |
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

The service owns model and provider selection. An explicit model must resolve to a registered ID or alias. A provider/model mismatch is rejected. The generic `openai` ID, or an omitted API model, uses the configured `DEFAULT_PROVIDER`; when none is configured it resolves to Perplexity's declared `turbo` default. A provider specified with the generic ID uses that provider's declared default model.

Each completion shares the caller's context and the configured end-to-end `TIMEOUT`. Providers create context-aware HTTP requests, and Duck.ai passes cancellation to its browser capture and HTTP request.

`GET /v1/providers` and the CLI/MCP provider listing probe provider URLs with HTTP GET. `alive` means that an HTTP response was received, including an error status. It does not test model inference. `/health` reports local registry state and does not probe upstream URLs.

## Security and deployment

The HTTP listener defaults to loopback. Binding to another address requires `API_TOKEN`, and all API routes require its bearer token. MCP uses local stdio and does not open a network listener. All three modes are built into the root `voie` executable.

The provider catalogue is runtime data. Use `voie models --json` or `GET /v1/models` rather than copying model counts into other integrations.
