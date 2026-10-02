# Model Fallback and Retry Specification

**Status:** Approved for implementation
**Date:** 2026-10-01
**Scope:** Configurable model retries and equivalent-model fallback across classic, automatic, and ensemble completions, exposed consistently through CLI, MCP, and HTTP API.

## Goal

When a selected model cannot complete a request, retry it within a bounded policy and then try a configured or demonstrably equivalent model. Keep selection, execution, failure classification, and attempt reporting in the shared application service so all transports follow the same behavior.

## Existing behavior

- `internal/app.Service` selects a provider/model and directly returns provider call failures. Classic completion does not switch models.
- Automatic routing chooses one candidate before making a provider call. If that call fails, it returns the error.
- Ensemble sends requests to its selected candidates in parallel. It synthesizes when at least two succeed; otherwise the ensemble fails. Failed candidates are not replaced.
- HTTP, CLI, and MCP translate their own inputs into `app.CompletionRequest`; the app service owns the completion workflow.
- `routing.json` already stores model descriptions/capabilities and task preferences. Runtime configuration currently comes from environment variables and that routing file.

## Goals

1. Add bounded retries for the selected model and a bounded set of alternate models.
2. Allow an ordered fallback list to be configured per primary model and overridden on an individual request.
3. Infer fallback candidates only when the service can establish that they fit the same task and declared capability requirements.
4. Apply the same policy to classic, auto, and ensemble calls, including the ensemble synthesizer.
5. Expose policy defaults and per-request overrides through CLI, MCP, and HTTP without duplicating fallback logic in transports.
6. Report attempts and their outcomes through the existing routing metadata and CLI progress path.

## Non-goals

- Probing every provider/model in advance or maintaining a live availability database.
- Retrying malformed input, unsupported models, or user cancellation.
- Silently changing an explicitly selected model when no configured or verifiably compatible fallback exists.
- Streaming partial output from one model and then switching to another after that output has been delivered.
- Changing provider catalog declarations or promising that a provider's static `Working` flag guarantees live availability.

## Policy model and precedence

The shared policy has these fields:

| Field | Meaning |
| --- | --- |
| `enabled` | Enables retries and fallback. Defaults to `true`; callers can disable it per request. |
| `max_retries` | Extra attempts on the same model after its initial attempt. Defaults to `1`. |
| `max_fallback_models` | Maximum number of distinct alternate models attempted for one completion. Defaults to `1`. |
| `models` | Optional ordered model IDs to try after a given primary model. |

The default retry count and fallback limit preserve bounded latency while providing a useful first fallback. `enabled` false disables both retries and fallback, restoring the current single-call behavior. `max_retries` set to zero disables same-model retries, and `max_fallback_models` set to zero disables fallback while leaving retries active. Values must be non-negative; `max_retries` is capped at 3 and `max_fallback_models` at 3 to keep request amplification bounded.

Policy precedence is:

1. Per-request values override the global defaults field by field.
2. An explicitly supplied per-request fallback model list replaces the configured list for that request; an explicitly empty list clears it.
3. Otherwise, use the ordered per-primary model list from `routing.json`.
4. If no explicit list applies, infer candidates from the strategy's eligible candidate set. Classic inference requires the primary model to declare at least one capability, and every declared primary capability must also be declared by the fallback. If compatibility cannot be established, do not infer a fallback.

An explicit fallback list is an operator/user declaration of equivalence, but every ID must still be registered, distinct from the primary, and usable under provider and strategy constraints. Unknown IDs, duplicates, self-reference, and malformed policy fail validation rather than being ignored.

Example routing policy:

```json
{
  "fallback": {
    "enabled": true,
    "max_retries": 1,
    "max_fallback_models": 2,
    "models": {
      "primary-model": ["fallback-one", "fallback-two"]
    }
  },
  "models": {
    "primary-model": {"capabilities": ["coding", "reasoning"]},
    "fallback-one": {"capabilities": ["coding", "reasoning", "writing"]},
    "fallback-two": {"capabilities": ["coding", "reasoning"]}
  },
  "tasks": {}
}
```

The environment variables `FALLBACK_ENABLED`, `FALLBACK_MAX_RETRIES`, and `FALLBACK_MAX_MODELS` override the global scalar defaults. Per-model ordered lists remain in the routing file. The default routing-file path and `ROUTING_CONFIG_PATH` behavior remain unchanged.

## Failure classification and attempt behavior

The provider boundary must expose typed failure information sufficient to distinguish:

- **Retryable on the same model:** network/connectivity failures, request timeouts while the caller context remains active, HTTP 408, HTTP 429, and HTTP 5xx.
- **Fallback eligible without retrying the same model:** a provider reports that the selected model is unavailable (for example, a model-specific HTTP 404).
- **Not retryable or fallback eligible:** invalid input, unknown model/provider, provider/model mismatch, authentication/configuration failures, caller cancellation, or service deadline exhaustion.

Provider adapters must wrap status/network details in a shared typed error; the app layer must not classify failures by searching human-readable error strings. If an adapter cannot provide a more specific class, its error is treated as non-retryable and returned as an upstream failure.

The typed error carries the failure category and the upstream HTTP status only. It must not carry the upstream response body. The body is removed from provider error messages entirely, so it is absent from the error returned to API clients, from attempt metadata, and from service and transport logs. A provider may record the body only in provider-scoped logs that are emitted when `DEBUG` is enabled, and it must do so through the existing debug logger rather than through the error value. Provider request logs follow the same rule: model, provider, status and outcome only.

For a retryable failure, make up to `max_retries` additional calls to the same model, subject to the completion's existing overall timeout. Once same-model retries are exhausted, or a model-unavailable failure occurs, try fallback models in precedence order up to `max_fallback_models`. Stop on first successful non-empty completion. Never retry after cancellation or after the request context expires. Retries and fallback calls use the same messages and request context; no prompt or intermediate answer is sent to another candidate.

For streaming requests, retries and fallback are allowed only until the first content chunk has been delivered to the caller. After the first chunk, a failure terminates the stream with the existing provider-error event; the service must not splice a second model's answer into a partial first answer.

## Strategy behavior

### Classic

The requested model remains the primary. If its call is eligible for retry/fallback, apply the shared policy. When `provider` is explicitly pinned, fallback models must also be supported by that provider; otherwise return the original failure rather than violating the provider constraint. The final response's `model` and `provider` identify the model that actually succeeded.

### Auto

The router still selects the primary model as today. Fallback candidates must be eligible for the same task, provider filter, and required capabilities, and exclude the primary and every model already attempted. Order them by the request-level explicit list, the primary model's configured list, the task policy's preferred-model ordering, then stable model-ID ordering; discard any explicit candidate that violates the auto route's constraints. A failed primary must not cause a second router classification call. If all eligible attempts fail or the fallback budget is exhausted, return the final typed provider error and record attempt outcomes in service logs.

### Ensemble

Retry each initially selected candidate independently using the shared retry count. After parallel initial attempts and retries, replace failed candidate slots in deterministic order, using the request-level explicit list, each failed primary's configured fallback list, then task/capability-compatible candidates. `max_fallback_models` is shared across the whole ensemble completion. Replace failed slots only while replacement is needed: stop once the number of successful candidates equals the originally selected candidate count, or when the shared fallback budget is exhausted. Synthesis proceeds when at least two distinct candidate models have succeeded, preserving the existing minimum; otherwise return the existing insufficient-results error and record attempt outcomes in service logs.

The synthesis model uses the same retry/fallback policy, but it is never eligible as an ensemble candidate. If synthesis ultimately fails, the completion fails; candidate answers are not returned as a substitute for synthesis.

## Shared request and response contract

Add an optional `fallback` policy override to `app.CompletionRequest` with pointer/presence semantics so omitted fields inherit global policy and explicit zero/empty values can override it. Its model list also needs presence semantics: omitted inherits configured lists; an empty list clears them. It contains `enabled`, `max_retries`, `max_fallback_models`, and optional ordered `models`.

Extend `RoutingInfo` with an additive `attempts` array. Each entry records `model`, `provider`, `attempt` (1-based ordinal within the completion), `outcome`, and `duration_ms`:

| `outcome` | Meaning |
| --- | --- |
| `succeeded` | The attempt returned a non-empty completion. |
| `retryable_failure` | A transient failure; the same model may be retried. |
| `unavailable` | The provider reported that model unavailable; fallback proceeds without another attempt on that model. |
| `failed` | A permanent failure, or the final attempt of a model that will not be retried. |

Existing `RoutedModel` information remains available and compatible, and `attempts` is omitted when no attempt was recorded. Do not include prompts, responses, credentials, or provider response bodies in attempt metadata or logs. Successful responses expose the full attempt list. Failed requests preserve existing transport error shapes in this release; CLI progress and service logs report the models tried and final failure category.

The API's normal JSON response continues to identify the actual successful model/provider. SSE emits only the chosen model's content and reports fallback decisions in initial routing metadata/progress before content starts. MCP structured output includes the same routing metadata. CLI continues to write only the final answer to stdout; its existing stderr progress reports retries and fallback choices.

## Transport controls

All transports translate to the common request override and rely on `app.Service` to execute it:

- **HTTP:** accept an optional `fallback` JSON object on `/v1/chat/completions` with the four policy fields.
- **MCP:** add the same optional object to `chat_completion` input and preserve the structured routing output.
- **CLI:** add `--fallback`/`--no-fallback`, `--fallback-retries`, `--fallback-limit`, and repeatable `--fallback-model` flags to `chat`. A supplied `--fallback-model` list replaces the configured list for that command. Flag validation uses the same bounds as the application service.
- **Global configuration:** add the three scalar environment variables above and the `fallback` object to the existing routing policy JSON. Per-model lists are defined by model ID in that object.

There is no separate fallback endpoint or strategy. Existing calls that omit overrides use the configured global policy.

## Validation and errors

- Validate global policy at runtime startup against registered model IDs and configured bounds.
- Validate request overrides before model selection or provider calls.
- Ignore no invalid explicit fallback entries; return an input/configuration error naming the invalid entry without exposing provider secrets.
- Preserve current error categories when the policy is disabled or no fallback is available. When attempts were made, return the final failure category and attach all attempts to routing metadata/log context.
- Caller cancellation and overall deadline exhaustion stop all retries, candidate replacements, and synthesis work.
- Repository checks (`go vet`, `gofmt -l`, `go test ./...`) must run in CI on every push and pull request so regressions block a release before a tag is cut.

## Compatibility

- Existing request fields and response fields remain valid; new request and response properties are optional/additive.
- Calls use the configured defaults when a request does not specify an override.
- `fallback.enabled` defaults to true with one retry and one fallback model; setting it false restores the current single-model failure behavior.
- Models without declared equivalence metadata keep current behavior unless an explicit fallback list is configured.
- CLI answer stdout, API response framing, MCP tool names, and authentication behavior remain unchanged.

## Acceptance criteria

1. A classic request retries a transient primary failure only up to the configured count, then succeeds on the first eligible fallback or returns the final error.
2. A model-specific unavailable error proceeds directly to fallback without pointless retries of that model.
3. Invalid requests, unsupported models, authentication/configuration failures, cancellation, and exhausted deadlines do not trigger retries or fallback.
4. An explicitly pinned provider is never bypassed by fallback selection.
5. Auto fallback candidates satisfy the original task/provider/capability constraints, follow the declared precedence, and are attempted without rerunning the router.
6. Ensemble retries failed candidates, uses a shared fallback budget to replace failed slots, and synthesizes only with at least two distinct successful candidates.
7. The synthesizer can retry and fall back without ever being selected as an ensemble candidate.
8. Streaming may switch models only before the first content chunk; it never joins partial answers from different models.
9. CLI, HTTP, and MCP accept equivalent policy values and successful fallback responses report the same final model and attempt sequence.
10. Global defaults, request overrides, explicit empty lists, invalid IDs, duplicate IDs, and bounds are validated consistently.
11. Attempt metadata and logs include model/provider/outcome/timing only and never contain request or response content.
12. Existing classic/auto/ensemble behavior and interface contracts remain covered when fallback is disabled.
