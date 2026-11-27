# voie

![voie free ai api](docs/voie-banner.png)

`voie` is a free, OpenAI-compatible API relay. One Go executable relays requests to public model providers and exposes the same catalogue through an HTTP API, a CLI, an MCP server over stdio, and an A2A endpoint.

voie does not run models. Inference stays on the services it forwards to. Some
providers work without an account, some need a token, and some start a local
gateway or CLI toolchain on demand.

The registry has 52 providers in five groups: keyless endpoints, token APIs,
coding agents over ACP, local gateways, and 16 third-party gateways. Only usable
providers appear in the catalogue.

## Quick start

**Linux and macOS**

```sh
curl -fsSL https://raw.githubusercontent.com/benoitpetit/voie/main/install.sh | sh
voie serve
```

**Windows** (PowerShell)

```powershell
irm https://raw.githubusercontent.com/benoitpetit/voie/main/install.ps1 | iex
voie serve
```

**npm**

```sh
npm install -g voie-ai
voie serve
```

The API listens on `http://127.0.0.1:8080`. In another terminal:

```sh
voie models --free
voie chat --model MODEL_ID "Hello"
```

```sh
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"MODEL_ID","messages":[{"role":"user","content":"Hello"}]}'
```

Only usable providers appear in `voie models`. Keyless models work immediately;
for token-backed providers, run `voie connect <provider>` once.

Other install options, supported platforms, and the release process are in the
[installation guide](docs/install.md).

## Interfaces

The same executable provides four surfaces:

| Surface | Start with | Details |
| --- | --- | --- |
| HTTP API | `voie serve` | [API reference](api-reference.md) |
| CLI | `voie` | [CLI guide](docs/cli.md) |
| MCP | `voie mcp` | [MCP guide](docs/mcp.md) |
| A2A | `voie serve` | [A2A](#a2a) |

Commands: `serve`, `chat`, `conversations`, `models`, `providers`, `healthcheck`,
`mcp`, `connect`, `install`, `keys`, `completion`, `version`, and `update`.
Run `voie <command> --help` for flags.

MCP client configuration:

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

## Providers

`voie models` lists every callable model with `PROVIDER`, `MODEL`, `TIER`
(`free` or `paid`) and `PRICE`. Filter with `--free`, `--paid`, or
`--provider NAME`; use `--json` for scripts.

- `voie install` lists installable local gateways and their next step.
- `voie connect <provider>` saves a token or starts an upstream login.
- `voie keys <provider>` prints where to get a credential.
- `voie connect disconnect <provider> --remove-app` removes the saved key and
  installed files.

See [providers](docs/providers.md) for the groups, credentials, and gateway
caveats.

## Configuration

voie reads environment variables at command start. It does not load `.env` by
itself.

| Variable | Default | Purpose |
| --- | --- | --- |
| `HOST` | `127.0.0.1` | HTTP bind address |
| `PORT` | `8080` | HTTP bind port |
| `API_TOKEN` | empty | Bearer token required for non-loopback binds |
| `TIMEOUT` | `120` | Completion timeout in seconds |
| `DEBUG` | `false` | Debug logging |

The full list, including routing, conversation, fallback, and provider
variables, is in [configuration](docs/configuration.md) and
[`.env.example`](.env.example).

## A2A

voie is also an A2A server and client. Agents discover the public card at
`GET /.well-known/agent-card.json`, then call `POST /a2a` with JSON-RPC
`SendMessage` or `SendStreamingMessage`. To call a remote agent:

```sh
voie connect a2a:<name> --url https://agent.example
voie chat --model a2a-<name>-<skill> "Hello"
```

Remote skills appear in `voie models` like any other model.

## Documentation

- [Install](docs/install.md): release installers, npm, source builds, releases.
- [Configuration](docs/configuration.md): every environment variable.
- [Providers](docs/providers.md): catalogues, credentials, gateways, A2A.
- [CLI](docs/cli.md): commands, strategies, fallback, conversations.
- [MCP](docs/mcp.md): tools, schemas, and client setup.
- [HTTP API](api-reference.md): endpoints, errors, and OpenAI compatibility.
- [Architecture](docs/architecture.md): shared service and transport layout.
- [Deployment](docs/deploy-coolify.md): hosted origin on Coolify.
- [Local gateway](docs/local-gateway.md): OpenCode Zen and Go relay.
- [Agent Skill](skills/voie/SKILL.md): portable instructions for coding agents.

Public services receive the prompts you send them. Keep secrets out of requests
unless you have checked the provider's data practices.

## Licence

MIT. See [LICENSE](LICENSE).
