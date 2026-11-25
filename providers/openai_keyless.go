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
	Name                   string
	Label                  string
	URL                    string
	CompletionsURL         string
	URLSupplier            func() string            // dynamic models/health URL
	CompletionsURLSupplier func() string            // dynamic chat URL
	ModelsSupplier         func() map[string]string // dynamically refreshed model map
	DefaultModel           string
	ModelIDs               map[string]string // voie ID -> upstream model ID
	Description            string
	SupportsStream         bool
	Headers                map[string]string           // optional extra headers (e.g. a loopback gateway key)
	TokenSupplier          func() string               // resolves the bearer token per request
	BodyHint               func(int, []byte) string    // optional upstream-specific error detail
	ResponseHint           func(string) error          // optional answer validation (e.g. expired session)
	OmitDefaultMaxTokens   bool                        // gateway rejects unsupported generation controls
	Ensure                 func(context.Context) error // optional lazy gateway startup
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
	Model         string             `json:"model"`
	Messages      []Message          `json:"messages"`
	Stream        bool               `json:"stream"`
	MaxTokens     *int               `json:"max_tokens,omitempty"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	Stop          []string           `json:"stop,omitempty"`
	Tools         []map[string]any   `json:"tools,omitempty"`
	ToolChoice    any                `json:"tool_choice,omitempty"`
	StreamOptions *app.StreamOptions `json:"stream_options,omitempty"`
}

type openAIKeylessCompletion struct {
	Choices []struct {
		Message struct {
			Content   string      `json:"content"`
			ToolCalls interface{} `json:"tool_calls,omitempty"`
		} `json:"message"`
		Delta struct {
			Content   string      `json:"content"`
			ToolCalls interface{} `json:"tool_calls,omitempty"`
		} `json:"delta"`
	} `json:"choices"`
}

func (p *openAIKeylessProvider) resolveURL() string {
	if p.cfg.URLSupplier != nil {
		return p.cfg.URLSupplier()
	}
	return p.cfg.URL
}

func (p *openAIKeylessProvider) modelIDs() map[string]string {
	ids := make(map[string]string, len(p.cfg.ModelIDs))
	for id, upstream := range p.cfg.ModelIDs {
		ids[id] = upstream
	}
	if p.cfg.ModelsSupplier != nil {
		for id, upstream := range p.cfg.ModelsSupplier() {
			ids[id] = upstream
		}
	}
	return ids
}

func (p *openAIKeylessProvider) GetInfo() ProviderInfo {
	all := p.modelIDs()
	models := make([]string, 0, len(all))
	for id := range all {
		models = append(models, id)
	}
	sort.Strings(models)
	return ProviderInfo{
		Name:               p.cfg.Name,
		Label:              p.cfg.Label,
		URL:                p.resolveURL(),
		Working:            true,
		SupportedModels:    models,
		DefaultModel:       p.cfg.DefaultModel,
		SupportsStream:     p.cfg.SupportsStream,
		NeedsAuth:          false,
		Description:        p.cfg.Description,
		ReverseEngineering: false,
	}
}

func (p *openAIKeylessProvider) lookupModelID(model string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(model))
	for id, upstream := range p.modelIDs() {
		if strings.EqualFold(id, key) {
			return upstream, true
		}
	}
	return "", false
}

// UpstreamModel returns the ID sent to the upstream service for a public ID.
func (p *openAIKeylessProvider) UpstreamModel(model string) string {
	upstream, err := p.resolve(model)
	if err != nil {
		return ""
	}
	return upstream
}

func (p *openAIKeylessProvider) SupportsModel(model string) bool {
	_, ok := p.lookupModelID(model)
	return ok
}

func (p *openAIKeylessProvider) resolve(model string) (string, error) {
	upstream, ok := p.lookupModelID(model)
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
	options := app.RequestOptionsFrom(ctx)
	var maxTokens *int
	if options.MaxTokens != nil {
		maxTokens = options.MaxTokens
	} else if !p.cfg.OmitDefaultMaxTokens {
		defaultTokens := 1024
		maxTokens = &defaultTokens
	}
	payload, err := json.Marshal(openAIKeylessRequest{
		Model: model, Messages: messages, Stream: stream, MaxTokens: maxTokens,
		Temperature: options.Temperature, TopP: options.TopP, Stop: options.Stop,
		Tools: options.Tools, ToolChoice: options.ToolChoice, StreamOptions: options.StreamOptions,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: encode request: %w", p.cfg.Name, err)
	}
	completionsURL := p.cfg.CompletionsURL
	if p.cfg.CompletionsURLSupplier != nil {
		completionsURL = p.cfg.CompletionsURLSupplier()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, completionsURL, bytes.NewReader(payload))
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
	if p.cfg.TokenSupplier != nil {
		req.Header.Set("Authorization", "Bearer "+p.cfg.TokenSupplier())
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
		failure := app.NewProviderFailure(classifyStatus(resp.StatusCode), p.cfg.Name, model, resp.StatusCode, nil)
		if p.cfg.BodyHint != nil {
			if hint := p.cfg.BodyHint(resp.StatusCode, body); hint != "" {
				return nil, fmt.Errorf("%w: %s", failure, hint)
			}
		}
		return nil, failure
	}
	return resp, nil
}

func (p *openAIKeylessProvider) decodeCompletion(body io.Reader, messages []Message, model string) (*ChatCompletionResponse, error) {
	var parsed openAIKeylessCompletion
	if err := json.NewDecoder(io.LimitReader(body, 8<<20)).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("%s: decode response: %w", p.cfg.Name, err)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("%s: empty response", p.cfg.Name)
	}
	message := parsed.Choices[0].Message
	if message.Content == "" && message.ToolCalls == nil {
		return nil, fmt.Errorf("%s: empty response", p.cfg.Name)
	}
	if p.cfg.ResponseHint != nil {
		if err := p.cfg.ResponseHint(message.Content); err != nil {
			return nil, err
		}
	}
	response := CreateResponse("chatcmpl-"+uuid.New().String(), model, message.Content, EstimatePromptTokens(messages))
	if message.ToolCalls != nil {
		response.Choices[0].Message.ToolCalls = message.ToolCalls
	}
	return response, nil
}

func (p *openAIKeylessProvider) decodeStream(ctx context.Context, body io.Reader, callback func(string)) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var content strings.Builder
	sawToolCalls := false
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
		if len(parsed.Choices) == 0 {
			continue
		}
		delta := parsed.Choices[0].Delta
		if calls := delta.ToolCalls; calls != nil {
			sawToolCalls = true
			if sink := app.ToolCallSinkFrom(ctx); sink != nil {
				sink(calls)
			}
		}
		if delta.Content == "" {
			continue
		}
		content.WriteString(delta.Content)
		callback(delta.Content)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("%s: read stream: %w", p.cfg.Name, err)
	}
	if content.Len() == 0 && !sawToolCalls {
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
	return p.decodeStream(ctx, resp.Body, callback)
}
