package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	jimmyBaseURL     = "https://chatjimmy.ai"
	jimmyChatURL     = jimmyBaseURL + "/api/chat"
	jimmyModel       = "llama3.1-8B"
	jimmyUserAgent   = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36"
	jimmyHTTPTimeout = 90 * time.Second
)

var jimmyStatsMarkers = regexp.MustCompile(`(?s)<!--stats-->.*?<!--/stats-->|<\|stats\|>.*?<\|/stats\|>`)

type Jimmy struct {
	endpoint string
	client   *http.Client
}

type jimmyChatOptions struct {
	SelectedModel string `json:"selectedModel"`
	SystemPrompt  string `json:"systemPrompt"`
	TopK          int    `json:"topK"`
}

type jimmyChatRequest struct {
	Messages    []Message        `json:"messages"`
	ChatOptions jimmyChatOptions `json:"chatOptions"`
	Attachment  interface{}      `json:"attachment"`
}

func (p *Jimmy) GetInfo() ProviderInfo {
	return ProviderInfo{
		Name:               "jimmy",
		Label:              "ChatJimmy",
		URL:                jimmyBaseURL,
		Working:            true,
		Alive:              true,
		SupportedModels:    []string{jimmyModel},
		DefaultModel:       jimmyModel,
		SupportsStream:     false,
		NeedsAuth:          false,
		Description:        "ChatJimmy's public chat API using the Llama 3.1 8B model; account-free, non-streaming upstream.",
		ReverseEngineering: true,
	}
}

func (p *Jimmy) SupportsModel(model string) bool {
	return strings.EqualFold(strings.TrimSpace(model), jimmyModel)
}

func (p *Jimmy) ChatCompletion(ctx context.Context, messages []Message, model string) (*ChatCompletionResponse, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		model = jimmyModel
	}
	if !p.SupportsModel(model) {
		return nil, fmt.Errorf("jimmy: unsupported model %q", model)
	}
	model = jimmyModel
	if len(messages) == 0 {
		return nil, fmt.Errorf("jimmy: no messages provided")
	}

	systemPrompt := make([]string, 0, 1)
	for _, message := range messages {
		if message.Role == "system" {
			systemPrompt = append(systemPrompt, message.Content)
		}
	}
	payload := jimmyChatRequest{
		Messages: messages,
		ChatOptions: jimmyChatOptions{
			SelectedModel: model,
			SystemPrompt:  strings.Join(systemPrompt, "\n\n"),
			TopK:          8,
		},
		Attachment: nil,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("jimmy: encode request: %w", err)
	}

	endpoint := p.endpoint
	if endpoint == "" {
		endpoint = jimmyChatURL
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("jimmy: invalid endpoint: %w", err)
	}
	baseURL := parsed.Scheme + "://" + parsed.Host
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("jimmy: create request: %w", err)
	}
	req.Header.Set("Accept", "text/plain, */*")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", baseURL)
	req.Header.Set("Referer", baseURL+"/")
	req.Header.Set("User-Agent", jimmyUserAgent)

	client := p.client
	if client == nil {
		client = &http.Client{Timeout: jimmyHTTPTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jimmy: request failed: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("jimmy: read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("jimmy: upstream returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	content := strings.TrimSpace(jimmyStatsMarkers.ReplaceAllString(string(responseBody), ""))
	if content == "" {
		return nil, fmt.Errorf("jimmy: empty response")
	}

	response := CreateResponse(fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()), model, content, EstimatePromptTokens(messages))
	response.Provider = "jimmy"
	return response, nil
}

func (p *Jimmy) ChatCompletionStream(ctx context.Context, messages []Message, model string, callback func(chunk string)) error {
	response, err := p.ChatCompletion(ctx, messages, model)
	if err != nil {
		return err
	}
	if callback != nil {
		callback(response.Choices[0].Message.Content)
	}
	return nil
}
