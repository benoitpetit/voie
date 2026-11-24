package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

var taskCategories = []string{"coding", "reasoning", "writing", "translation", "summarization", "general"}

var supportedTasks = func() map[string]struct{} {
	tasks := make(map[string]struct{}, len(taskCategories))
	for _, task := range taskCategories {
		tasks[task] = struct{}{}
	}
	return tasks
}()

// SupportedTaskCategories returns the task hints accepted by automatic routing.
func SupportedTaskCategories() []string {
	return append([]string(nil), taskCategories...)
}

type ModelCandidate struct {
	Model        string   `json:"model"`
	Provider     string   `json:"provider"`
	Description  string   `json:"description"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type RouteDecision struct {
	Task     string    `json:"task"`
	Model    string    `json:"model,omitempty"`
	Models   []string  `json:"models,omitempty"`
	Reason   string    `json:"reason"`
	Attempts []Attempt `json:"-"`
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
		Attempts: decision.Attempts,
	}, nil
}

// autoFallbacks returns the ordered, distinct fallback model IDs for the
// automatic path: the request-level explicit list, the primary's configured
// list, then the task/capability-compatible pool in preferred order. Every
// candidate must survive the same task, provider and capability constraints
// the router applied, and the primary and router are never eligible. An
// explicitly empty request list clears eligibility.
func (s *Service) autoFallbacks(request CompletionRequest, primaryModel string, policy FallbackPolicy) ([]string, error) {
	if request.Fallback != nil && request.Fallback.Models != nil && len(request.Fallback.Models) == 0 {
		return nil, nil
	}
	task, taskRule, err := s.resolveProfile(request.Task)
	if err != nil {
		return nil, err
	}
	eligible, err := s.modelCandidates(request.Provider, task, taskRule, primaryModel)
	if err != nil {
		return nil, err
	}
	pool := make([]string, 0, len(eligible))
	poolSet := make(map[string]bool, len(eligible))
	for _, candidate := range eligible {
		key := strings.ToLower(strings.TrimSpace(candidate.Model))
		if key == "" || poolSet[key] {
			continue
		}
		poolSet[key] = true
		pool = append(pool, key)
	}
	out := make([]string, 0, len(pool))
	seen := make(map[string]bool, len(pool))
	add := func(id string) {
		key := strings.ToLower(strings.TrimSpace(id))
		if !poolSet[key] || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, key)
	}
	if request.Fallback != nil {
		for _, id := range request.Fallback.Models {
			add(id)
		}
	}
	if configured, ok := s.configuredFallbacks(policy, primaryModel); ok {
		for _, id := range configured {
			add(id)
		}
	}
	for _, id := range pool {
		add(id)
	}
	return out, nil
}

func (s *Service) selectCandidates(ctx context.Context, request CompletionRequest, limit int) ([]ModelCandidate, RouteDecision, error) {
	return s.selectCandidatesExcluding(ctx, request, limit, "")
}

func (s *Service) selectCandidatesExcluding(ctx context.Context, request CompletionRequest, limit int, excludedModel string) ([]ModelCandidate, RouteDecision, error) {
	if limit < 1 || limit > 5 {
		return nil, RouteDecision{}, appError(ErrInvalidInput, "automatic routing candidate limit must be between 1 and 5", nil)
	}
	taskHint, taskRule, err := s.resolveProfile(request.Task)
	if err != nil {
		return nil, RouteDecision{}, err
	}
	candidates, err := s.modelCandidates(request.Provider, taskHint, taskRule, excludedModel)
	if err != nil {
		return nil, RouteDecision{}, err
	}
	if len(candidates) == 0 {
		return nil, RouteDecision{}, appError(ErrRouting, "no enabled models match the routing constraints", nil)
	}
	if len(candidates) == 1 {
		candidate := candidates[0]
		return []ModelCandidate{candidate}, RouteDecision{Task: taskHint, Model: candidate.Model, Models: []string{candidate.Model}, Reason: "only eligible model"}, nil
	}
	selectedCount := min(limit, len(candidates))
	selection := SelectionMode(strings.ToLower(strings.TrimSpace(string(taskRule.Selection))))
	if selection == "" {
		selection = SelectionLocal
	}
	if selection == SelectionLocal {
		selected := append([]ModelCandidate(nil), candidates[:selectedCount]...)
		decision := RouteDecision{Task: taskHint, Reason: "selected by configured model priority", Models: make([]string, len(selected))}
		for i, candidate := range selected {
			decision.Models[i] = candidate.Model
		}
		if limit == 1 {
			decision.Model = selected[0].Model
			decision.Models = []string{selected[0].Model}
		}
		return selected, decision, nil
	}
	if selection != SelectionRouter {
		return nil, RouteDecision{}, appError(ErrRouting, fmt.Sprintf("profile %q has unsupported selection mode %q", taskHint, selection), nil)
	}
	if s.options.RouterModel == "" {
		return nil, RouteDecision{}, appError(ErrRouting, fmt.Sprintf("profile %q uses router selection but ROUTER_MODEL is not configured", taskHint), nil)
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
		{Role: "system", Content: routingSystemPrompt(selectedCount)},
		{Role: "user", Content: string(input)},
	}
	routeStarted := time.Now()
	response, err := router.ChatCompletion(ctx, routerMessages, s.options.RouterModel)
	routeAttempt := Attempt{Model: s.options.RouterModel, Provider: router.GetInfo().Name, Attempt: 1, DurationMillis: time.Since(routeStarted).Milliseconds()}
	failed := func(err error) ([]ModelCandidate, RouteDecision, error) {
		routeAttempt.Outcome = OutcomeFailed
		return nil, RouteDecision{Task: taskHint, Attempts: []Attempt{routeAttempt}}, err
	}
	if err != nil {
		routeAttempt.Outcome = outcomeFor(err)
		return failed(appError(ErrRouting, "router model request failed", err))
	}
	if response == nil || len(response.Choices) == 0 {
		routeAttempt.Outcome = OutcomeFailed
		return failed(appError(ErrRouting, "router model returned no decision", nil))
	}
	routeAttempt.Outcome = OutcomeSucceeded
	decision, err := parseRouteDecision(response.Choices[0].Message.Content)
	if err != nil {
		return failed(appError(ErrRouting, "router model returned an invalid decision", err))
	}
	decision.Task = strings.ToLower(strings.TrimSpace(decision.Task))
	if decision.Task == "" {
		decision.Task = taskHint
	}
	if decision.Task != taskHint {
		return failed(appError(ErrRouting, "router task does not match the requested task", nil))
	}
	decision.Attempts = append(decision.Attempts, routeAttempt)
	selectedIDs := decision.Models
	if selectedCount == 1 {
		if decision.Model == "" || len(decision.Models) != 0 {
			return failed(appError(ErrRouting, "router decision must contain exactly one model", nil))
		}
		selectedIDs = []string{decision.Model}
	} else if len(selectedIDs) != selectedCount || decision.Model != "" {
		return failed(appError(ErrRouting, fmt.Sprintf("router decision must contain exactly %d models", selectedCount), nil))
	}
	selected := make([]ModelCandidate, 0, len(selectedIDs))
	seen := make(map[string]struct{}, len(selectedIDs))
	for _, id := range selectedIDs {
		key := strings.ToLower(strings.TrimSpace(id))
		if _, duplicate := seen[key]; duplicate {
			return failed(appError(ErrRouting, fmt.Sprintf("router selected model %q more than once", id), nil))
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
			return failed(appError(ErrRouting, fmt.Sprintf("router selected ineligible model %q", id), nil))
		}
		selected = append(selected, *match)
	}
	decision.Models = make([]string, len(selected))
	for i, candidate := range selected {
		decision.Models[i] = candidate.Model
	}
	if selectedCount == 1 {
		decision.Model = selected[0].Model
	}
	return selected, decision, nil
}

func (s *Service) resolveProfile(requested string) (string, TaskRule, error) {
	name := strings.ToLower(strings.TrimSpace(requested))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(s.options.RoutingPolicy.DefaultProfile))
		if name == "" {
			name = "general"
		}
	}
	if rule, ok := lookupTaskRule(s.options.RoutingPolicy.Profiles, name); ok {
		return name, rule, nil
	}
	if rule, ok := lookupTaskRule(s.options.RoutingPolicy.Tasks, name); ok {
		return name, rule, nil
	}
	if _, legacy := supportedTasks[name]; legacy {
		if name == "general" {
			return name, TaskRule{}, nil
		}
		return name, TaskRule{RequiredCapabilities: []string{name}}, nil
	}
	return "", TaskRule{}, appError(ErrInvalidInput, fmt.Sprintf("routing profile %q is not configured", name), nil)
}

func lookupTaskRule(rules map[string]TaskRule, name string) (TaskRule, bool) {
	for key, rule := range rules {
		if strings.EqualFold(strings.TrimSpace(key), strings.TrimSpace(name)) {
			return rule, true
		}
	}
	return TaskRule{}, false
}

func (s *Service) modelCandidates(providerFilter, task string, rule TaskRule, excludedModel string) ([]ModelCandidate, error) {
	providerFilter = strings.ToLower(strings.TrimSpace(providerFilter))
	if providerFilter != "" && s.registry.Get(providerFilter) == nil {
		return nil, appError(ErrUnknownProvider, fmt.Sprintf("provider %q is not registered", providerFilter), nil)
	}
	preferred := make(map[string]int, len(rule.PreferredModels))
	preferredCanonical := make(map[string]int, len(rule.PreferredModels))
	canonicalOwner := make(map[string]string, len(rule.PreferredModels))
	for i, model := range rule.PreferredModels {
		exact := strings.ToLower(strings.TrimSpace(model))
		preferred[exact] = i
		canonical := canonicalModelID(model)
		if owner, ok := canonicalOwner[canonical]; ok && owner != exact {
			delete(preferredCanonical, canonical) // ambiguous: two public IDs share a raw ID
			continue
		}
		canonicalOwner[canonical] = exact
		preferredCanonical[canonical] = i
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
		if !leftOK {
			left, leftOK = preferredCanonical[canonicalModelID(candidates[i].Model)]
		}
		right, rightOK := preferred[strings.ToLower(candidates[j].Model)]
		if !rightOK {
			right, rightOK = preferredCanonical[canonicalModelID(candidates[j].Model)]
		}
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

// canonicalModelID maps public prefixed IDs and raw upstream IDs to one
// comparison key, so routing.json can use the IDs shown by `voie models`.
func canonicalModelID(id string) string {
	return strings.ToLower(strings.TrimSpace(NormalizeModelID(id)))
}

func (s *Service) modelDescriptor(model string) ModelDescriptor {
	for id, descriptor := range s.options.RoutingPolicy.Models {
		if strings.EqualFold(strings.TrimSpace(id), strings.TrimSpace(model)) {
			return descriptor
		}
	}
	canonical := canonicalModelID(model)
	var fallback ModelDescriptor
	matches := 0
	for id, descriptor := range s.options.RoutingPolicy.Models {
		if canonicalModelID(id) == canonical {
			fallback = descriptor
			matches++
		}
	}
	if matches == 1 {
		return fallback
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
	if limit < 1 {
		limit = 1
	}
	if limit == 1 {
		return `Choose the best model for the supplied routing profile from the supplied candidates. Return only JSON with keys "task", "model", and "reason". Set "task" to the supplied profile name. Never invent a model ID.`
	}
	return fmt.Sprintf(`Choose exactly %d distinct models for the supplied routing profile from the supplied candidates. Return only JSON with keys "task", "models" (array), and "reason". Set "task" to the supplied profile name. Never invent a model ID.`, limit)
}
