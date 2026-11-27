# Configuration

voie reads environment variables when a command starts. It does not load a
`.env` file by itself, so export the values through your shell, container, or
process manager. [`.env.example`](../.env.example) lists every supported
variable.

On Linux, `<user-config-dir>` is usually `~/.config`; macOS and Windows use
their standard per-user configuration directories.

## Server and runtime

| Variable | Default | Purpose |
| --- | --- | --- |
| `HOST` | `127.0.0.1` | HTTP bind address |
| `PORT` | `8080` | HTTP bind port |
| `DEBUG` | `false` | Enable debug logging |
| `TIMEOUT` | `120` | End-to-end completion timeout in seconds |
| `DEFAULT_PROVIDER` | empty | Provider used for the generic `openai` model |
| `API_TOKEN` | empty | Bearer token required for non-loopback binds |
| `REQUIRE_CONVERSATION_SCOPE` | `false` | Reject hosted `/v1/*` requests without a trusted conversation scope; requires `API_TOKEN` |

A non-loopback `HOST` is refused when `API_TOKEN` is empty. Protected HTTP
requests send `Authorization: Bearer <API_TOKEN>`.

Hosted deployments should also set `REQUIRE_CONVERSATION_SCOPE=true`. The
trusted Worker supplies the 64-character lowercase hexadecimal
`X-Voie-Conversation-Scope` header and replaces any client-supplied value;
hosted API clients should not set it themselves.

## Routing, conversations, and fallback

| Variable | Default | Purpose |
| --- | --- | --- |
| `ROUTER_MODEL` | empty | Model used by profiles with `selection: "router"`; ensemble synthesis fallback |
| `SYNTHESIS_MODEL` | empty | Model that merges ensemble answers; falls back to `ROUTER_MODEL` |
| `ROUTING_CONFIG_PATH` | `<user-config-dir>/voie/routing.json` | Local profiles, model descriptions, ensemble defaults, and fallback rules |
| `CONVERSATION_DB_PATH` | `<user-config-dir>/voie/conversations.db` | Local SQLite conversation database |
| `CONVERSATION_TTL` | `720h` | Inactivity lifetime for local conversations |
| `FALLBACK_ENABLED` | `true` | Enable retries and fallback on provider failures |
| `FALLBACK_MAX_RETRIES` | `1` | Same-model retries for transient failures, `0`–`3` |
| `FALLBACK_MAX_MODELS` | `1` | Extra models tried after the primary, `0`–`3` |

Per-model fallback lists in `routing.json` override the environment values.
See [routing](architecture.md#routing).

## Provider credentials

`voie connect` stores provider tokens in
`<user-config-dir>/voie/credentials.json` with `0600` permissions. Environment
variables override saved values.

| Variable | Default | Provider |
| --- | --- | --- |
| `OPENCODE_ZEN_KEY` | `voie-local` | Local `opencode-zen` / `opencode-go` gateway key |
| `VOIE_OPENROUTER_KEY` | saved token | OpenRouter |
| `VOIE_ORCAROUTER_KEY` | saved token | OrcaRouter |
| `VOIE_ANTIGRAVITY_KEY` | `voie-local` | Local CLIProxyAPI gateway |
| `VOIE_KIRO_KEY` | `voie-local` | Local Kiro ACP gateway |
| `VOIE_CLINE_KEY` | `voie-local` | Local Cline ACP bridge |
| `VOIE_GROQ_KEY` | saved token | Groq |
| `VOIE_GOOGLE_AI_STUDIO_KEY` | saved token | Google AI Studio |
| `VOIE_NVIDIA_NIM_KEY` | saved token | NVIDIA NIM |
| `VOIE_ZAI_KEY` | saved token | Z.ai |
| `VOIE_MISTRAL_KEY` | saved token | Mistral |
| `VOIE_CLOUDFLARE_AI_KEY` | saved token | Cloudflare Workers AI |
| `VOIE_CLOUDFLARE_ACCOUNT_ID` | `ACCOUNT_ID` | Cloudflare account used in the Workers AI URL |
| `VOIE_COHERE_TRIAL_KEY` | saved token | Cohere trial |
| `VOIE_REQUESTY_KEY` | saved token | Requesty |
| `VOIE_AIHUBMIX_KEY` | saved token | AIHubMix |
| `VOIE_ANYAPI_KEY` | saved token | AnyAPI |
| `VOIE_BEATAPI_KEY` | saved token | BeatAPI |
| `VOIE_IFLYTEK_SPARK_KEY` | saved token | iFlytek Spark APIPassword |
| `VOIE_VLM_RUN_KEY` | `vlmrun` | VLM Run; anonymous bearer works by default |
| `VOIE_RUNCRATE_KEY` | saved token | Runcrate (`rc_live_...`) |

ACP and third-party gateway providers follow the same
`VOIE_<PROVIDER>_KEY` pattern:

`VOIE_CODEBUDDY_KEY`, `VOIE_MINIMAX_CODE_KEY`, `VOIE_DEVIN_KEY`,
`VOIE_QODER_KEY`, `VOIE_COPILOT_CLI_KEY`, `VOIE_CURSOR_KEY`,
`VOIE_OMNIROUTE_KEY`, `VOIE_QODER2API_KEY`, `VOIE_WINDSURFAPI_KEY`,
`VOIE_COPILOT_API_KEY`, `VOIE_CURSOR_API_PROXY_KEY`, `VOIE_CODEARTS2API_KEY`,
`VOIE_CLI2API_KEY`, `VOIE_WORKBUDDY2API_KEY`, `VOIE_FREELLMPOOL_KEY`,
`VOIE_FREE_LLM_ROUTER_KEY`, `VOIE_OLLAMA_KEY`, `VOIE_LMSTUDIO_KEY`,
`VOIE_LLAMACPP_KEY`, `VOIE_9ROUTERX_KEY`, `VOIE_FREELLMAPI_KEY`, and
`VOIE_PPLX_PROXY_KEY`.

Additional overrides: `VOIE_OPENCODE_GATEWAY_URL` (local gateway base URL),
`KIRO_GATEWAY_DIR` (ACP bridge checkout), and `VOIE_CLINE_FREE_FEED_URL`
(free-model feed).

## Logs

`voie chat` writes progress to stderr. HTTP and MCP logs carry a shared request
ID. Logs omit query values, headers, prompts, answers, bodies, token counts,
and client IPs. Set `DEBUG=true` when you need diagnostics; upstream providers
may then log their own response bodies.
