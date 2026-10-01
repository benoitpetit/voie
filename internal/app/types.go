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
	ID             string       `json:"id"`
	Object         string       `json:"object"`
	Created        int64        `json:"created"`
	Model          string       `json:"model"`
	ConversationID string       `json:"conversation_id,omitempty"`
	Routing        *RoutingInfo `json:"routing,omitempty"`
	Choices        []Choice     `json:"choices"`
}

type RoutingInfo struct {
	Strategy Strategy      `json:"strategy"`
	Task     string        `json:"task,omitempty"`
	Models   []RoutedModel `json:"models,omitempty"`
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
	Models map[string]ModelDescriptor
	Tasks  map[string]TaskRule
}

type ModelDescriptor struct {
	Description  string
	Capabilities []string
}

type TaskRule struct {
	RequiredCapabilities []string
	PreferredModels      []string
}

type Provider interface {
	ChatCompletion(ctx context.Context, messages []Message, model string) (*ChatCompletionResponse, error)
	ChatCompletionStream(ctx context.Context, messages []Message, model string, callback func(chunk string)) error
	GetInfo() ProviderInfo
	SupportsModel(model string) bool
}
