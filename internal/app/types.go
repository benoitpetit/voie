package app

import "context"

type Message struct {
	Role       string      `json:"role"`
	Content    string      `json:"content"`
	Name       string      `json:"name,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolCalls  interface{} `json:"tool_calls,omitempty"`
}

type ChatCompletionResponse struct {
	ID             string       `json:"id"`
	Object         string       `json:"object"`
	Created        int64        `json:"created"`
	Model          string       `json:"model"`
	Provider       string       `json:"provider,omitempty"`
	ConversationID string       `json:"conversation_id,omitempty"`
	Routing        *RoutingInfo `json:"routing,omitempty"`
	Choices        []Choice     `json:"choices"`
	Usage          *Usage       `json:"usage,omitempty"`
}

type Choice struct {
	Message      Message `json:"message"`
	Delta        *Delta  `json:"delta,omitempty"`
	FinishReason string  `json:"finish_reason,omitempty"`
	Index        int     `json:"index"`
}

type Delta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type StreamChunk struct {
	ID             string         `json:"id"`
	Object         string         `json:"object"`
	Created        int64          `json:"created"`
	Model          string         `json:"model"`
	ConversationID string         `json:"conversation_id,omitempty"`
	Routing        *RoutingInfo   `json:"routing,omitempty"`
	Choices        []StreamChoice `json:"choices"`
}

// StreamChoice is the OpenAI streaming choice shape. It deliberately has no
// message field: a chat.completion.chunk carries only delta, finish_reason and
// index, while a non-streaming chat.completion carries message.
type StreamChoice struct {
	Delta        *Delta  `json:"delta,omitempty"`
	FinishReason *string `json:"finish_reason"`
	Index        int     `json:"index"`
}

type RoutingInfo struct {
	Strategy       Strategy      `json:"strategy"`
	Task           string        `json:"task,omitempty"`
	EnsembleMode   EnsembleMode  `json:"ensemble_mode,omitempty"`
	Agents         int           `json:"agents,omitempty"`
	Turns          int           `json:"turns,omitempty"`
	SynthesisModel string        `json:"synthesis_model,omitempty"`
	Models         []RoutedModel `json:"models,omitempty"`
	Attempts       []Attempt     `json:"attempts,omitempty"`
}

// FallbackOverride is a per-request fallback policy merge. Omitted pointer
// fields inherit the global policy; an explicitly empty Models list clears
// every fallback candidate.
type FallbackOverride struct {
	Enabled           *bool    `json:"enabled,omitempty"`
	MaxRetries        *int     `json:"max_retries,omitempty"`
	MaxFallbackModels *int     `json:"max_fallback_models,omitempty"`
	Models            []string `json:"models,omitempty"`
}

// Attempt records one provider call for routing.attempts metadata. It carries
// model, provider, outcome and elapsed time only; never prompts or answers.
type Attempt struct {
	Model          string `json:"model"`
	Provider       string `json:"provider"`
	Attempt        int    `json:"attempt"`
	Outcome        string `json:"outcome"`
	DurationMillis int64  `json:"duration_ms,omitempty"`
}

type RoutedModel struct {
	Model          string `json:"model"`
	Provider       string `json:"provider"`
	Status         string `json:"status"`
	DurationMillis int64  `json:"duration_ms,omitempty"`
}

type ProviderInfo struct {
	Name               string   `json:"name"`
	Label              string   `json:"label"`
	URL                string   `json:"url"`
	Working            bool     `json:"working"`
	Alive              bool     `json:"alive"`
	SupportedModels    []string `json:"supported_models"`
	DefaultModel       string   `json:"default_model"`
	SupportsStream     bool     `json:"supports_stream"`
	NeedsAuth          bool     `json:"needs_auth"`
	Description        string   `json:"description"`
	ReverseEngineering bool     `json:"reverse_engineering"`
}

type ModelInfo struct {
	ID         string                 `json:"id"`
	Object     string                 `json:"object"`
	Created    int64                  `json:"created"`
	OwnedBy    string                 `json:"owned_by"`
	Permission []interface{}          `json:"permission"`
	Root       string                 `json:"root"`
	Parent     *string                `json:"parent"`
	Meta       map[string]interface{} `json:"meta,omitempty"`
}

type RoutingPolicy struct {
	Models         map[string]ModelDescriptor
	Profiles       map[string]TaskRule
	Tasks          map[string]TaskRule // legacy alias for Profiles
	DefaultProfile string
	Ensemble       EnsemblePolicy
}

type ModelDescriptor struct {
	Description  string
	Capabilities []string
}

type TaskRule struct {
	RequiredCapabilities []string
	PreferredModels      []string
	Selection            SelectionMode
}

type SelectionMode string

const (
	SelectionLocal  SelectionMode = "local"
	SelectionRouter SelectionMode = "router"
)

type EnsembleMode string

const (
	EnsembleIndependent EnsembleMode = "independent"
	EnsembleChain       EnsembleMode = "chain"
	EnsembleDebate      EnsembleMode = "debate"
)

// EnsembleRequestOptions overrides the configured ensemble defaults for one
// request. Models supplies an exact candidate list; CandidateCount controls
// automatic candidate selection when Models is omitted. ChainAgents is kept
// for compatibility with existing chain requests.
type EnsembleRequestOptions struct {
	Mode           EnsembleMode `json:"mode,omitempty" jsonschema:"optional ensemble algorithm: independent, chain, or debate"`
	CandidateCount *int         `json:"candidate_count,omitempty" jsonschema:"optional number of agents to select automatically, from 2 to 5; when models is provided it must match the list length"`
	Models         []string     `json:"models,omitempty" jsonschema:"optional exact list of 2 to 5 distinct agent model IDs; omit to select from the routing profile"`
	ChainAgents    *int         `json:"chain_agents,omitempty" jsonschema:"optional legacy chain length for chain mode, from 2 to 5"`
	DebateTurns    *int         `json:"debate_turns,omitempty" jsonschema:"optional debate review rounds for debate mode, from 0 to 5"`
}

type EnsemblePolicy struct {
	Mode                  EnsembleMode
	CandidateCount        int
	ChainAgents           int
	DebateTurns           int
	DebateTurnsConfigured bool
}

// FallbackPolicy is the validated runtime fallback policy. Models maps a
// routed model ID to an ordered list of fallback model IDs.
type FallbackPolicy struct {
	Enabled           bool
	MaxRetries        int
	MaxFallbackModels int
	Models            map[string][]string
}

type Provider interface {
	ChatCompletion(ctx context.Context, messages []Message, model string) (*ChatCompletionResponse, error)
	ChatCompletionStream(ctx context.Context, messages []Message, model string, callback func(chunk string)) error
	GetInfo() ProviderInfo
	SupportsModel(model string) bool
}
