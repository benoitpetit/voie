# voie-ai

![voie — free ai api](voie-banner.png)

[voie](https://github.com/benoitpetit/voie) is a free, OpenAI-compatible API relay. One Go executable forwards requests to public model providers and exposes the same registry through an HTTP API, a CLI, and an MCP server over stdio. Nothing is downloaded or run as a model on your machine.

This package installs the voie binary from the latest GitHub release. It has no native dependencies, no model files, and no version number to keep in sync with the CLI.

## Install

```sh
npm install -g voie-ai
voie models --json
```

Run it once without a global install:

```sh
npx voie-ai models
npx voie-ai chat --model MODEL_ID "Hello"
```

Start the local API, then point an OpenAI SDK at `http://127.0.0.1:8080/v1`:

```sh
voie serve
```

Model IDs are runtime data. Read them with `voie models --json` or `GET /v1/models` rather than copying them from documentation.

## What the installer does

- Reads the latest release from the GitHub API.
- Picks the asset for the current OS and CPU: Linux and macOS amd64 or arm64, Windows amd64.
- Checks the archive SHA-256 against the release `checksums.txt`.
- Stores the binary in `~/.cache/voie-ai/<release tag>/`. The launcher uses the newest cached version.
- Set `VOIE_CACHE_DIR` to move the cache, or `GITHUB_TOKEN` if the GitHub API rate limit gets in the way.

If support for a platform is missing, installation fails with the platform name in the message instead of installing something that will not run.

## Documentation

- [Project README](https://github.com/benoitpetit/voie#readme)
- [CLI guide](https://github.com/benoitpetit/voie/blob/main/docs/cli.md)
- [HTTP API reference](https://github.com/benoitpetit/voie/blob/main/api-reference.md)
- [MCP setup](https://github.com/benoitpetit/voie/blob/main/docs/mcp.md)
- [All releases](https://github.com/benoitpetit/voie/releases)

## Development

```sh
npm test        # unit tests, no network
npm pack        # build the tarball for publishing
```
