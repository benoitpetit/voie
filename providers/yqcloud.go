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

	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/utils"
)

type Yqcloud struct{}

func (p *Yqcloud) GetInfo() ProviderInfo {
	return ProviderInfo{
		Name:         "yqcloud",
		Label:        "Yqcloud",
		URL:          "https://chat9.yqcloud.top",
		Working:      true,
		DefaultModel: "gpt-4",
		SupportedModels: []string{
			"gpt-4", "gpt-3.5-turbo",
		},
		SupportsStream:     true,
		NeedsAuth:          false,
		Description:        "Yqcloud free AI provider",
		ReverseEngineering: true,
	}
}

func (p *Yqcloud) SupportsModel(model string) bool {
	supported := []string{"gpt-4", "gpt-3.5", "yqcloud"}
	for _, m := range supported {
		if strings.Contains(model, m) {
			return true
		}
	}
	return false
}

func formatPromptYqcloud(messages []Message) string {
	var prompt strings.Builder
	for _, msg := range messages {
		switch msg.Role {
		case "user", "assistant":
			prompt.WriteString(msg.Content + "\n")
		}
	}
	return prompt.String()
}

func (p *Yqcloud) ChatCompletion(ctx context.Context, messages []Message, model string) (*ChatCompletionResponse, error) {
	var fullContent strings.Builder
	err := p.ChatCompletionStream(ctx, messages, model, func(chunk string) {
		fullContent.WriteString(chunk)
	})

	if err != nil {
		return nil, err
	}

	promptTokens := EstimatePromptTokens(messages)
	return CreateResponse(
		fmt.Sprintf("chatcmpl-%d", time.Now().Unix()),
		model,
		fullContent.String(),
		promptTokens,
	), nil
}

func (p *Yqcloud) ChatCompletionStream(ctx context.Context, messages []Message, model string, callback func(chunk string)) error {
	url := "https://api.binjie.fun/api/generateStream"

	// Extraire le message système s'il existe
	systemMessage := ""
	currentMessages := messages
	if len(currentMessages) > 0 && currentMessages[0].Role == "system" {
		systemMessage = currentMessages[0].Content
		currentMessages = currentMessages[1:]
	}

	prompt := formatPromptYqcloud(currentMessages)
	userId := fmt.Sprintf("#/chat/%d", time.Now().UnixMilli())

	payload := map[string]interface{}{
		"prompt":         prompt,
		"userId":         userId,
		"network":        true,
		"system":         systemMessage,
		"withoutContext": false,
		"stream":         true,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DNT", "1")
	req.Header.Set("Origin", "https://chat9.yqcloud.top")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Referer", "https://chat9.yqcloud.top/")
	req.Header.Set("Sec-CH-UA", `"Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", `"Linux"`)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	client := &http.Client{
		Timeout: 120 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return app.WrapNetworkFailure(err, "yqcloud", model)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		utils.Debug("yqcloud: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		return app.NewProviderFailure(classifyStatus(resp.StatusCode), "yqcloud", model, resp.StatusCode, nil)
	}

	// Lire le contenu par chunks
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		chunk := scanner.Text()
		if chunk != "" {
			callback(chunk)
		}
	}

	return scanner.Err()
}
