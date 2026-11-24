package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type CompletionRequest struct {
	Model          string                  `json:"model"`
	Provider       string                  `json:"provider,omitempty"`
	Strategy       Strategy                `json:"strategy,omitempty"`
	Task           string                  `json:"task,omitempty"`
	Models         []string                `json:"models,omitempty"`
	Ensemble       *EnsembleRequestOptions `json:"ensemble,omitempty"`
	Fallback       *FallbackOverride       `json:"fallback,omitempty"`
	ConversationID string                  `json:"conversation_id,omitempty"`
	Messages       []Message               `json:"messages"`
	N              int                     `json:"n,omitempty"`
	Options        RequestOptions          `json:"-"`
	OnProgress     func(ProgressEvent)     `json:"-"`
	progress       *progressEmitter
}

type ProgressEvent struct {
	Stage    string
	Message  string
	Model    string
	Provider string
	Status   string
}

type progressEmitter struct {
	mu       sync.Mutex
	callback func(ProgressEvent)
}

func (p *progressEmitter) emit(event ProgressEvent) {
	if p == nil || p.callback == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	defer func() { _ = recover() }()
	p.callback(event)
}

func (r CompletionRequest) emitProgress(event ProgressEvent) {
	r.progress.emit(event)
}

func initProgress(request *CompletionRequest) {
	if request.OnProgress != nil {
		request.progress = &progressEmitter{callback: request.OnProgress}
	}
}

type Strategy string

const (
	StrategyClassic  Strategy = "classic"
	StrategyAuto     Strategy = "auto"
	StrategyEnsemble Strategy = "ensemble"
)

func normalizeStrategy(strategy Strategy) Strategy {
	if strategy == "" {
		return StrategyClassic
	}
	return strategy
}

type ServiceOptions struct {
	DefaultProvider string
	RouterModel     string
	SynthesisModel  string
	RoutingPolicy   RoutingPolicy
	Fallback        *FallbackPolicy
	Conversations   ConversationStore
	Timeout         time.Duration
	HealthProbe     func(context.Context, string) bool
	HealthClient    *http.Client
	HealthTimeout   time.Duration
}

type Service struct {
	registry *Registry
	options  ServiceOptions
}

func NewService(registry *Registry, options ServiceOptions) (*Service, error) {
	if registry == nil {
		return nil, appError(ErrInvalidInput, "provider registry is required", nil)
	}
	options.DefaultProvider = strings.ToLower(strings.TrimSpace(options.DefaultProvider))
	options.RouterModel = strings.TrimSpace(options.RouterModel)
	options.SynthesisModel = strings.TrimSpace(options.SynthesisModel)
	if options.DefaultProvider != "" && registry.Get(options.DefaultProvider) == nil {
		return nil, appError(ErrUnknownProvider, fmt.Sprintf("configured default provider %q is unknown", options.DefaultProvider), nil)
	}
	if options.RouterModel != "" && registry.GetForModel(options.RouterModel) == nil {
		return nil, appError(ErrUnknownModel, fmt.Sprintf("configured router model %q is unknown", options.RouterModel), nil)
	}
	if options.SynthesisModel != "" && registry.GetForModel(options.SynthesisModel) == nil {
		return nil, appError(ErrUnknownModel, fmt.Sprintf("configured synthesis model %q is unknown", options.SynthesisModel), nil)
	}
	if options.Fallback == nil {
		options.Fallback = &FallbackPolicy{Enabled: true, MaxRetries: 1, MaxFallbackModels: 1}
	}
	if options.RoutingPolicy.Models == nil {
		options.RoutingPolicy.Models = make(map[string]ModelDescriptor)
	}
	if options.RoutingPolicy.Profiles == nil {
		options.RoutingPolicy.Profiles = make(map[string]TaskRule)
	}
	if options.RoutingPolicy.Tasks == nil {
		options.RoutingPolicy.Tasks = make(map[string]TaskRule)
	}
	options.RoutingPolicy.DefaultProfile = strings.ToLower(strings.TrimSpace(options.RoutingPolicy.DefaultProfile))
	if options.RoutingPolicy.DefaultProfile == "" {
		options.RoutingPolicy.DefaultProfile = "general"
	}
	if options.RoutingPolicy.Ensemble.Mode == "" {
		options.RoutingPolicy.Ensemble.Mode = EnsembleIndependent
	}
	if options.RoutingPolicy.Ensemble.CandidateCount == 0 {
		options.RoutingPolicy.Ensemble.CandidateCount = 3
	}
	if options.RoutingPolicy.Ensemble.DebateTurns == 0 && !options.RoutingPolicy.Ensemble.DebateTurnsConfigured {
		options.RoutingPolicy.Ensemble.DebateTurns = 2
	}
	if options.Timeout < 0 {
		return nil, appError(ErrInvalidInput, "timeout must be positive", nil)
	}
	if options.HealthTimeout <= 0 {
		options.HealthTimeout = 3 * time.Second
	}
	if options.HealthClient == nil {
		options.HealthClient = &http.Client{Timeout: options.HealthTimeout}
	}
	if options.HealthProbe == nil {
		client := options.HealthClient
		options.HealthProbe = func(ctx context.Context, providerURL string) bool {
			return defaultHealthProbe(ctx, providerURL, client)
		}
	}
	return &Service{registry: registry, options: options}, nil
}

// FallbackPolicy returns a copy of the resolved fallback policy. A nil
// configured policy resolves to the documented defaults.
func (s *Service) FallbackPolicy() FallbackPolicy {
	fallback := s.options.Fallback
	if fallback == nil {
		return FallbackPolicy{Enabled: true, MaxRetries: 1, MaxFallbackModels: 1}
	}
	out := *fallback
	if fallback.Models != nil {
		models := make(map[string][]string, len(fallback.Models))
		for model, candidates := range fallback.Models {
			models[model] = append([]string(nil), candidates...)
		}
		out.Models = models
	}
	return out
}

// RoutingProfiles returns the configured profile names together with legacy
// built-in task names for CLI completions and clients.
func (s *Service) RoutingProfiles() []string {
	values := map[string]struct{}{}
	for _, name := range taskCategories {
		values[name] = struct{}{}
	}
	for name := range s.options.RoutingPolicy.Tasks {
		values[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	for name := range s.options.RoutingPolicy.Profiles {
		values[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	out := make([]string, 0, len(values))
	for name := range values {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (s *Service) Complete(ctx context.Context, request CompletionRequest) (*ChatCompletionResponse, error) {
	initProgress(&request)
	if err := validateMessages(request.Messages); err != nil {
		request.emitProgress(ProgressEvent{Stage: "accepted", Message: "Invalid completion request", Status: "failed"})
		return nil, err
	}
	strategy := normalizeStrategy(request.Strategy)
	if err := s.validateRequestOptions(request, strategy); err != nil {
		return nil, err
	}
	policy, err := s.resolvePolicy(request)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := s.completionContext(ctx)
	defer cancel()
	callCtx = WithRequestOptions(callCtx, request.Options)
	_, version, userMessages, err := s.prepareConversation(callCtx, &request)
	if err != nil {
		return nil, err
	}
	request.emitProgress(ProgressEvent{Stage: "accepted", Message: "Completion accepted: " + string(strategy), Status: "started"})
	var provider Provider
	var model string
	var routing *RoutingInfo
	var response *ChatCompletionResponse
	var attempts []Attempt
	switch strategy {
	case StrategyClassic:
		if provider, model, err = s.selectProvider(request); err == nil {
			response, attempts, err = s.callWithFallback(callCtx, request, provider, model, strings.ToLower(strings.TrimSpace(request.Provider)), policy)
		}
	case StrategyAuto:
		request.emitProgress(ProgressEvent{Stage: "routing", Message: "Selecting model from routing profile", Status: "started"})
		var candidate ModelCandidate
		var decision RoutingInfo
		candidate, decision, err = s.selectAutomatic(callCtx, request)
		if err == nil {
			request.Task = decision.Task
			request.emitProgress(ProgressEvent{Stage: "routing", Message: "Selected " + candidate.Model + " (" + candidate.Provider + ")", Model: candidate.Model, Provider: candidate.Provider, Status: "succeeded"})
			var fallbacks []string
			fallbacks, err = s.autoFallbacks(request, candidate.Model, policy)
			if err == nil {
				provider = s.registry.Get(candidate.Provider)
				response, attempts, err = s.callWithFallbacks(callCtx, request, provider, candidate.Model, fallbacks, strings.ToLower(strings.TrimSpace(request.Provider)), len(decision.Attempts)+1, policy)
			}
			if err == nil {
				last := attempts[len(attempts)-1]
				response.Model = last.Model
				response.Provider = last.Provider
				decision.Attempts = append(decision.Attempts, attempts...)
				decision.Models = []RoutedModel{{Model: last.Model, Provider: last.Provider, Status: "succeeded"}}
				response.Routing = &decision
			}
		} else {
			request.emitProgress(ProgressEvent{Stage: "routing", Message: "Automatic routing failed", Status: "failed"})
		}
	case StrategyEnsemble:
		response, err = s.completeEnsemble(callCtx, request)
	default:
		return nil, appError(ErrInvalidInput, fmt.Sprintf("unsupported strategy %q", strategy), nil)
	}
	if err != nil {
		return nil, err
	}
	if strategy == StrategyClassic {
		response.Routing = &RoutingInfo{Strategy: StrategyClassic, Attempts: attempts}
	}
	if response == nil || len(response.Choices) == 0 {
		return nil, appError(ErrUpstream, "completion returned an empty response", nil)
	}
	if request.ConversationID != "" {
		request.emitProgress(ProgressEvent{Stage: "conversation", Message: "Saving conversation turn", Status: "started"})
		info := RoutingInfo{Strategy: strategy, Models: []RoutedModel{{Model: model, Provider: response.Provider, Status: "succeeded"}}}
		if response.Routing != nil {
			info = *response.Routing
		} else if routing != nil {
			info = *routing
		}
		updated, err := s.options.Conversations.AppendTurn(callCtx, request.ConversationID, version, userMessages, response.Choices[0].Message, info)
		if err != nil {
			request.emitProgress(ProgressEvent{Stage: "conversation", Message: "Conversation save failed", Status: "failed"})
			return nil, err
		}
		response.ConversationID = updated.ID
		request.emitProgress(ProgressEvent{Stage: "conversation", Message: "Conversation turn saved", Status: "succeeded"})
	}
	return response, nil
}

func (s *Service) CompleteStream(ctx context.Context, request CompletionRequest, callback func(string)) error {
	if callback == nil {
		return appError(ErrInvalidInput, "stream callback is required", nil)
	}
	return s.CompleteStreamWithInfo(ctx, request, func(chunk string, _ *RoutingInfo) { callback(chunk) })
}

// CompleteStreamWithInfo emits the route decision with each chunk so transports
// can include it in their initial streaming event.
func (s *Service) CompleteStreamWithInfo(ctx context.Context, request CompletionRequest, callback func(string, *RoutingInfo)) error {
	initProgress(&request)
	if err := validateMessages(request.Messages); err != nil {
		return err
	}
	if callback == nil {
		return appError(ErrInvalidInput, "stream callback is required", nil)
	}
	strategy := normalizeStrategy(request.Strategy)
	if err := s.validateRequestOptions(request, strategy); err != nil {
		return err
	}
	policy, err := s.resolvePolicy(request)
	if err != nil {
		return err
	}
	callCtx, cancel := s.completionContext(ctx)
	defer cancel()
	callCtx = WithRequestOptions(callCtx, request.Options)
	_, version, userMessages, err := s.prepareConversation(callCtx, &request)
	if err != nil {
		return err
	}
	request.emitProgress(ProgressEvent{Stage: "accepted", Message: "Completion accepted: " + string(strategy), Status: "started"})
	var routing *RoutingInfo
	var streamText strings.Builder
	streamCallback := func(chunk string) { streamText.WriteString(chunk); callback(chunk, routing) }
	if strategy == StrategyEnsemble {
		run, err := s.runEnsemble(callCtx, request)
		if err != nil {
			return err
		}
		routing = run.routing
		request.emitProgress(ProgressEvent{Stage: "synthesis", Message: "Streaming synthesized completion", Model: run.model, Provider: run.synthesizer.GetInfo().Name, Status: "started"})
		synthesisPolicy, policyErr := s.resolvePolicy(request)
		if policyErr != nil {
			return policyErr
		}
		if !synthesisPolicy.Enabled {
			synthesisPolicy.MaxRetries, synthesisPolicy.MaxFallbackModels = 0, 0
		}
		synthesisPolicy.MaxFallbackModels = run.budget
		synthesisRequest := request
		synthesisRequest.Messages = run.synthesis
		synthesisRequest.OnProgress = func(event ProgressEvent) {
			if event.Stage == "model" && event.Status == "started" {
				run.routing.SynthesisModel = event.Model
			}
			request.emitProgress(event)
		}
		initProgress(&synthesisRequest)
		fallbacks := s.ensembleSynthesisFallbacks(request, synthesisPolicy, run.model, run.used)
		attempts, streamErr := s.streamWithFallbacks(callCtx, synthesisRequest, run.synthesizer, run.model, fallbacks, "", len(run.routing.Attempts)+1, synthesisPolicy, func(chunk string) { streamCallback(chunk) })
		run.routing.Attempts = append(run.routing.Attempts, attempts...)
		if streamErr != nil {
			request.emitProgress(ProgressEvent{Stage: "synthesis", Message: "Synthesis failed", Model: run.model, Provider: run.synthesizer.GetInfo().Name, Status: "failed"})
			return s.contextOrUpstreamError(callCtx, streamErr)
		}
		if len(attempts) > 0 {
			run.routing.SynthesisModel = attempts[len(attempts)-1].Model
		}
		request.emitProgress(ProgressEvent{Stage: "synthesis", Message: "Synthesis completed", Model: run.model, Provider: run.synthesizer.GetInfo().Name, Status: "succeeded"})
	} else if strategy == StrategyClassic {
		provider, model, err := s.selectProvider(request)
		if err != nil {
			return err
		}
		routing = &RoutingInfo{Strategy: StrategyClassic, Models: []RoutedModel{{Model: model, Provider: provider.GetInfo().Name, Status: "started"}}}
		attempts, streamErr := s.streamWithFallback(callCtx, request, provider, model, strings.ToLower(strings.TrimSpace(request.Provider)), policy, streamCallback)
		if streamErr != nil {
			return streamErr
		}
		routing.Attempts = attempts
		if len(attempts) > 0 {
			last := attempts[len(attempts)-1]
			if last.Outcome == OutcomeSucceeded {
				routing.Models = []RoutedModel{{Model: last.Model, Provider: last.Provider, Status: "succeeded"}}
			}
		}
	} else {
		request.emitProgress(ProgressEvent{Stage: "routing", Message: "Selecting model from routing profile", Status: "started"})
		candidate, info, selectErr := s.selectAutomatic(callCtx, request)
		if selectErr != nil {
			request.emitProgress(ProgressEvent{Stage: "routing", Message: "Automatic routing failed", Status: "failed"})
			return selectErr
		}
		request.Task = info.Task
		request.emitProgress(ProgressEvent{Stage: "routing", Message: "Selected " + candidate.Model + " (" + candidate.Provider + ")", Model: candidate.Model, Provider: candidate.Provider, Status: "succeeded"})
		fallbacks, fbErr := s.autoFallbacks(request, candidate.Model, policy)
		if fbErr != nil {
			return fbErr
		}
		routing = &RoutingInfo{Strategy: StrategyAuto, Task: info.Task, Attempts: append([]Attempt(nil), info.Attempts...), Models: []RoutedModel{{Model: candidate.Model, Provider: candidate.Provider, Status: "started"}}}
		attempts, streamErr := s.streamWithFallbacks(callCtx, request, s.registry.Get(candidate.Provider), candidate.Model, fallbacks, strings.ToLower(strings.TrimSpace(request.Provider)), len(info.Attempts)+1, policy, streamCallback)
		if streamErr != nil {
			return streamErr
		}
		routing.Attempts = append(routing.Attempts, attempts...)
		if len(attempts) > 0 {
			last := attempts[len(attempts)-1]
			routing.Models = []RoutedModel{{Model: last.Model, Provider: last.Provider, Status: "succeeded"}}
		}
	}
	if request.ConversationID != "" {
		request.emitProgress(ProgressEvent{Stage: "conversation", Message: "Saving conversation turn", Status: "started"})
		assistant := Message{Role: "assistant", Content: streamText.String()}
		info := RoutingInfo{Strategy: strategy}
		if routing != nil {
			info = *routing
		}
		updated, err := s.options.Conversations.AppendTurn(callCtx, request.ConversationID, version, userMessages, assistant, info)
		if err != nil {
			request.emitProgress(ProgressEvent{Stage: "conversation", Message: "Conversation save failed", Status: "failed"})
			return err
		}
		_ = updated
		request.emitProgress(ProgressEvent{Stage: "conversation", Message: "Conversation turn saved", Status: "succeeded"})
	}
	return nil
}

func (s *Service) validateRequestOptions(request CompletionRequest, strategy Strategy) error {
	if request.N > 1 {
		return appError(ErrInvalidInput, "only one completion choice is supported", nil)
	}
	switch strategy {
	case StrategyClassic:
		if strings.TrimSpace(request.Task) != "" || len(request.Models) != 0 || request.Ensemble != nil {
			return appError(ErrInvalidInput, "task, models, and ensemble options require auto or ensemble strategy", nil)
		}
	case StrategyAuto:
		if strings.TrimSpace(request.Model) != "" || len(request.Models) != 0 || request.Ensemble != nil {
			return appError(ErrInvalidInput, "auto selects its model from a profile; model, models, and ensemble options are not accepted", nil)
		}
	case StrategyEnsemble:
		if strings.TrimSpace(request.Model) != "" {
			return appError(ErrInvalidInput, "ensemble uses models for candidates and does not accept model", nil)
		}
	default:
		return appError(ErrInvalidInput, fmt.Sprintf("unsupported strategy %q", strategy), nil)
	}
	if strategy == StrategyEnsemble {
		if _, err := s.resolveEnsembleOptions(request); err != nil {
			return err
		}
		if strings.TrimSpace(request.Task) != "" {
			if _, _, err := s.resolveProfile(request.Task); err != nil {
				return err
			}
		}
	}
	if strategy == StrategyAuto {
		if _, _, err := s.resolveProfile(request.Task); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) prepareConversation(ctx context.Context, request *CompletionRequest) (Conversation, int64, []Message, error) {
	if strings.TrimSpace(request.ConversationID) == "" {
		return Conversation{}, 0, nil, nil
	}
	if s.options.Conversations == nil {
		return Conversation{}, 0, nil, appError(ErrConversationStore, "conversation storage is not configured", nil)
	}
	c, err := s.options.Conversations.Get(ctx, request.ConversationID)
	if err != nil {
		return Conversation{}, 0, nil, err
	}
	if c.Version > 0 {
		for _, m := range request.Messages {
			if m.Role == "system" || m.Role == "developer" {
				return Conversation{}, 0, nil, appError(ErrInvalidInput, "system instructions cannot be replaced after a conversation has started", nil)
			}
		}
	}
	turn := append([]Message(nil), request.Messages...)
	request.Messages = append(c.ContextMessages(), request.Messages...)
	return c, c.Version, turn, nil
}

func (s *Service) CreateConversation(ctx context.Context) (Conversation, error) {
	if s.options.Conversations == nil {
		return Conversation{}, appError(ErrConversationStore, "conversation storage is not configured", nil)
	}
	return s.options.Conversations.Create(ctx)
}
func (s *Service) GetConversation(ctx context.Context, id string) (Conversation, error) {
	if s.options.Conversations == nil {
		return Conversation{}, appError(ErrConversationStore, "conversation storage is not configured", nil)
	}
	return s.options.Conversations.Get(ctx, id)
}
func (s *Service) ListConversations(ctx context.Context, limit, offset int) ([]ConversationSummary, error) {
	if s.options.Conversations == nil {
		return nil, appError(ErrConversationStore, "conversation storage is not configured", nil)
	}
	return s.options.Conversations.List(ctx, limit, offset)
}
func (s *Service) DeleteConversation(ctx context.Context, id string) error {
	if s.options.Conversations == nil {
		return appError(ErrConversationStore, "conversation storage is not configured", nil)
	}
	return s.options.Conversations.Delete(ctx, id)
}

func (s *Service) ListModels() []ModelInfo {
	models := make([]ModelInfo, 0)
	seen := make(map[string]bool)
	for _, name := range s.registry.GetProviderNames() {
		provider := s.registry.Get(name)
		info := provider.GetInfo()
		if !info.Working {
			continue
		}
		for _, id := range info.SupportedModels {
			ownerName, ownerInfo := name, info
			if !provider.SupportsModel(id) {
				if routed := s.registry.GetForModel(id); routed != nil {
					ownerInfo = routed.GetInfo()
					ownerName = strings.ToLower(ownerInfo.Name)
				}
			}
			publicID := PrefixModelID(ownerName, id)
			if seen[publicID] {
				continue
			}
			seen[publicID] = true
			models = append(models, ModelInfo{
				ID: publicID, Object: "model", Created: time.Now().Unix(), OwnedBy: ownerInfo.Label,
				Group:      ownerInfo.Label,
				Permission: []interface{}{}, Root: id,
				Meta: map[string]interface{}{"provider": ownerName, "url": ownerInfo.URL},
			})
		}
	}
	// Providers and their model declarations arrive in deterministic order, but
	// sorting explicitly preserves stable ordering if their order changes later.
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models
}

func (s *Service) ListProviders(ctx context.Context) ([]ProviderInfo, error) {
	names := s.workingProviderNames()
	providers := make([]ProviderInfo, len(names))
	var waitGroup sync.WaitGroup
	for i, name := range names {
		providers[i] = s.registry.Get(name).GetInfo()
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			probeCtx, cancel := context.WithTimeout(ctx, s.options.HealthTimeout)
			defer cancel()
			providers[index].Alive = s.options.HealthProbe(probeCtx, providers[index].URL)
		}(i)
	}
	waitGroup.Wait()
	if err := ctx.Err(); err != nil {
		return nil, s.contextOrUpstreamError(ctx, err)
	}
	return providers, nil
}

// ListProviderMetadata returns the catalogue without probing external URLs.
func (s *Service) ListProviderMetadata() []ProviderInfo {
	names := s.registry.GetProviderNames()
	providers := make([]ProviderInfo, 0, len(names))
	for _, name := range names {
		providers = append(providers, s.registry.Get(name).GetInfo())
	}
	return providers
}

// workingProviderNames hides providers that cannot serve requests, so a token
// provider without a credential is absent from every discovery surface.
func (s *Service) workingProviderNames() []string {
	all := s.registry.GetProviderNames()
	names := make([]string, 0, len(all))
	for _, name := range all {
		if s.registry.Get(name).GetInfo().Working {
			names = append(names, name)
		}
	}
	return names
}

func supportsModel(provider Provider, model string) bool {
	if provider.SupportsModel(model) {
		return true
	}
	normalized := NormalizeModelID(model)
	return normalized != model && provider.SupportsModel(normalized)
}

func callModel(provider Provider, model string) string {
	if provider.SupportsModel(model) {
		return model
	}
	normalized := NormalizeModelID(model)
	if provider.SupportsModel(normalized) {
		return normalized
	}
	return model
}

func (s *Service) selectProvider(request CompletionRequest) (Provider, string, error) {
	requestedModel := strings.TrimSpace(request.Model)
	requestedProvider := strings.ToLower(strings.TrimSpace(request.Provider))
	genericModel := requestedModel == "" || strings.EqualFold(requestedModel, "openai")

	var provider Provider
	if requestedProvider != "" {
		provider = s.registry.Get(requestedProvider)
		if provider == nil {
			return nil, "", appError(ErrUnknownProvider, fmt.Sprintf("provider %q is not registered", requestedProvider), nil)
		}
	} else if genericModel {
		if s.options.DefaultProvider != "" {
			provider = s.registry.Get(s.options.DefaultProvider)
		} else {
			provider = s.registry.Get("perplexity")
		}
	} else {
		provider = s.registry.GetForModel(requestedModel)
		if provider == nil {
			return nil, "", appError(ErrUnknownModel, fmt.Sprintf("model %q is not supported", requestedModel), nil)
		}
	}
	if provider == nil {
		return nil, "", appError(ErrUnknownProvider, "no default provider is registered", nil)
	}
	info := provider.GetInfo()
	if !info.Working {
		return nil, "", appError(ErrProviderDisabled, fmt.Sprintf("provider %q is disabled", info.Name), nil)
	}

	model := requestedModel
	if genericModel {
		model = info.DefaultModel
		if model == "" {
			return nil, "", appError(ErrInvalidInput, fmt.Sprintf("provider %q has no default model", info.Name), nil)
		}
	} else if requestedProvider != "" {
		routed := s.registry.GetForModel(model)
		if routed == nil {
			return nil, "", appError(ErrUnknownModel, fmt.Sprintf("model %q is not supported", model), nil)
		}
		if !supportsModel(provider, model) {
			return nil, "", appError(ErrProviderModelMismatch, fmt.Sprintf("provider %q does not support model %q", info.Name, model), nil)
		}
	}
	return provider, callModel(provider, model), nil
}

func validateMessages(messages []Message) error {
	if len(messages) == 0 {
		return appError(ErrInvalidInput, "messages must not be empty", nil)
	}
	for i, message := range messages {
		switch message.Role {
		case "system", "developer", "user":
			if strings.TrimSpace(message.Content) == "" {
				return appError(ErrInvalidInput, fmt.Sprintf("message %d must have content", i), nil)
			}
		case "tool":
			if strings.TrimSpace(message.Content) == "" {
				return appError(ErrInvalidInput, fmt.Sprintf("message %d must have content", i), nil)
			}
			if strings.TrimSpace(message.ToolCallID) == "" {
				return appError(ErrInvalidInput, fmt.Sprintf("message %d must have tool_call_id", i), nil)
			}
		case "assistant":
			if strings.TrimSpace(message.Content) == "" && message.ToolCalls == nil {
				return appError(ErrInvalidInput, fmt.Sprintf("message %d must have content or tool calls", i), nil)
			}
		default:
			return appError(ErrInvalidInput, fmt.Sprintf("message %d has unsupported role %q", i, message.Role), nil)
		}
	}
	return nil
}

func (s *Service) completionContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if s.options.Timeout > 0 {
		return context.WithTimeout(parent, s.options.Timeout)
	}
	return context.WithCancel(parent)
}

func (s *Service) contextOrUpstreamError(ctx context.Context, cause error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(cause, context.DeadlineExceeded) {
		return appError(ErrTimeout, "provider request exceeded its deadline", cause)
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(cause, context.Canceled) {
		return appError(ErrCanceled, "provider request was canceled", cause)
	}
	return appError(ErrUpstream, "provider request failed", cause)
}
