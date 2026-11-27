# CLI guide

The `voie` executable is the CLI, the HTTP server, and the MCP server. Install
it with the [installation guide](install.md), or build from a checkout:

```bash
go build -o voie .
```

## Commands

```text
voie serve [--host HOST] [--port PORT]
voie connect [provider] [--token TOKEN] [--field KEY=VALUE] [--list]
voie install [provider] [--list] [--verbose]
voie keys [provider]
voie chat --model MODEL [--provider PROVIDER] [--fallback-model ID]... [PROMPT...]
voie chat --strategy auto [--task TASK] [--conversation ID] [PROMPT...]
voie chat --strategy ensemble [--ensemble-mode MODE] [PROMPT...]
voie conversations create|list|show|delete
voie models [--json] [--free|--paid|--all] [--provider NAME]
voie providers [--json]
voie healthcheck
voie mcp
voie completion SHELL
voie version
voie update
```

`voie --help` and `voie <command> --help` work without loading configuration or
initializing providers.

## Chat

`chat` writes only the answer to stdout; progress and errors go to stderr.

```bash
voie chat --model MODEL_ID "Summarize this text"
cat prompt.txt | voie chat --model MODEL_ID
```

Classic requests require an explicit supported `--model`.

### Strategies

`--strategy` selects how the model is chosen:

- `classic` (default): use `--model`.
- `auto`: pick the first eligible model from a routing profile.
- `ensemble`: combine two to five agents and synthesize one answer.

```bash
voie chat --strategy auto --task coding "Review this patch"

voie chat --strategy ensemble --ensemble-mode independent \
  --candidate-count 3 "Compare these designs"

voie chat --strategy ensemble --ensemble-mode chain \
  --candidate-count 3 "Review this proposal"

voie chat --strategy ensemble --ensemble-mode debate \
  --debate-turns 2 "Compare these designs"
```

Ensemble modes are `independent` (parallel answers), `chain` (each agent
reviews the previous draft), and `debate` (agents critique each other's drafts).
`--candidate-count` is 2–5. Repeat `--ensemble-model ID` to choose exact agents;
if both are present, the count must match the list. `--task` names a profile
from `routing.json`; profiles can opt into `ROUTER_MODEL`. The synthesis model
is `SYNTHESIS_MODEL`, falling back to `ROUTER_MODEL`.

### Fallback

Transient failures (network errors, HTTP `408`, `429`, `5xx`) retry the same
model, then fall back to other models. Missing models skip straight to
fallback; other `4xx` responses stop.

```bash
voie chat --model MODEL_ID --no-fallback "Single attempt"
voie chat --model MODEL_ID --fallback-retries 0 "Go straight to fallback"
voie chat --model MODEL_ID --fallback-model MODEL_B --fallback-model MODEL_C "Try these in order"
```

Per-request flags: `--fallback`, `--no-fallback`, `--fallback-retries N`,
`--fallback-limit N` (each `0`–`3`), and repeatable `--fallback-model ID`.
`--fallback-model` replaces the configured candidate list for every strategy.
Without a flag, the global policy applies. See
[configuration](configuration.md#routing-conversations-and-fallback).

### Conversations

```bash
ID=$(voie conversations create)
voie chat --strategy auto --conversation "$ID" "Continue the discussion"
voie conversations list --limit 20 --offset 0
voie conversations show "$ID"
voie conversations delete "$ID"
```

Sessions are local, expire after `CONVERSATION_TTL` of inactivity (default
`720h`), and successful turns refresh expiry. `list` accepts `--limit` and
`--offset` (default `100, 0`) and omits transcripts; `show` prints JSON.

## Discovery

`voie models` lists `PROVIDER`, `MODEL`, `TIER`, and `PRICE`. `--free` and
`--paid` filter by tier, `--all` is the default, `--provider NAME` limits the
output, and `--json` is for scripts. A paid model is callable; the upstream
decides whether the request succeeds. Unknown IDs are rejected.

`voie providers` checks provider URLs with HTTP GET and reports whether each
one is reachable. `alive` does not prove that inference works. `voie healthcheck`
probes the local `/health` route and exits nonzero unless it returns `200`.

## Providers and gateways

`voie install` prints a compact `NAME`, `TYPE`, `STATUS`, `ACCOUNT`, `NEXT`
table; `NEXT` is the command to run next. `--verbose` adds ports, labels,
install commands, and setup hints. `voie install <name>` installs a gateway or
agent CLI.

`voie connect` with no argument opens a menu; `voie connect <provider> --token
<value>` works in scripts. `voie connect --list` shows what is connected.
`voie keys <provider>` prints the account URL and local key policy.

`voie connect disconnect <provider>` removes the saved key and hides the
provider's models. For CLI agents and gateways, it asks whether to delete
installed files; `--remove-app` or `--keep-app` answers non-interactively.

`voie connect pplx-proxy` installs the optional Perplexity gateway. It opens a
browser, captures the session cookie into the gateway `.env` with `0600`
permissions, and starts the gateway. If the session expires, rerun the command;
the cookie is never logged. Only its `auto` lane is free; subscription models
are `paid` with `price unknown`, and an upstream `403` is surfaced verbatim.

## A2A

`voie connect a2a:<name> --url https://agent.example [--token TOKEN]` discovers
the remote `/.well-known/agent-card.json` and exposes its skills as
`a2a-<name>-<skill>` models. Calls use JSON-RPC `SendMessage` or
`SendStreamingMessage`:

```bash
voie chat --model a2a-<name>-<skill> --stream "Hello"
```

The local server also publishes its own card and accepts JSON-RPC at `/a2a`.

## Surface differences

OpenAI request fields (`temperature`, `max_tokens`, `top_p`, `stop`, `tools`,
`tool_choice`, `stream_options`) are HTTP-only; the CLI exposes routing,
strategy, fallback, and streaming flags instead. Classic requests require an
explicit `--model` here, while HTTP and MCP can use `DEFAULT_PROVIDER` for the
generic `openai` model.

Progress lines on stderr start with a short `request_id`, shared with HTTP and
MCP logs.

## Configuration

`HOST`, `PORT`, `API_TOKEN`, `TIMEOUT`, `DEBUG`, routing, conversation, and
fallback settings are shared with the HTTP server. See
[configuration](configuration.md) for the full list.

## Troubleshooting

- Model rejected: run `voie models` and use an exact current ID.
- Provider unavailable: try another model. Reachability is not inference.
- Duck.ai: Chrome or Chromium must be installed on the host.
- Public bind rejected: set `API_TOKEN` before running `serve`.
