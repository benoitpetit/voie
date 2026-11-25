package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
)

// A2AProvider calls one configured remote A2A agent as a normal voie provider.
type A2AProvider struct {
	name   string
	client *http.Client
}

// NewA2AProvider builds a provider for a named agent.
func NewA2AProvider(name string) *A2AProvider {
	return &A2AProvider{name: strings.ToLower(strings.TrimSpace(name)), client: &http.Client{Timeout: openAIKeylessTimeout}}
}

func (p *A2AProvider) credential() (*config.Credentials, string, string, error) {
	store, err := config.LoadCredentials()
	if err != nil {
		return nil, "", "", err
	}
	base := strings.TrimRight(store.A2AAgentURL(p.name), "/")
	if base == "" {
		return nil, "", "", fmt.Errorf("a2a:%s is not connected", p.name)
	}
	return store, base + "/a2a", store.Token(p.name), nil
}

func (p *A2AProvider) models() []string { return A2AModelIDs(p.name) }

func (p *A2AProvider) skillFor(model string) (string, bool) {
	prefix := "a2a-" + p.name + "-"
	for _, id := range p.models() {
		if strings.EqualFold(id, model) {
			return strings.TrimPrefix(id, prefix), true
		}
	}
	return "", false
}

func (p *A2AProvider) SupportsModel(model string) bool {
	_, ok := p.skillFor(model)
	return ok
}

func (p *A2AProvider) GetInfo() app.ProviderInfo {
	store, err := config.LoadCredentials()
	working := err == nil && (strings.EqualFold(store.State(p.name), "connected") || store.Token(p.name) != "") && len(p.models()) > 0
	defaultModel := ""
	if models := p.models(); len(models) > 0 {
		defaultModel = models[0]
	}
	return app.ProviderInfo{
		Name: "a2a:" + p.name, Label: "A2A " + p.name, Working: working,
		SupportedModels: p.models(), DefaultModel: defaultModel,
		Description: "Remote A2A agent skills exposed as voie models",
	}
}

type a2aRPCResult struct {
	ID        string `json:"id"`
	ContextID string `json:"contextId"`
	Status    struct {
		State string `json:"state"`
	} `json:"status"`
	Artifacts []struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"artifacts"`
}

func a2aText(result a2aRPCResult) string {
	var out strings.Builder
	for _, artifact := range result.Artifacts {
		for _, part := range artifact.Parts {
			out.WriteString(part.Text)
		}
	}
	return out.String()
}

func (p *A2AProvider) call(ctx context.Context, method, model string, messages []app.Message) (a2aRPCResult, error) {
	_, endpoint, token, err := p.credential()
	if err != nil {
		return a2aRPCResult{}, err
	}
	skill, ok := p.skillFor(model)
	if !ok {
		return a2aRPCResult{}, fmt.Errorf("a2a:%s: unsupported model %q", p.name, model)
	}
	parts := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		if message.Content == "" {
			continue
		}
		parts = append(parts, map[string]any{"text": message.Content, "mediaType": "text/plain"})
	}
	payload := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method,
		"params": map[string]any{"message": map[string]any{
			"role": "ROLE_USER", "parts": parts,
			"metadata": map[string]any{"skill": skill, "model": model},
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return a2aRPCResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return a2aRPCResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := p.client.Do(request)
	if err != nil {
		return a2aRPCResult{}, app.WrapNetworkFailure(err, "a2a:"+p.name, model)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return a2aRPCResult{}, app.NewProviderFailure(classifyStatus(response.StatusCode), "a2a:"+p.name, model, response.StatusCode, nil)
	}
	if strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		return a2aRPCResult{}, fmt.Errorf("a2a:%s: unexpected stream response", p.name)
	}
	var envelope struct {
		Result a2aRPCResult `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return a2aRPCResult{}, app.NewProviderFailure(app.FailureUnavailable, "a2a:"+p.name, model, 0, err)
	}
	if envelope.Error != nil {
		return a2aRPCResult{}, app.NewProviderFailure(app.FailureUnavailable, "a2a:"+p.name, model, envelope.Error.Code, fmt.Errorf("%s", envelope.Error.Message))
	}
	return envelope.Result, nil
}

func (p *A2AProvider) ChatCompletion(ctx context.Context, messages []app.Message, model string) (*app.ChatCompletionResponse, error) {
	result, err := p.call(ctx, "SendMessage", model, messages)
	if err != nil {
		return nil, err
	}
	text := a2aText(result)
	if text == "" {
		return nil, app.NewProviderFailure(app.FailureUnavailable, "a2a:"+p.name, model, 0, fmt.Errorf("empty response"))
	}
	return &app.ChatCompletionResponse{Model: model, Choices: []app.Choice{{Message: app.Message{Role: "assistant", Content: text}}}}, nil
}

func (p *A2AProvider) ChatCompletionStream(ctx context.Context, messages []app.Message, model string, callback func(string)) error {
	_, endpoint, token, err := p.credential()
	if err != nil {
		return err
	}
	skill, ok := p.skillFor(model)
	if !ok {
		return fmt.Errorf("a2a:%s: unsupported model %q", p.name, model)
	}
	payload := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "SendStreamingMessage",
		"params": map[string]any{"message": map[string]any{
			"role": "ROLE_USER", "parts": []any{map[string]any{"text": a2aLastUserMessage(messages), "mediaType": "text/plain"}},
			"metadata": map[string]any{"skill": skill, "model": model},
		}},
	}
	body, _ := json.Marshal(payload)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := p.client.Do(request)
	if err != nil {
		return app.WrapNetworkFailure(err, "a2a:"+p.name, model)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return app.NewProviderFailure(classifyStatus(response.StatusCode), "a2a:"+p.name, model, response.StatusCode, nil)
	}
	scanner := bufio.NewScanner(response.Body)
	delivered := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event struct {
			ArtifactUpdate *struct {
				Artifact struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"artifact"`
			} `json:"artifactUpdate"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			continue
		}
		if event.ArtifactUpdate == nil {
			continue
		}
		for _, part := range event.ArtifactUpdate.Artifact.Parts {
			if part.Text != "" {
				delivered = true
				callback(part.Text)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return app.WrapNetworkFailure(err, "a2a:"+p.name, model)
	}
	if !delivered {
		return app.NewProviderFailure(app.FailureUnavailable, "a2a:"+p.name, model, 0, fmt.Errorf("empty response"))
	}
	return nil
}

func a2aLastUserMessage(messages []app.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" || messages[i].Role == "" {
			return messages[i].Content
		}
	}
	return ""
}

var _ app.Provider = (*A2AProvider)(nil)
