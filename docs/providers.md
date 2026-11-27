# Providers

voie forwards requests to public providers; it never runs a model locally.
The registry has 52 providers in five groups. Only usable providers appear in
`voie models` and in routing.

## Keyless

These work without an account:

`duckai`, `deepai`, `quillbot`, `yqcloud`, `jimmy`, `kilo`, `ovh`,
`blockrun`, `pollinations`, `opencode-zen`, and `opencode-go`.

The OpenCode providers relay through a local gateway on `127.0.0.1:8090`; see
[OpenCode gateway setup](local-gateway.md). Duck.ai needs Chrome or Chromium on
the host.

## Token APIs

`openrouter`, `orcarouter`, `groq`, `google-ai-studio`, `nvidia-nim`, `zai`,
`mistral`, `cloudflare-ai`, `cohere-trial`, `requesty`, `aihubmix`, `anyapi`,
`beatapi`, `iflytek-spark`, `vlm-run`, and `runcrate`.

Run `voie connect <provider>` once. `voie keys <provider>` prints where to get
the credential and which environment variable overrides the saved token.

## Coding agents

`codebuddy`, `minimax-code`, `devin`, `qoder`, `copilot-cli`, and `cursor` use
each product's official ACP interface. They run on your existing subscription
and show `subscribe` in the price column.

`voie install <provider>` installs the agent CLI; `voie connect <provider>`
starts its login flow.

## Local gateways

`antigravity`, `kiro`, and `cline` start a loopback bridge on demand.
`voie connect` installs the missing toolchain and walks through the upstream
login. Their free-model lists rotate, so prefer stable aliases and check the
upstream catalogue before pinning an ID.

Routing personal accounts through a third-party gateway may violate the
product's terms. Use accounts you can afford to lose.

## Third-party gateways

16 third-party gateways are declared as opt-in providers:

OmniRoute, Qoder2API, WindsurfAPI, Copilot API, Cursor ACP Proxy, CodeArts2API,
CLI2API, WorkBuddy2API, FreeLLM Pool, Free LLM Proxy Router, Ollama, LM Studio,
llama.cpp, 9routerx (web-cookie lanes disabled), FreeLLMAPI (remote catalogue
disabled), and pplx-proxy.

They are hidden until installed and carry account or terms-of-service risk.
`voie install` shows their state and the next command; `voie install <name>`
sets one up; `voie keys <name>` explains the account and local key policy.
Installed gateways start automatically on first use.

For gateways with a dashboard, `voie connect <gateway>` starts it, prints the
dashboard URL, and waits for you to finish before stopping it. Copilot API and
Cursor ACP Proxy offer their CLI login instead.

### pplx-proxy

`voie install pplx-proxy` / `voie connect pplx-proxy` installs the Perplexity
gateway on loopback port `8892`. `voie connect` opens a visible browser so you
can sign in, then captures the session cookie into the gateway's `.env` with
`0600` permissions. If Perplexity later rejects the session, rerun
`voie connect pplx-proxy`; the cookie value is never logged.

Only the `auto` lane is free. Subscription models are `paid` with
`price unknown`. An upstream `403` means the connected account tier cannot use
that model, and the provider message is surfaced verbatim.

## Accounts and cleanup

`voie connect` with no argument opens an interactive menu;
`voie connect <provider> --token <value>` works in scripts. `voie connect --list`
shows connection state, and `voie connect disconnect <provider>` removes the
saved key and hides the provider's models. For CLI agents and gateways, add
`--remove-app` to also delete installed files, or `--keep-app` to keep them.

## Free and paid models

`voie models` lists `TIER` (`free` or `paid`) and `PRICE`. Use `--free`,
`--paid`, `--all`, and `--provider NAME` to filter. `voie models --json`,
`GET /v1/models?tier=&provider=`, and MCP `list_models` expose the same fields.

A paid model is callable; the upstream decides whether the request succeeds.
Free models show `USD 0/0`. Public per-token prices come from OpenRouter when
available; paid models without a public price show `price unknown`. OrcaRouter
publishes its authenticated catalogue as `paid` with real prices.

## A2A agents

`voie connect a2a:<name> --url https://agent.example [--token TOKEN]` discovers
the remote agent card and exposes its skills as `a2a-<name>-<skill>` models.
`voie chat --model a2a-<name>-<skill> --stream PROMPT` forwards the call with
JSON-RPC `SendMessage` or `SendStreamingMessage`.

voie is also an A2A server: `GET /.well-known/agent-card.json` is public, and
`POST /a2a` accepts JSON-RPC calls behind the normal HTTP bearer policy.
