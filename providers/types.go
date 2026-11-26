package providers

import (
	"time"

	"github.com/benoitpetit/voie/internal/app"
)

type Message = app.Message
type ChatCompletionResponse = app.ChatCompletionResponse
type Choice = app.Choice
type Usage = app.Usage
type ProviderInfo = app.ProviderInfo
type Provider = app.Provider

func CreateResponse(id, model, content string, promptTokens int) *ChatCompletionResponse {
	completionTokens := len(content) / 4
	if completionTokens < 1 {
		completionTokens = 1
	}
	return &ChatCompletionResponse{
		ID: id, Object: "chat.completion", Created: time.Now().Unix(), Model: model,
		Choices: []Choice{{Message: Message{Role: "assistant", Content: content}, FinishReason: "stop"}},
		Usage:   &Usage{PromptTokens: promptTokens, CompletionTokens: completionTokens, TotalTokens: promptTokens + completionTokens},
	}
}

func EstimatePromptTokens(messages []Message) int {
	total := 0
	for _, msg := range messages {
		total += len(msg.Content)/4 + 4
	}
	if total < 1 {
		total = 1
	}
	return total
}
