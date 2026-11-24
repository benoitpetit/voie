package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/utils"
)

type Perplexity struct{}

// perplexityCookies returns an optional Cookie header captured from an
// anonymous browser session. Perplexity's edge cookies (pplx.edge-sid and
// pplx.edge-vid) are set by its JavaScript and cannot be obtained with a plain
// HTTP GET; without them the endpoint returns a "Sign up" block.
func perplexityCookies() string {
	return strings.TrimSpace(os.Getenv("VOIE_PERPLEXITY_COOKIES"))
}

const perplexityUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Safari/537.36"

// Observed from an anonymous Chromium session on 2026-11-24. Perplexity now
// wraps the ask payload in `params`, uses a different request reason and needs
// the pplx.edge-sid / pplx.edge-vid cookies set by its own JavaScript. Without
// those cookies the endpoint answers 200 with a "Sign up" text block.
const (
	perplexityAskEndpoint = "https://www.perplexity.ai/rest/sse/perplexity_ask"
	perplexityAPIVersion  = "2.18"
)

var perplexityBlockUseCases = []string{
	"answer_modes", "media_items", "inline_entity_cards", "place_widgets", "finance_widgets",
	"news_widgets", "search_result_widgets", "inline_images", "inline_assets", "placeholder_cards",
	"diff_blocks", "entity_group_v2", "refinement_filters", "canvas_mode", "maps_preview",
	"answer_tabs", "preserve_latex", "in_context_suggestions", "pending_followups", "inline_claims",
	"unified_assets", "workflow_steps", "workflow_widgets", "navigation_results", "background_agents",
}

// Only `turbo` answers anonymously with the current wire format; every other
// advertised model returns a "Sign up and repeat your request" text block.
var perplexityModels = []string{"turbo"}

var perplexityAliases = map[string]string{
	"perplexity": "turbo",
	"pplx":       "turbo",
	"sonar":      "turbo",
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
	resolvedModel := p.resolveModel(model)
	var fullContent strings.Builder
	err := p.ChatCompletionStream(ctx, messages, resolvedModel, func(chunk string) {
		fullContent.WriteString(chunk)
	})

	if err != nil {
		return nil, err
	}

	return CreateResponse(
		fmt.Sprintf("chatcmpl-%s", strings.ReplaceAll(uuid.New().String(), "-", "")[:24]),
		resolvedModel,
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

	requestID := uuid.New().String()
	zone := time.Now().Location().String()
	if zone == "" {
		zone = "UTC"
	}
	params := map[string]interface{}{
		"attachments":                          []string{},
		"language":                             "en-US",
		"timezone":                             zone,
		"search_focus":                         "internet",
		"sources":                              []string{"web"},
		"frontend_uuid":                        requestID,
		"mode":                                 "copilot",
		"model_preference":                     resolved,
		"is_related_query":                     false,
		"is_sponsored":                         false,
		"frontend_context_uuid":                uuid.New().String(),
		"prompt_source":                        "user",
		"query_source":                         "home",
		"is_incognito":                         false,
		"time_from_first_type":                 900,
		"local_search_enabled":                 false,
		"use_schematized_api":                  true,
		"send_back_text_in_streaming_api":      false,
		"supported_block_use_cases":            perplexityBlockUseCases,
		"client_coordinates":                   nil,
		"mentions":                             []string{},
		"dsl_query":                            prompt,
		"skip_search_enabled":                  true,
		"is_nav_suggestions_disabled":          false,
		"source":                               "default",
		"always_search_override":               false,
		"override_no_search":                   false,
		"should_ask_for_mcp_tool_confirmation": true,
		"supports_tool_approval_modal":         true,
		"experiment_assignments":               map[string]string{},
		"force_enable_browser_agent":           false,
		"supported_features":                   []string{"browser_agent_permission_banner_v1.1"},
		"extended_context":                     false,
		"is_browser_extension_upsell_enabled":  true,
		"version":                              perplexityAPIVersion,
		"rum_session_id":                       uuid.New().String(),
	}
	payload := map[string]interface{}{"params": params, "query_str": prompt}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, perplexityAskEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", "https://www.perplexity.ai/")
	req.Header.Set("User-Agent", perplexityUA)
	req.Header.Set("x-perplexity-request-try-number", "1")
	req.Header.Set("x-perplexity-request-endpoint", perplexityAskEndpoint)
	req.Header.Set("x-perplexity-request-reason", "ask-query-state-provider")
	req.Header.Set("x-request-id", requestID)
	req.Header.Set("sec-ch-ua", `"Chromium";v="155", "Not(A:Brand";v="24"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Linux"`)
	if cookies := perplexityCookies(); cookies != "" {
		req.Header.Set("Cookie", cookies)
	}

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
		if data == "" || data == "[DONE]" || strings.HasPrefix(data, "[DONE]") {
			continue
		}

		var evt map[string]interface{}
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			continue
		}

		// Perplexity streams the full answer in nested workflow blocks. Older
		// responses used blocks[].markdown_block.answer; both are collected.
		var answers []string
		var walk func(interface{})
		walk = func(node interface{}) {
			switch value := node.(type) {
			case map[string]interface{}:
				if text, ok := value["text_payload"].(map[string]interface{}); ok {
					if answer, ok := text["text"].(string); ok && answer != "" {
						answers = append(answers, answer)
					}
				}
				if block, ok := value["markdown_block"].(map[string]interface{}); ok {
					if answer, ok := block["answer"].(string); ok && answer != "" {
						answers = append(answers, answer)
					}
				}
				for _, child := range value {
					walk(child)
				}
			case []interface{}:
				for _, child := range value {
					walk(child)
				}
			}
		}
		walk(evt)

		// Only emit the part not delivered yet: each SSE event replays the
		// accumulated answer rather than emitting deltas.
		for _, answer := range answers {
			if len(answer) > delivered {
				callback(answer[delivered:])
				delivered = len(answer)
			}
		}
	}

	return scanner.Err()
}
