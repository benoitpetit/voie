package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Quillbot struct{}

const quillbotUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36"

func (p *Quillbot) GetInfo() ProviderInfo {
	return ProviderInfo{
		Name:         "quillbot",
		Label:        "Quillbot AI",
		URL:          "https://quillbot.com/ai-chat",
		Working:      true,
		DefaultModel: "quillbot",
		SupportedModels: []string{
			"quillbot", "quillbot-search",
		},
		SupportsStream:     true,
		NeedsAuth:          false,
		Description:        "Quillbot AI Chat - reverse-engineered, no auth required",
		ReverseEngineering: true,
	}
}

func (p *Quillbot) SupportsModel(model string) bool {
	lower := strings.ToLower(model)
	return strings.Contains(lower, "quillbot")
}

func formatPromptQuillbot(messages []Message) string {
	var parts []string
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			parts = append(parts, msg.Content)
		case "user", "assistant":
			parts = append(parts, msg.Content)
		}
	}
	return strings.Join(parts, "\n\n")
}

func (p *Quillbot) ChatCompletion(ctx context.Context, messages []Message, model string) (*ChatCompletionResponse, error) {
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

func (p *Quillbot) ChatCompletionStream(ctx context.Context, messages []Message, model string, callback func(chunk string)) error {
	conversationID := uuid.New().String()
	url := fmt.Sprintf("https://quillbot.com/api/ai-chat/chat/conversation/%s", conversationID)

	payload := map[string]interface{}{
		"message": map[string]interface{}{
			"content": formatPromptQuillbot(messages) + "\n\n",
		},
		"context": map[string]interface{}{
			"editorContext":    "",
			"selectionContext": "",
			"userDialect":      "en-us",
			"apiVersion":       2,
		},
		"origin": map[string]interface{}{
			"name": "ai-chat.chat",
			"url":  "https://quillbot.com",
		},
	}

	if strings.ToLower(model) == "quillbot-search" {
		payload["tools"] = map[string]interface{}{
			"web_search_builtin": map[string]interface{}{},
		}
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://quillbot.com")
	req.Header.Set("Platform-Type", "webapp")
	req.Header.Set("QB-Product", "AI-CHAT")
	req.Header.Set("Referer", fmt.Sprintf("https://quillbot.com/ai-chat/c/%s", conversationID))
	req.Header.Set("User-Agent", quillbotUA)
	req.Header.Set("Useridtoken", "empty-token")
	req.Header.Set("Webapp-Version", "42.61.1")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("quillbot: status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var evt struct {
			Type    string `json:"type"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}

		if evt.Type == "content" && evt.Content != "" {
			callback(evt.Content)
		}
	}

	return scanner.Err()
}
