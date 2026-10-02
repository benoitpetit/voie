package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// resolvePolicy merges the global fallback policy with the request override.
// Pointer fields override the base policy; an explicitly supplied Models list
// is validated but not stored here so eligibleFallbacks keeps the request
// precedence.
func (s *Service) resolvePolicy(request CompletionRequest) (FallbackPolicy, error) {
	policy := s.FallbackPolicy()
	override := request.Fallback
	if override == nil {
		return policy, nil
	}
	if override.Enabled != nil {
		policy.Enabled = *override.Enabled
	}
	if override.MaxRetries != nil {
		if *override.MaxRetries < 0 || *override.MaxRetries > 3 {
			return policy, appError(ErrInvalidInput, fmt.Sprintf("fallback max_retries must be between 0 and 3, got %d", *override.MaxRetries), nil)
		}
		policy.MaxRetries = *override.MaxRetries
	}
	if override.MaxFallbackModels != nil {
		if *override.MaxFallbackModels < 0 || *override.MaxFallbackModels > 3 {
			return policy, appError(ErrInvalidInput, fmt.Sprintf("fallback max_fallback_models must be between 0 and 3, got %d", *override.MaxFallbackModels), nil)
		}
		policy.MaxFallbackModels = *override.MaxFallbackModels
	}
	if override.Models != nil {
		seen := make(map[string]struct{}, len(override.Models))
		for _, id := range override.Models {
			id = strings.TrimSpace(id)
			if s.registry.GetForModel(id) == nil {
				return policy, appError(ErrUnknownModel, fmt.Sprintf("fallback model %q is not supported", id), nil)
			}
			if _, duplicate := seen[strings.ToLower(id)]; duplicate {
				return policy, appError(ErrInvalidInput, fmt.Sprintf("fallback model %q is listed more than once", id), nil)
			}
			seen[strings.ToLower(id)] = struct{}{}
		}
	}
	return policy, nil
}

// classifyFailure returns the failure category of an error, treating anything
// that is not a typed ProviderFailure as permanent (not retry-eligible).
func classifyFailure(err error) FailureCategory {
	var failure *ProviderFailure
	if errors.As(err, &failure) {
		return failure.Category
	}
	return FailurePermanent
}

func outcomeFor(err error) string {
	switch classifyFailure(err) {
	case FailureTransient:
		return OutcomeRetryableFailure
	case FailureUnavailable:
		return OutcomeUnavailable
	default:
		return OutcomeFailed
	}
}

// eligibleFallbacks returns ordered, distinct fallback model IDs for the
// primary model under the given strategy. Precedence is the request-level
// explicit list, the configured per-primary list, then inference. An
// explicitly empty request list clears eligibility. An exhausted set yields
// nil, never an error.
func (s *Service) eligibleFallbacks(request CompletionRequest, primaryModel string, pin string, policy FallbackPolicy, strategy Strategy) ([]string, error) {
	var candidates []string
	if request.Fallback != nil && request.Fallback.Models != nil {
		if len(request.Fallback.Models) == 0 {
			return nil, nil
		}
		candidates = request.Fallback.Models
	} else if configured, ok := s.configuredFallbacks(policy, primaryModel); ok {
		candidates = configured
	} else if strategy == StrategyClassic {
		candidates = s.classicFallbackCandidates(primaryModel)
	} else {
		return nil, nil
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, id := range candidates {
		normalized := strings.ToLower(strings.TrimSpace(id))
		if normalized == "" || normalized == strings.ToLower(strings.TrimSpace(primaryModel)) {
			continue
		}
		if _, duplicate := seen[normalized]; duplicate {
			continue
		}
		provider := s.registry.GetForModel(id)
		if provider == nil || !provider.GetInfo().Working {
			continue
		}
		if pin != "" && !strings.EqualFold(provider.GetInfo().Name, pin) {
			continue
		}
		if strategy == StrategyClassic && !s.classicCompatible(primaryModel, id) {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out, nil
}

func (s *Service) configuredFallbacks(policy FallbackPolicy, primaryModel string) ([]string, bool) {
	for key, candidates := range policy.Models {
		if strings.EqualFold(strings.TrimSpace(key), strings.TrimSpace(primaryModel)) {
			return candidates, true
		}
	}
	return nil, false
}

func (s *Service) classicCompatible(primaryModel, candidate string) bool {
	primary := s.modelDescriptor(primaryModel)
	if len(primary.Capabilities) == 0 {
		return true
	}
	return hasCapabilities(s.modelDescriptor(candidate).Capabilities, primary.Capabilities)
}

// classicFallbackCandidates infers fallback models when no explicit list is
// configured: candidates must declare every capability declared by the
// primary, and the primary itself must declare at least one capability.
func (s *Service) classicFallbackCandidates(primaryModel string) []string {
	primaryDescriptor := s.modelDescriptor(primaryModel)
	if len(primaryDescriptor.Capabilities) == 0 {
		return nil
	}
	excluded := map[string]struct{}{
		strings.ToLower(strings.TrimSpace(primaryModel)):            {},
		strings.ToLower(strings.TrimSpace(s.options.RouterModel)): {},
	}
	seen := make(map[string]struct{})
	candidates := make([]string, 0)
	for _, providerName := range s.registry.GetProviderNames() {
		provider := s.registry.Get(providerName)
		if !provider.GetInfo().Working {
			continue
		}
		for _, model := range provider.GetInfo().SupportedModels {
			key := strings.ToLower(strings.TrimSpace(model))
			if key == "" {
				continue
			}
			if _, skip := excluded[key]; skip {
				continue
			}
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			routed := s.registry.GetForModel(model)
			if routed == nil || !routed.GetInfo().Working {
				continue
			}
			if !hasCapabilities(s.modelDescriptor(model).Capabilities, primaryDescriptor.Capabilities) {
				continue
			}
			seen[key] = struct{}{}
			candidates = append(candidates, key)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
	return candidates
}

// callWithFallback runs the non-streaming completion with the shared retry and
// fallback policy. The first model is the primary; fallback candidates are
// tried in eligibility order up to MaxFallbackModels. Same-model retries only
// happen on transient failures while the caller context stays active.
func (s *Service) callWithFallback(ctx context.Context, request CompletionRequest, primary Provider, primaryModel string, pin string, policy FallbackPolicy) (*ChatCompletionResponse, []Attempt, error) {
	if !policy.Enabled {
		policy.MaxRetries, policy.MaxFallbackModels = 0, 0
	}
	fallbacks, err := s.eligibleFallbacks(request, primaryModel, pin, policy, StrategyClassic)
	if err != nil {
		return nil, nil, err
	}
	models := append([]string{strings.ToLower(strings.TrimSpace(primaryModel))}, fallbacks...)
	attempts := make([]Attempt, 0, 1)
	var lastErr error
	ordinal := 0
	for index, candidate := range models {
		if ctx.Err() != nil {
			return nil, attempts, s.contextOrUpstreamError(ctx, ctx.Err())
		}
		provider := primary
		if index > 0 {
			provider = s.registry.GetForModel(candidate)
			if provider == nil || !provider.GetInfo().Working {
				continue
			}
		}
		providerName := provider.GetInfo().Name
	retryLoop:
		for retry := 0; ; retry++ {
			if ctx.Err() != nil {
				return nil, attempts, s.contextOrUpstreamError(ctx, ctx.Err())
			}
			ordinal++
			started := time.Now()
			request.emitProgress(ProgressEvent{Stage: "model", Message: "Calling model " + candidate, Model: candidate, Provider: providerName, Status: "started"})
			response, callErr := provider.ChatCompletion(ctx, request.Messages, candidate)
			attempts = append(attempts, Attempt{Model: candidate, Provider: providerName, Attempt: ordinal, Outcome: OutcomeFailed, DurationMillis: time.Since(started).Milliseconds()})
			attempt := &attempts[len(attempts)-1]
			if callErr == nil {
				if response == nil || len(response.Choices) == 0 {
					request.emitProgress(ProgressEvent{Stage: "model", Message: "Model call failed", Model: candidate, Provider: providerName, Status: "failed"})
					return nil, attempts, appError(ErrUpstream, fmt.Sprintf("provider %q returned an empty completion", providerName), nil)
				}
				attempt.Outcome = OutcomeSucceeded
				response.Provider = providerName
				request.emitProgress(ProgressEvent{Stage: "model", Message: "Model call completed", Model: candidate, Provider: providerName, Status: "succeeded"})
				return response, attempts, nil
			}
			request.emitProgress(ProgressEvent{Stage: "model", Message: "Model call failed", Model: candidate, Provider: providerName, Status: "failed"})
			attempt.Outcome = outcomeFor(callErr)
			lastErr = callErr
			switch classifyFailure(callErr) {
			case FailurePermanent:
				return nil, attempts, s.contextOrUpstreamError(ctx, callErr)
			case FailureUnavailable:
				break retryLoop
			default:
				if retry < policy.MaxRetries {
					continue
				}
				break retryLoop
			}
		}
		if index >= policy.MaxFallbackModels {
			break
		}
	}
	return nil, attempts, s.contextOrUpstreamError(ctx, lastErr)
}

// streamWithFallback runs the streaming completion with the shared policy.
// Models may only switch before the first content chunk is delivered; after
// that a failure terminates with the upstream error, never splicing answers.
func (s *Service) streamWithFallback(ctx context.Context, request CompletionRequest, primary Provider, primaryModel string, pin string, policy FallbackPolicy, onChunk func(string)) ([]Attempt, error) {
	if !policy.Enabled {
		policy.MaxRetries, policy.MaxFallbackModels = 0, 0
	}
	fallbacks, err := s.eligibleFallbacks(request, primaryModel, pin, policy, StrategyClassic)
	if err != nil {
		return nil, err
	}
	models := append([]string{strings.ToLower(strings.TrimSpace(primaryModel))}, fallbacks...)
	attempts := make([]Attempt, 0, 1)
	var lastErr error
	ordinal := 0
	delivered := false
	for index, candidate := range models {
		if ctx.Err() != nil {
			return attempts, s.contextOrUpstreamError(ctx, ctx.Err())
		}
		provider := primary
		if index > 0 {
			provider = s.registry.GetForModel(candidate)
			if provider == nil || !provider.GetInfo().Working {
				continue
			}
		}
		providerName := provider.GetInfo().Name
	retryLoop:
		for retry := 0; ; retry++ {
			if ctx.Err() != nil {
				return attempts, s.contextOrUpstreamError(ctx, ctx.Err())
			}
			ordinal++
			started := time.Now()
			request.emitProgress(ProgressEvent{Stage: "model", Message: "Streaming from " + candidate, Model: candidate, Provider: providerName, Status: "started"})
			streamErr := provider.ChatCompletionStream(ctx, request.Messages, candidate, func(chunk string) {
				delivered = true
				onChunk(chunk)
			})
			attempts = append(attempts, Attempt{Model: candidate, Provider: providerName, Attempt: ordinal, Outcome: OutcomeFailed, DurationMillis: time.Since(started).Milliseconds()})
			attempt := &attempts[len(attempts)-1]
			if streamErr == nil {
				attempt.Outcome = OutcomeSucceeded
				request.emitProgress(ProgressEvent{Stage: "model", Message: "Model stream completed", Model: candidate, Provider: providerName, Status: "succeeded"})
				return attempts, nil
			}
			request.emitProgress(ProgressEvent{Stage: "model", Message: "Model stream failed", Model: candidate, Provider: providerName, Status: "failed"})
			attempt.Outcome = outcomeFor(streamErr)
			lastErr = streamErr
			if delivered {
				return attempts, s.contextOrUpstreamError(ctx, streamErr)
			}
			switch classifyFailure(streamErr) {
			case FailurePermanent:
				return attempts, s.contextOrUpstreamError(ctx, streamErr)
			case FailureUnavailable:
				break retryLoop
			default:
				if retry < policy.MaxRetries {
					continue
				}
				break retryLoop
			}
		}
		if index >= policy.MaxFallbackModels {
			break
		}
	}
	return attempts, s.contextOrUpstreamError(ctx, lastErr)
}