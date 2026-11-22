package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/utils"
)

const openAIKeylessTimeout = 120 * time.Second

// openAIKeylessConfig describes an anonymous, OpenAI-compatible chat endpoint.
// CompletionsURL is the full chat-completions URL; URL is the health-probe URL.
type openAIKeylessConfig struct {
	Name           string
	Label          string
	URL            string
	CompletionsURL string
	DefaultModel   string
	ModelIDs       map[string]string // voie ID -> upstream model ID
	Description    string
	SupportsStream bool
	Headers        map[string]string // optional extra headers (e.g. a loopback gateway key)
	Ensure         func(context.Context) error // optional lazy gateway startup
}

type openAIKeylessProvider struct {
	cfg    openAIKeylessConfig
	client *http.Client
}

func newOpenAIKeylessProvider(cfg openAIKeylessConfig, client *http.Client) *openAIKeylessProvider {
	if client == nil {
		client = &http.Client{Timeout: openAIKeylessTimeout}
	}
	return &openAIKeylessProvider{cfg: cfg, client: client}
}

var _ app.Provider = (*openAIKeylessProvider)(nil)

type openAIKeylessRequest struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	Stream    bool      `json:"stream"`
	MaxTokens int       `json:"max_tokens"`
}

type openAIKeylessCompletion struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

func (p *openAIKeylessProvider) GetInfo() ProviderInfo {
	models := make([]string, 0, len(p.cfg.ModelIDs))
	for id := range p.cfg.ModelIDs {
		models = append(models, id)
	}
	sort.Strings(models)
	return ProviderInfo{
		Name:               p.cfg.Name,
		Label:              p.cfg.Label,
		URL:                p.cfg.URL,
		Working:            true,
		SupportedModels:    models,
		DefaultModel:       p.cfg.DefaultModel,
		SupportsStream:     p.cfg.SupportsStream,
		NeedsAuth:          false,
		Description:        p.cfg.Description,
		ReverseEngineering: false,
	}
}

func (p *openAIKeylessProvider) SupportsModel(model string) bool {
	_, ok := p.cfg.ModelIDs[strings.ToLower(strings.TrimSpace(model))]
	return ok
}

func (p *openAIKeylessProvider) resolve(model string) (string, error) {
	upstream, ok := p.cfg.ModelIDs[strings.ToLower(strings.TrimSpace(model))]
	if !ok {
		return "", fmt.Errorf("%s: unsupported model %q", p.cfg.Name, model)
	}
	return upstream, nil
}

func (p *openAIKeylessProvider) request(ctx context.Context, messages []Message, model string, stream bool) (*http.Response, error) {
	if p.cfg.Ensure != nil {
		if err := p.cfg.Ensure(ctx); err != nil {
			return nil, app.WrapNetworkFailure(err, p.cfg.Name, model)
		}
	}
	payload, err := json.Marshal(openAIKeylessRequest{
		Model:     model,
		Messages:  messages,
		Stream:    stream,
		MaxTokens: 1024,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: encode request: %w", p.cfg.Name, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.CompletionsURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("%s: build request: %w", p.cfg.Name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	names := make([]string, 0, len(p.cfg.Headers))
	for name := range p.cfg.Headers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		req.Header.Set(name, p.cfg.Headers[name])
	}
	// Anonymous lanes set no Authorization header; a local gateway may supply one.
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, app.WrapNetworkFailure(err, p.cfg.Name, model)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		utils.Debug("%s: status %d: %s", p.cfg.Name, resp.StatusCode, strings.TrimSpace(string(body)))
		_ = resp.Body.Close()
		return nil, app.NewProviderFailure(classifyStatus(resp.StatusCode), p.cfg.Name, model, resp.StatusCode, nil)
	}
	return resp, nil
}

func (p *openAIKeylessProvider) decodeCompletion(body io.Reader, messages []Message, model string) (*ChatCompletionResponse, error) {
	var parsed openAIKeylessCompletion
	if err := json.NewDecoder(io.LimitReader(body, 8<<20)).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("%s: decode response: %w", p.cfg.Name, err)
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return nil, fmt.Errorf("%s: empty response", p.cfg.Name)
	}
	return CreateResponse("chatcmpl-"+uuid.New().String(), model, parsed.Choices[0].Message.Content, EstimatePromptTokens(messages)), nil
}

func (p *openAIKeylessProvider) decodeStream(body io.Reader, callback func(string)) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var content strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}
		var parsed openAIKeylessCompletion
		if err := json.Unmarshal([]byte(data), &parsed); err != nil {
			continue
		}
		if len(parsed.Choices) == 0 || parsed.Choices[0].Delta.Content == "" {
			continue
		}
		delta := parsed.Choices[0].Delta.Content
		content.WriteString(delta)
		callback(delta)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("%s: read stream: %w", p.cfg.Name, err)
	}
	if content.Len() == 0 {
		return fmt.Errorf("%s: empty response", p.cfg.Name)
	}
	return nil
}

func (p *openAIKeylessProvider) ChatCompletion(ctx context.Context, messages []Message, model string) (*ChatCompletionResponse, error) {
	upstream, err := p.resolve(model)
	if err != nil {
		return nil, err
	}
	resp, err := p.request(ctx, messages, upstream, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return p.decodeCompletion(resp.Body, messages, model)
}

func (p *openAIKeylessProvider) ChatCompletionStream(ctx context.Context, messages []Message, model string, callback func(chunk string)) error {
	upstream, err := p.resolve(model)
	if err != nil {
		return err
	}
	if !p.cfg.SupportsStream {
		response, err := p.ChatCompletion(ctx, messages, model)
		if err != nil {
			return err
		}
		callback(response.Choices[0].Message.Content)
		return nil
	}
	resp, err := p.request(ctx, messages, upstream, true)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return p.decodeStream(resp.Body, callback)
}
