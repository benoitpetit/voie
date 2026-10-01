# Request Logging and CLI Progress Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make CLI, HTTP API, and MCP model operations observable while keeping answers/protocol output unchanged and monochrome.

**Architecture:** Add a local-only progress callback to `app.CompletionRequest` and emit lifecycle events from both buffered and streaming service paths. The CLI renders those events to stderr with Agent Spinner on a TTY and plain lines otherwise; HTTP and MCP add request lifecycle logging at their transport boundaries using the existing logger.

**Tech Stack:** Go 1.25.5, Cobra, MCP Go SDK, Agent Spinner, `net/http`, existing `utils` logger.

**Spec:** `docs/superpowers/specs/2026-10-01-request-logging-cli-progress-design.md`

## Global Constraints

- Keep successful `voie chat` stdout limited to the completion answer.
- Do not log prompts, assistant answers, auth tokens, request bodies, headers, query strings, MCP arguments/results, tokens, or client IPs.
- All logs and progress output are monochrome and go to stderr.
- Preserve HTTP/SSE status, response body, schemas, and immediate flush behavior.
- MCP tool names and input/output schemas remain unchanged.
- Progress/log callbacks and logging failures must not change operation outcomes.

## Review Focus

- A panic in a local progress callback must not fail a completion; cover in Task 1.
- Streaming completions must report phases and still persist conversations; cover in Task 1.
- HTTP auth failures, implicit 200 responses, and SSE flushes must be logged without body/query leakage; cover in Task 2.
- Redirected CLI output must remain answer-only on stdout and emit readable progress on stderr; cover in Task 3.
- MCP tool errors and sensitive arguments/results must not leak into logs or stdout protocol output; cover in Task 4.

---

### Task 1: App completion progress events

**Files:**
- Modify: `internal/app/service.go`, `internal/app/ensemble.go`
- Test: `internal/app/service_test.go`, `internal/app/ensemble_test.go`

**Interfaces:**
- Produces `type ProgressEvent struct { Stage string; Message string; Model string; Provider string; Status string }` and `CompletionRequest.OnProgress func(ProgressEvent)`.
- Events use the stages `accepted`, `routing`, `model`, `synthesis`, and `conversation`; statuses are `started`, `succeeded`, or `failed`.
- The callback is local-only and is not added to transport request structs or JSON.

- [x] Add tests for classic, automatic, ensemble, persistence, and streaming progress events, plus callback panic isolation.
- [x] Run `GOCACHE=/tmp/voie-go-cache go test ./internal/app -run 'Progress' -count=1`; observe failures because the callback/event API is absent.
- [x] Add the callback and safe event emitter; instrument buffered/streaming route, provider, ensemble synthesis, and persistence transitions without including prompt or answer content.
- [x] Rerun the focused tests and `GOCACHE=/tmp/voie-go-cache go test ./internal/app`.
- [x] Commit as `feat: expose completion progress events`.

### Task 2: HTTP request lifecycle and routing logs

**Files:**
- Create: `internal/transport/httpapi/logging.go`
- Modify: `internal/transport/httpapi/handler.go`, `utils/logger.go`
- Test: `internal/transport/httpapi/handler_test.go`, `utils/logger_test.go`

**Interfaces:**
- `requestLogging(next http.Handler) http.Handler` wraps the complete CORS/auth handler chain.
- A status recorder tracks first status and bytes written and preserves `http.Flusher`.
- Existing `utils.NewRequestLogger` emits start and final records with the same short ID, method, path, status, and elapsed time; the query and IP arguments are never populated or logged.

- [x] Add tests for query/header/body redaction and SSE flush/status capture.
- [x] Run `GOCACHE=/tmp/voie-go-cache go test ./internal/transport/httpapi -run 'RequestLog' -count=1`; observe the expected missing lifecycle records/flush behavior.
- [x] Implement the outer logging wrapper and safe status recorder; update `RequestLogger` so no-color is the default and the final record includes the request identity.
- [x] Add chat routing summary and typed error-category records without logging message contents.
- [x] Rerun focused tests and `GOCACHE=/tmp/voie-go-cache go test ./internal/transport/httpapi ./utils`.
- [x] Commit as `feat: log HTTP request lifecycles`.

### Task 3: Monochrome CLI progress display

**Files:**
- Create: `internal/cli/progress.go`
- Modify: `internal/cli/chat.go`, `go.mod`, `go.sum`
- Test: `internal/cli/cli_test.go`, `internal/cli/progress_test.go`

**Interfaces:**
- `newProgressReporter(stderr io.Writer, isTerminal bool) progressReporter` consumes `app.ProgressEvent` and exposes `Handle(app.ProgressEvent)` and `Finish(error)`.
- `runChatWithOptions` accepts an optional progress reporter internally; the public command uses `command.ErrOrStderr()` and detects whether that writer is a terminal.
- Agent Spinner handles TTY animation; non-TTY output is one concise line per event.

- [x] Add tests proving line-mode progress goes only to stderr, no ANSI escapes appear, stdout contains only the answer, and failures finish the reporter.
- [x] Run `GOCACHE=/tmp/voie-go-cache go test ./internal/cli -run 'Progress|Chat' -count=1`; observe failures because no reporter is connected.
- [x] Add the pinned Agent Spinner dependency and implement TTY/non-TTY reporters; connect `CompletionRequest.OnProgress` in the chat command.
- [x] Rerun focused tests and `GOCACHE=/tmp/voie-go-cache go test ./internal/cli`.
- [x] Commit as `feat: show CLI completion progress`.

### Task 4: MCP tool lifecycle logs

**Files:**
- Create: `internal/transport/mcp/logging.go`
- Modify: `internal/transport/mcp/server.go`
- Test: `internal/transport/mcp/server_test.go`

**Interfaces:**
- `logToolCall[I, O any](name string, handler func(context.Context, *mcp.CallToolRequest, I) (*mcp.CallToolResult, O, error))` wraps each registered typed handler.
- Start/end/failure records include a short invocation ID, tool name, and elapsed time, but omit handler arguments and returned values.

- [x] Add tests for successful and failed calls, secret markers in arguments/results, and unchanged tool registration/schema.
- [x] Run `GOCACHE=/tmp/voie-go-cache go test ./internal/transport/mcp -run 'ToolLifecycleLogs' -count=1`; observe missing records.
- [x] Implement the typed lifecycle wrapper and apply it to every registered MCP tool.
- [x] Route default utility logs to stderr so MCP protocol stdout remains reserved; verify with a logger test.
- [x] Rerun focused tests and `GOCACHE=/tmp/voie-go-cache go test ./internal/transport/mcp ./utils`.
- [x] Commit as `feat: log MCP tool calls`.

### Task 5: Documentation, full verification, and release

**Files:**
- Modify: `README.md` and relevant API/MCP/CLI documentation discovered during implementation.
- Include all implementation commits plus the existing queued commits on `main`.

- [x] Document stderr progress/log behavior, answer-only stdout, and redaction boundaries.
- [x] Run `GOCACHE=/tmp/voie-go-cache go test ./...`, `GOCACHE=/tmp/voie-go-cache go vet ./...`, and `CGO_ENABLED=0 GOCACHE=/tmp/voie-go-cache go build ./...` after the final changes; all three pass.
- [x] Review the release workflow and verify that tag `v0.0.2` triggers the intended release process; confirm the tag does not exist locally or on `origin`.
- [x] Complete a fresh whole-branch review and address the Important finding with RED→GREEN tests.
- [x] Commit documentation and review fixes.
- [x] Push `main` and annotated tag `v0.0.2` to `origin`; verify the release workflow and uploaded archives.
