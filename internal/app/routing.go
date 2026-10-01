package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

var supportedTasks = map[string]struct{}{
	"coding": {}, "reasoning": {}, "writing": {}, "translation": {},
	"summarization": {}, "general": {},
}

type ModelCandidate struct {
	Model        string   `json:"model"`
	Provider     string   `json:"provider"`
	Description  string   `json:"description"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type RouteDecision struct {
	Task   string   `json:"task"`
	Model  string   `json:"model,omitempty"`
	Models []string `json:"models,omitempty"`
	Reason string   `json:"reason"`
}

type routingPrompt struct {
	TaskHint   string           `json:"task_hint,omitempty"`
	Candidates []ModelCandidate `json:"candidates"`
	Messages   []Message        `json:"messages"`
}

func (s *Service) selectAutomatic(ctx context.Context, request CompletionRequest) (ModelCandidate, RoutingInfo, error) {
	candidates, decision, err := s.selectCandidates(ctx, request, 1)
	if err != nil {
		return ModelCandidate{}, RoutingInfo{}, err
	}
	selected := candidates[0]
	return selected, RoutingInfo{
		Strategy: StrategyAuto,
		Task:     decision.Task,
		Models:   []RoutedModel{{Model: selected.Model, Provider: selected.Provider, Status: "succeeded"}},
	}, nil
}

func (s *Service) selectCandidates(ctx context.Context, request CompletionRequest, limit int) ([]ModelCandidate, RouteDecision, error) {
	return s.selectCandidatesExcluding(ctx, request, limit, "")
}

func (s *Service) selectCandidatesExcluding(ctx context.Context, request CompletionRequest, limit int, excludedModel string) ([]ModelCandidate, RouteDecision, error) {
	if limit < 1 || limit > 3 {
		return nil, RouteDecision{}, appError(ErrInvalidInput, "automatic routing candidate limit must be between 1 and 3", nil)
	}
	taskHint := strings.ToLower(strings.TrimSpace(request.Task))
	var taskRule TaskRule
	if taskHint != "" {
		if _, ok := supportedTasks[taskHint]; !ok {
			return nil, RouteDecision{}, appError(ErrInvalidInput, fmt.Sprintf("task %q is not supported", taskHint), nil)
		}
		taskRule = s.options.RoutingPolicy.Tasks[taskHint]
	}
	candidates, err := s.modelCandidates(request.Provider, taskHint, taskRule, excludedModel)
	if err != nil {
		return nil, RouteDecision{}, err
	}
	if len(candidates) == 0 {
		return nil, RouteDecision{}, appError(ErrRouting, "no enabled models match the routing constraints", nil)
	}
	if taskHint != "" && len(candidates) == 1 {
		candidate := candidates[0]
		return []ModelCandidate{candidate}, RouteDecision{Task: taskHint, Model: candidate.Model, Models: []string{candidate.Model}, Reason: "unique model matched task rules"}, nil
	}
	if s.options.RouterModel == "" {
		return nil, RouteDecision{}, appError(ErrRouting, "ROUTER_MODEL is required to choose among multiple candidates", nil)
	}
	router := s.registry.GetForModel(s.options.RouterModel)
	if router == nil || !router.GetInfo().Working {
		return nil, RouteDecision{}, appError(ErrRouting, fmt.Sprintf("router model %q is unavailable", s.options.RouterModel), nil)
	}
	input, err := json.Marshal(routingPrompt{TaskHint: taskHint, Candidates: candidates, Messages: request.Messages})
	if err != nil {
		return nil, RouteDecision{}, appError(ErrRouting, "could not encode routing request", err)
	}
	routerMessages := []Message{
		{Role: "system", Content: routingSystemPrompt(limit)},
		{Role: "user", Content: string(input)},
	}
	response, err := router.ChatCompletion(ctx, routerMessages, s.options.RouterModel)
	if err != nil {
		return nil, RouteDecision{}, appError(ErrRouting, "router model request failed", err)
	}
	if response == nil || len(response.Choices) == 0 {
		return nil, RouteDecision{}, appError(ErrRouting, "router model returned no decision", nil)
	}
	decision, err := parseRouteDecision(response.Choices[0].Message.Content)
	if err != nil {
		return nil, RouteDecision{}, appError(ErrRouting, "router model returned an invalid decision", err)
	}
	decision.Task = strings.ToLower(strings.TrimSpace(decision.Task))
	if taskHint != "" && decision.Task != taskHint {
		return nil, RouteDecision{}, appError(ErrRouting, "router task does not match the requested task", nil)
	}
	if _, ok := supportedTasks[decision.Task]; !ok {
		return nil, RouteDecision{}, appError(ErrRouting, fmt.Sprintf("router returned unsupported task %q", decision.Task), nil)
	}
	allCandidates := append([]ModelCandidate(nil), candidates...)
	if rule, ok := s.options.RoutingPolicy.Tasks[decision.Task]; ok {
		candidates = filterRequiredCapabilities(candidates, rule.RequiredCapabilities)
	}
	if len(candidates) == 0 {
		return nil, RouteDecision{}, appError(ErrRouting, "no models satisfy the router's task constraints", nil)
	}
	selectedIDs := decision.Models
	if limit == 1 {
		if decision.Model == "" || len(decision.Models) != 0 {
			return nil, RouteDecision{}, appError(ErrRouting, "router decision must contain exactly one model", nil)
		}
		selectedIDs = []string{decision.Model}
	} else if len(selectedIDs) < 2 || len(selectedIDs) > limit || decision.Model != "" {
		return nil, RouteDecision{}, appError(ErrRouting, fmt.Sprintf("router decision must contain between 2 and %d models", limit), nil)
	}
	if taskHint == "" {
		for _, id := range selectedIDs {
			found := false
			for _, candidate := range candidates {
				if strings.EqualFold(candidate.Model, id) {
					found = true
					break
				}
			}
			if !found {
				wasEligibleBeforeTaskFilter := false
				for _, candidate := range allCandidates {
					if strings.EqualFold(candidate.Model, id) {
						wasEligibleBeforeTaskFilter = true
						break
					}
				}
				if !wasEligibleBeforeTaskFilter {
					return nil, RouteDecision{}, appError(ErrRouting, fmt.Sprintf("router selected ineligible model %q", id), nil)
				}
				request.Task = decision.Task
				return s.selectCandidatesExcluding(ctx, request, limit, excludedModel)
			}
		}
	}
	selected := make([]ModelCandidate, 0, len(selectedIDs))
	seen := make(map[string]struct{}, len(selectedIDs))
	for _, id := range selectedIDs {
		key := strings.ToLower(strings.TrimSpace(id))
		if _, duplicate := seen[key]; duplicate {
			return nil, RouteDecision{}, appError(ErrRouting, fmt.Sprintf("router selected model %q more than once", id), nil)
		}
		seen[key] = struct{}{}
		var match *ModelCandidate
		for i := range candidates {
			if strings.EqualFold(candidates[i].Model, id) {
				match = &candidates[i]
				break
			}
		}
		if match == nil {
			return nil, RouteDecision{}, appError(ErrRouting, fmt.Sprintf("router selected ineligible model %q", id), nil)
		}
		selected = append(selected, *match)
	}
	decision.Models = make([]string, len(selected))
	for i, candidate := range selected {
		decision.Models[i] = candidate.Model
	}
	if limit == 1 {
		decision.Model = selected[0].Model
	}
	return selected, decision, nil
}

func (s *Service) modelCandidates(providerFilter, task string, rule TaskRule, excludedModel string) ([]ModelCandidate, error) {
	providerFilter = strings.ToLower(strings.TrimSpace(providerFilter))
	if providerFilter != "" && s.registry.Get(providerFilter) == nil {
		return nil, appError(ErrUnknownProvider, fmt.Sprintf("provider %q is not registered", providerFilter), nil)
	}
	preferred := make(map[string]int, len(rule.PreferredModels))
	for i, model := range rule.PreferredModels {
		preferred[strings.ToLower(strings.TrimSpace(model))] = i
	}
	seen := make(map[string]struct{})
	candidates := make([]ModelCandidate, 0)
	for _, providerName := range s.registry.GetProviderNames() {
		provider := s.registry.Get(providerName)
		info := provider.GetInfo()
		if !info.Working || providerFilter != "" && !strings.EqualFold(providerFilter, info.Name) {
			continue
		}
		for _, model := range info.SupportedModels {
			key := strings.ToLower(strings.TrimSpace(model))
			if key == "" || key == strings.ToLower(strings.TrimSpace(s.options.RouterModel)) || key == strings.ToLower(strings.TrimSpace(excludedModel)) {
				continue
			}
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			selectedProvider := provider
			if providerFilter == "" {
				if routed := s.registry.GetForModel(model); routed != nil {
					selectedProvider = routed
				}
			}
			if !selectedProvider.GetInfo().Working {
				continue
			}
			descriptor := s.modelDescriptor(model)
			if !hasCapabilities(descriptor.Capabilities, rule.RequiredCapabilities) {
				continue
			}
			description := strings.TrimSpace(descriptor.Description)
			if description == "" {
				description = strings.TrimSpace(selectedProvider.GetInfo().Description)
			}
			candidates = append(candidates, ModelCandidate{
				Model: model, Provider: selectedProvider.GetInfo().Name,
				Description: description, Capabilities: append([]string(nil), descriptor.Capabilities...),
			})
			seen[key] = struct{}{}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, leftOK := preferred[strings.ToLower(candidates[i].Model)]
		right, rightOK := preferred[strings.ToLower(candidates[j].Model)]
		if leftOK != rightOK {
			return leftOK
		}
		if leftOK && left != right {
			return left < right
		}
		return strings.ToLower(candidates[i].Model) < strings.ToLower(candidates[j].Model)
	})
	return candidates, nil
}

func (s *Service) modelDescriptor(model string) ModelDescriptor {
	for id, descriptor := range s.options.RoutingPolicy.Models {
		if strings.EqualFold(id, model) {
			return descriptor
		}
	}
	return ModelDescriptor{}
}

func hasCapabilities(available, required []string) bool {
	set := make(map[string]struct{}, len(available))
	for _, capability := range available {
		set[strings.ToLower(strings.TrimSpace(capability))] = struct{}{}
	}
	for _, capability := range required {
		if _, ok := set[strings.ToLower(strings.TrimSpace(capability))]; !ok {
			return false
		}
	}
	return true
}

func filterRequiredCapabilities(candidates []ModelCandidate, required []string) []ModelCandidate {
	filtered := make([]ModelCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if hasCapabilities(candidate.Capabilities, required) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func parseRouteDecision(content string) (RouteDecision, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(strings.TrimSpace(content)))
	decoder.DisallowUnknownFields()
	var decision RouteDecision
	if err := decoder.Decode(&decision); err != nil {
		return RouteDecision{}, err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return RouteDecision{}, fmt.Errorf("router response contains trailing data")
	}
	return decision, nil
}

func routingSystemPrompt(limit int) string {
	if limit == 1 {
		return `Classify the task and choose one model from the supplied candidates. Return only JSON with keys "task", "model", and "reason". The task must be one of coding, reasoning, writing, translation, summarization, general. Never invent a model ID.`
	}
	return fmt.Sprintf(`Classify the task and choose between 2 and %d distinct models from the supplied candidates. Return only JSON with keys "task", "models" (array), and "reason". The task must be one of coding, reasoning, writing, translation, summarization, general. Never invent a model ID.`, limit)
}
