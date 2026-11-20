# voie-ai

Installs the [voie](https://github.com/benoitpetit/voie) CLI binary from the latest GitHub release. No compiler, no Node runtime for voie itself, no per-version package updates.

```sh
npm install -g voie-ai
voie models --json
```

Or run it once without installing:

```sh
npx voie-ai models
npx voie-ai chat --model MODEL_ID "Hello"
```

The installer picks the asset for the current OS and CPU (Linux and macOS amd64/arm64, Windows amd64), checks its SHA-256 against `checksums.txt`, and stores the binary under `~/.cache/voie-ai/<release tag>/`. The launcher runs the newest cached binary. Set `VOIE_CACHE_DIR` to change that location; set `GITHUB_TOKEN` if GitHub API rate limits affect installation.

```
npm test        # unit tests, no network
npm pack        # build the tarball for publishing
```
