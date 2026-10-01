# Request Logging and CLI Progress Specification

**Status:** Draft for user review  
**Date:** 2026-10-01  
**Scope:** CLI completion progress, HTTP request lifecycle logs, and MCP tool-call logs.

## Goal

Make long-running model and API operations visibly active and diagnosable without changing the completion contract or mixing progress output into command results. All terminal output is monochrome.

## Existing behavior

- `utils.RequestLogger` can emit start and end messages, but HTTP handlers do not use it.
- Runtime startup and MCP startup messages use `utils`, which the executable directs to stderr and configures without color.
- MCP tool handlers currently return structured results but do not log individual tool calls.
- CLI `chat` blocks on `app.Service.Complete` and writes the final answer to stdout.
- App routing knows when it classifies a request, calls models, fans out an ensemble, synthesizes a result, and persists a conversation.

## CLI progress

### Terminal behavior

- Use `github.com/benoitpetit/agent-spinner` for animated progress when the command's stderr is an interactive terminal.
- Use its terminal renderer on stderr, with a monochrome spinner style. Do not emit color escape sequences.
- When stderr is not a terminal, emit concise line-oriented progress events on stderr rather than animated cursor updates. This keeps long operations observable in redirected or scripted runs.
- Keep final answer text as the only successful `voie chat` output on stdout. Errors remain errors and are reported by the existing command path.
- Stop the spinner on success, failure, deadline, and cancellation. Final progress should include the selected model/provider when known, but must not include prompt or response text.

### Progress events

Add a non-serialized optional progress observer to the common app completion request. It is a local callback, never accepted from HTTP JSON or MCP input. Emit events at these transitions:

1. Completion accepted and strategy identified.
2. Automatic router classification started/completed, when a router call is needed.
3. Candidate provider calls started and completed; ensemble updates identify each model and success/failure without showing its answer.
4. Ensemble synthesis started/completed.
5. Conversation turn persistence started/completed, when a conversation ID was supplied.

Classic requests should show that the configured/explicit model is being called. Automatic requests should show the inferred model and provider once selected. The CLI observer updates the spinner message; it does not own model selection or transport behavior.

Progress callbacks must not change success/failure outcomes. A callback panic must not crash a completion; the implementation should isolate callback panics at the observer boundary.

## HTTP request logging

- Wrap the HTTP handler with request lifecycle logging so every request, including authentication and routing failures, has a start and completion record.
- Each record includes a short request ID, method, URL path, status code, and elapsed duration. Do not log query strings, headers, request bodies, prompts, response bodies, tokens, or client IP addresses.
- For chat completions, log the strategy/model routing summary when available, plus typed failure category on error. Do not log message contents.
- Send records to stderr through the existing logging utility. Emit no colors.
- Preserve `http.Flusher` behavior so SSE events remain immediately flushed. The logging wrapper must record the final status without buffering the response.
- Log a fallback server error status if a handler exits without writing a status. Do not write a second response or alter the handler's status/body.

## MCP tool logging

- Wrap each registered MCP tool handler with a common lifecycle logger.
- Record request ID, tool name, start, elapsed time, and success/error. Do not log tool arguments, completion messages, conversation content, or serialized structured results.
- Use the existing stderr logger only; stdout remains reserved for MCP protocol messages.
- Keep MCP tool names and schemas unchanged by the logging wrapper.

## Shared logging rules

- Keep logs human-readable, monochrome, and safe to show in terminal or service logs.
- Never record prompts, assistant answers, authorization tokens, MCP arguments, or arbitrary URL query parameters.
- Request IDs are per HTTP request and per MCP tool invocation. A CLI completion may use a separate local progress context; no ID needs to be exposed in the answer.
- Logging failures must not change request results.
- Default log level remains INFO. Detailed provider selection and phase messages are INFO progress/log events; verbose upstream implementation details remain behind DEBUG.

## Compatibility

- HTTP JSON and SSE response schemas remain unchanged by logging; existing optional routing metadata continues to be populated as before.
- API authentication, status mapping, timeout behavior, and streaming framing remain unchanged.
- MCP tool inputs, outputs, and protocol transport remain unchanged.
- CLI `chat` stdout stays answer-only on success; progress is isolated to stderr.
- Non-TTY CLI invocations remain machine-usable because progress never goes to stdout.

## Acceptance criteria

1. A TTY `voie chat` displays a monochrome animated indicator and meaningful stage updates during routing, provider work, and synthesis.
2. A redirected/non-TTY `voie chat` emits line-oriented progress only to stderr and returns the same answer-only stdout as before.
3. HTTP request logs include method, path, request ID, final status, and duration for success and failure; no request content or query values appear.
4. SSE still flushes each chunk as it is produced while logging the final status and duration.
5. MCP logs tool start/end/failure and duration on stderr while stdout remains valid MCP protocol output.
6. No output path emits color codes.
7. Observer callback failures and logging failures do not alter completion outcomes.
8. Existing classic API/CLI/MCP tests remain valid; tests cover each new logging and progress path.

## Dependency reference

The proposed CLI animation uses [Agent Spinner](https://github.com/benoitpetit/agent-spinner), whose documented API supports start/update/stop/fail, provides a terminal renderer that writes to stderr, and lists no external dependencies.

