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
	Model          string    `json:"model"`
	Provider       string    `json:"provider,omitempty"`
	Strategy       Strategy  `json:"strategy,omitempty"`
	Task           string    `json:"task,omitempty"`
	Models         []string  `json:"models,omitempty"`
	ConversationID string    `json:"conversation_id,omitempty"`
	Messages       []Message `json:"messages"`
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
	if options.DefaultProvider != "" && registry.Get(options.DefaultProvider) == nil {
		return nil, appError(ErrUnknownProvider, fmt.Sprintf("configured default provider %q is unknown", options.DefaultProvider), nil)
	}
	if options.RouterModel != "" && registry.GetForModel(options.RouterModel) == nil {
		return nil, appError(ErrUnknownModel, fmt.Sprintf("configured router model %q is unknown", options.RouterModel), nil)
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

func (s *Service) Complete(ctx context.Context, request CompletionRequest) (*ChatCompletionResponse, error) {
	if err := validateMessages(request.Messages); err != nil {
		return nil, err
	}
	callCtx, cancel := s.completionContext(ctx)
	defer cancel()
	_, version, userMessages, err := s.prepareConversation(callCtx, &request)
	if err != nil {
		return nil, err
	}
	strategy := normalizeStrategy(request.Strategy)
	var provider Provider
	var model string
	var routing *RoutingInfo
	var response *ChatCompletionResponse
	switch strategy {
	case StrategyClassic:
		provider, model, err = s.selectProvider(request)
	case StrategyAuto:
		var candidate ModelCandidate
		var decision RoutingInfo
		candidate, decision, err = s.selectAutomatic(callCtx, request)
		if err == nil {
			provider = s.registry.Get(candidate.Provider)
			model = candidate.Model
			routing = &decision
		}
	case StrategyEnsemble:
		response, err = s.completeEnsemble(callCtx, request)
	default:
		return nil, appError(ErrInvalidInput, fmt.Sprintf("unsupported strategy %q", strategy), nil)
	}
	if err != nil {
		return nil, err
	}
	if strategy != StrategyEnsemble {
		response, err = provider.ChatCompletion(callCtx, request.Messages, model)
		if err != nil {
			return nil, s.contextOrUpstreamError(callCtx, err)
		}
		if response == nil || len(response.Choices) == 0 {
			return nil, appError(ErrUpstream, fmt.Sprintf("provider %q returned an empty completion", provider.GetInfo().Name), nil)
		}
		response.Provider = provider.GetInfo().Name
		response.Model = model
		response.Routing = routing
	}
	if response == nil || len(response.Choices) == 0 {
		return nil, appError(ErrUpstream, "completion returned an empty response", nil)
	}
	if request.ConversationID != "" {
		info := RoutingInfo{Strategy: strategy, Models: []RoutedModel{{Model: model, Provider: response.Provider, Status: "succeeded"}}}
		if response.Routing != nil {
			info = *response.Routing
		} else if routing != nil {
			info = *routing
		}
		updated, err := s.options.Conversations.AppendTurn(callCtx, request.ConversationID, version, userMessages, response.Choices[0].Message, info)
		if err != nil {
			return nil, err
		}
		response.ConversationID = updated.ID
	}
	return response, nil
}

func (s *Service) CompleteStream(ctx context.Context, request CompletionRequest, callback func(string)) error {
	if err := validateMessages(request.Messages); err != nil {
		return err
	}
	if callback == nil {
		return appError(ErrInvalidInput, "stream callback is required", nil)
	}
	callCtx, cancel := s.completionContext(ctx)
	defer cancel()
	_, version, userMessages, err := s.prepareConversation(callCtx, &request)
	if err != nil {
		return err
	}
	strategy := normalizeStrategy(request.Strategy)
	var routing *RoutingInfo
	var streamText strings.Builder
	streamCallback := func(chunk string) { streamText.WriteString(chunk); callback(chunk) }
	if strategy == StrategyEnsemble {
		response, err := s.completeEnsemble(callCtx, request)
		if err != nil {
			return err
		}
		// The synthesis response is already available. Stream its final answer as one chunk.
		streamCallback(response.Choices[0].Message.Content)
		routing = response.Routing
	} else {
		var provider Provider
		var model string
		if strategy == StrategyAuto {
			candidate, info, selectErr := s.selectAutomatic(callCtx, request)
			if selectErr != nil {
				return selectErr
			}
			provider, model, routing = s.registry.Get(candidate.Provider), candidate.Model, &info
		} else if strategy == StrategyClassic {
			provider, model, err = s.selectProvider(request)
		} else {
			return appError(ErrInvalidInput, fmt.Sprintf("unsupported strategy %q", strategy), nil)
		}
		if err != nil {
			return err
		}
		if err = provider.ChatCompletionStream(callCtx, request.Messages, model, streamCallback); err != nil {
			return s.contextOrUpstreamError(callCtx, err)
		}
	}
	if request.ConversationID != "" {
		assistant := Message{Role: "assistant", Content: streamText.String()}
		info := RoutingInfo{Strategy: strategy}
		if routing != nil {
			info = *routing
		}
		updated, err := s.options.Conversations.AppendTurn(callCtx, request.ConversationID, version, userMessages, assistant, info)
		if err != nil {
			return err
		}
		_ = updated
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
func (s *Service) ListConversations(ctx context.Context) ([]ConversationSummary, error) {
	if s.options.Conversations == nil {
		return nil, appError(ErrConversationStore, "conversation storage is not configured", nil)
	}
	return s.options.Conversations.List(ctx)
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
			if seen[id] {
				continue
			}
			seen[id] = true
			ownerName, ownerInfo := name, info
			if routed := s.registry.GetForModel(id); routed != nil {
				ownerInfo = routed.GetInfo()
				ownerName = strings.ToLower(ownerInfo.Name)
			}
			models = append(models, ModelInfo{
				ID: id, Object: "model", Created: time.Now().Unix(), OwnedBy: ownerInfo.Label,
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
	names := s.registry.GetProviderNames()
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
		if !provider.SupportsModel(model) {
			return nil, "", appError(ErrProviderModelMismatch, fmt.Sprintf("provider %q does not support model %q", info.Name, model), nil)
		}
	}
	return provider, model, nil
}

func validateMessages(messages []Message) error {
	if len(messages) == 0 {
		return appError(ErrInvalidInput, "messages must not be empty", nil)
	}
	for i, message := range messages {
		switch message.Role {
		case "system", "developer", "user", "tool":
			if strings.TrimSpace(message.Content) == "" {
				return appError(ErrInvalidInput, fmt.Sprintf("message %d must have content", i), nil)
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
