# OpenCode gateway setup

`voie` can relay to OpenCode's free Zen catalogue and to the Go catalogue through
a local `opencode-zen` gateway. The gateway holds the upstream credentials;
applications calling `voie` never provide an account or an API key.

Two providers use this gateway:

| Provider | Model IDs | Upstream credential required in the gateway |
| --- | --- | --- |
| `opencode-zen` | `oc2a-*` | Existing OpenCode Zen key (or the anonymous lane, which is heavily restricted) |
| `opencode-go` | `oc2ago-*` | **Go API key or Go OAuth — one of the two is mandatory** |

Go-catalogue IDs are included in an OpenCode Go subscription. They are
"free" only from the caller's point of view: the subscription is billed to the
gateway owner, not to the application calling `voie`.

## The local key is not an OpenCode key

`OPENCODE_ZEN_KEY` (default `voie-local`) is the gateway's own loopback key.
`voie` sends it only to `http://127.0.0.1:8090` as
`Authorization: Bearer <local key>`. It is never forwarded to OpenCode. Its only
purpose is to stop other local processes from using your gateway. If you change
`server_keys` in the gateway configuration, set the same value in
`OPENCODE_ZEN_KEY`.

The real upstream credential (your Zen key, a Go key, or an OAuth connection)
lives in the gateway configuration file, `~/.config/opencode-zen/config.json`,
with mode `600`. Keep that file private and out of version control.

## Free Zen models (`oc2a-*`)

The gateway was configured with `zen_keys` copied from an existing
`opencode` CLI login, `anonymous: true` as a fallback, and `models.free_only:
true`. No further action is needed. Availability is capped by OpenCode's daily
free quota, which resets at 00:00 UTC.

```bash
voie models --json | jq '.data[] | select(.id | startswith("oc2a-"))'
voie chat --model oc2a-hy4-flash-free "Hello"
```

## Go models (`oc2ago-*`): choose one option

Both options end with the same result: `go_keys` or `go_oauth` is present in the
gateway configuration, and `oc2ago-*` IDs become callable. Without **one** of
them those IDs return an upstream authentication error.

### Option A — Go API key

1. Edit `~/.config/opencode-zen/config.json` and put your Go key in `go_keys`:

   ```json
   "go_keys": ["your-go-api-key"],
   ```

2. Reload the gateway configuration from the management UI or restart the
   gateway:

   ```bash
   ~/.local/bin/opencode-zen -config ~/.config/opencode-zen/config.json
   ```

3. Check the resolver reports a Go credential, then call a model:

   ```bash
   curl -s http://127.0.0.1:8090/healthz | jq '.keys.go'
   voie chat --model oc2ago-gpt-6-luna "Hello"
   ```

### Option B — Go device OAuth (no key in the config file)

The management WebUI listens on `127.0.0.1:8091` only. Log in with the `admin`
user and the password stored in the gateway configuration file.

1. Open `http://127.0.0.1:8091`.
2. Go to **Models & Quota** and start the **Go** device login.
3. Open the verification URL in your browser, enter the displayed `user_code`,
   authorize the request and select the organization/workspace if asked.
4. The gateway stores and refreshes `go_oauth` itself. No API key is written to
   the configuration file.

Then verify and call:

```bash
curl -s http://127.0.0.1:8090/healthz | jq '.keys.go'
voie chat --model oc2ago-gpt-6-luna "Hello"
```

## Troubleshooting

- `401` from `127.0.0.1:8090` → `OPENCODE_ZEN_KEY` does not match the gateway's
  `server_keys`.
- `429 Free usage limit reached` on `oc2a-*` → the daily free quota is spent;
  wait for 00:00 UTC.
- Authentication errors on `oc2ago-*` → neither `go_keys` nor `go_oauth` is
  configured, or the Go session needs to be reconnected in the WebUI.
