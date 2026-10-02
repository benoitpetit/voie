package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/utils"
)

type Perplexity struct{}

const perplexityUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"

var perplexityModels = []string{
	"turbo",
	"pplx_pro",
	"gpt5",
	"gpt41",
	"gpt5_thinking",
	"o1",
	"o3",
	"o3pro",
	"o3mini",
	"o4mini",
	"r1",
	"claude45sonnet",
	"claude45sonnetthinking",
	"claude41opusthinking",
	"claude40opus",
	"claude40sonnetthinking",
	"claude37sonnetthinking",
	"claude35haiku",
	"gemini2flash",
	"gemini",
	"grok4",
	"grok",
	"llama_x_large",
	"mistral",
	"experimental",
	"pplx_pro_upgraded",
	"pplx_alpha",
	"pplx_beta",
	"pplx_reasoning",
}

var perplexityAliases = map[string]string{
	"perplexity":        "turbo",
	"pplx":              "turbo",
	"sonar":             "turbo",
	"gpt-5":             "gpt5",
	"gpt-4.1":           "gpt41",
	"gpt-5-thinking":    "gpt5_thinking",
	"o3-pro":            "o3pro",
	"gpt-4o":            "gpt41",
	"gpt-4":             "turbo",
	"claude-sonnet-4-5": "claude45sonnet",
	"claude-4-5-sonnet": "claude45sonnet",
	"claude-opus-4-1":   "claude41opusthinking",
	"claude-opus-4":     "claude40opus",
	"claude-3.7-sonnet": "claude37sonnetthinking",
	"claude-3.5-haiku":  "claude35haiku",
	"claude":            "claude45sonnet",
	"gemini-2-flash":    "gemini2flash",
	"gemini-flash":      "gemini2flash",
	"grok-4":            "grok4",
	"deepseek-r1":       "r1",
}

func (p *Perplexity) GetInfo() ProviderInfo {
	return ProviderInfo{
		Name:               "perplexity",
		Label:              "Perplexity AI",
		URL:                "https://www.perplexity.ai",
		Working:            true,
		DefaultModel:       "turbo",
		SupportedModels:    perplexityModels,
		SupportsStream:     true,
		NeedsAuth:          false,
		Description:        "Perplexity AI - reverse-engineered, anonymous access to many frontier models",
		ReverseEngineering: true,
	}
}

func (p *Perplexity) resolveModel(model string) string {
	lower := strings.ToLower(model)
	if lower == "" {
		return "turbo"
	}
	if aliased, ok := perplexityAliases[lower]; ok {
		return aliased
	}
	for _, m := range perplexityModels {
		if m == lower {
			return lower
		}
	}
	return "turbo"
}

func (p *Perplexity) SupportsModel(model string) bool {
	lower := strings.ToLower(model)
	if _, ok := perplexityAliases[lower]; ok {
		return true
	}
	for _, m := range perplexityModels {
		if m == lower {
			return true
		}
	}
	return false
}

func lastUserMessage(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	if len(messages) > 0 {
		return messages[len(messages)-1].Content
	}
	return ""
}

func systemMessageOf(messages []Message) string {
	for _, msg := range messages {
		if msg.Role == "system" {
			return msg.Content
		}
	}
	return ""
}

func (p *Perplexity) ChatCompletion(ctx context.Context, messages []Message, model string) (*ChatCompletionResponse, error) {
	var fullContent strings.Builder
	err := p.ChatCompletionStream(ctx, messages, model, func(chunk string) {
		fullContent.WriteString(chunk)
	})

	if err != nil {
		return nil, err
	}

	return CreateResponse(
		fmt.Sprintf("chatcmpl-%s", strings.ReplaceAll(uuid.New().String(), "-", "")[:24]),
		model,
		fullContent.String(),
		EstimatePromptTokens(messages),
	), nil
}

func (p *Perplexity) ChatCompletionStream(ctx context.Context, messages []Message, model string, callback func(chunk string)) error {
	prompt := lastUserMessage(messages)
	if prompt == "" {
		return fmt.Errorf("perplexity: no user message provided")
	}

	resolved := p.resolveModel(model)
	if sys := systemMessageOf(messages); sys != "" {
		prompt = sys + "\n\n" + prompt
	}

	payload := map[string]interface{}{
		"query_mode":                      "copilot",
		"query_str":                       prompt,
		"query":                           "",
		"dsl_query":                       prompt,
		"model_preference":                resolved,
		"is_incognito":                    false,
		"is_related_query":                false,
		"is_sponsored":                    false,
		"frontend_uuid":                   uuid.New().String(),
		"frontend_context_uuid":           uuid.New().String(),
		"prompt_source":                   "user",
		"query_source":                    "home",
		"skip_search_enabled":             true,
		"use_schematized_api":             true,
		"send_back_text_in_streaming_api": false,
		"time_from_first_type":            18361,
		"local_search_enabled":            false,
		"search_recency_filter":           nil,
		"client_coordinates":              nil,
		"mentions":                        []string{},
		"source":                          "default",
		"always_search_override":          false,
		"override_no_search":              false,
		"is_nav_suggestions_disabled":     false,
		"force_enable_browser_agent":      false,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://www.perplexity.ai/rest/sse/perplexity_ask", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://www.perplexity.ai")
	req.Header.Set("Referer", "https://www.perplexity.ai/")
	req.Header.Set("User-Agent", perplexityUA)
	req.Header.Set("x-perplexity-request-reason", "perplexity-query-state-provider")
	req.Header.Set("x-request-id", strings.ReplaceAll(uuid.New().String(), "-", ""))

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return app.WrapNetworkFailure(err, "perplexity", resolved)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		utils.Debug("perplexity: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		return app.NewProviderFailure(classifyStatus(resp.StatusCode), "perplexity", resolved, resp.StatusCode, nil)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	delivered := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}

		var evt struct {
			Blocks []struct {
				MarkdownBlock *struct {
					Answer   string `json:"answer"`
					Progress string `json:"progress"`
				} `json:"markdown_block"`
			} `json:"blocks"`
		}
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			continue
		}

		// Perplexity renvoie le bloc markdown complet à chaque étape : on ne
		// diffuse que la partie non encore envoyée.
		for _, block := range evt.Blocks {
			if block.MarkdownBlock == nil || block.MarkdownBlock.Answer == "" {
				continue
			}
			answer := block.MarkdownBlock.Answer
			if len(answer) > delivered {
				callback(answer[delivered:])
				delivered = len(answer)
			}
		}
	}

	return scanner.Err()
}
