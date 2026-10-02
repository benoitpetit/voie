package providers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/utils"
)

type DeepAI struct{}

const deepaiUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36"

// deepaiHash calcule le MD5 d'une chaîne puis inverse l'ordre de ses
// caractères hexadécimaux — la transformation appliquée par deepai.org.
func deepaiHash(input string) string {
	sum := md5.Sum([]byte(input))
	hexed := hex.EncodeToString(sum[:])
	b := []byte(hexed)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

// generateDeepAIKey reproduit l'algorithme côté client de deepai.org qui
// dérive une clé d'API anonyme ("tryit-...") à partir du User-Agent.
func generateDeepAIKey(userAgent string) string {
	randomStr := fmt.Sprintf("%.0f", rand.Float64()*100000000000)

	h1 := deepaiHash(userAgent + randomStr + "hackers_become_a_little_stinkier_every_time_they_hack")
	h2 := deepaiHash(userAgent + h1)
	h3 := deepaiHash(userAgent + h2)

	return fmt.Sprintf("tryit-%s-%s", randomStr, h3)
}

func (p *DeepAI) GetInfo() ProviderInfo {
	return ProviderInfo{
		Name:         "deepai",
		Label:        "DeepAI Chat",
		URL:          "https://deepai.org/chat",
		Working:      true,
		DefaultModel: "standard",
		SupportedModels: []string{
			"standard", "online", "gemma-4", "gemini-2.5-flash-lite", "deepseek-v3.2",
		},
		SupportsStream:     true,
		NeedsAuth:          false,
		Description:        "DeepAI chat - reverse-engineered anonymous API key generation",
		ReverseEngineering: true,
	}
}

var deepaiModels = map[string]bool{
	"standard":              true,
	"online":                true,
	"gemma-4":               true,
	"gemini-2.5-flash-lite": true,
	"deepseek-v3.2":         true,
}

func (p *DeepAI) resolveModel(model string) string {
	lower := strings.ToLower(model)
	if deepaiModels[lower] {
		return lower
	}
	return "standard"
}

func (p *DeepAI) SupportsModel(model string) bool {
	lower := strings.ToLower(model)
	if deepaiModels[lower] {
		return true
	}
	return strings.Contains(lower, "deepseek-v3")
}

func (p *DeepAI) ChatCompletion(ctx context.Context, messages []Message, model string) (*ChatCompletionResponse, error) {
	var fullContent strings.Builder
	err := p.ChatCompletionStream(ctx, messages, model, func(chunk string) {
		fullContent.WriteString(chunk)
	})

	if err != nil {
		return nil, err
	}

	return CreateResponse(
		fmt.Sprintf("chatcmpl-%s", strings.ReplaceAll(uuid.New().String(), "-", "")[:24]),
		p.resolveModel(model),
		fullContent.String(),
		EstimatePromptTokens(messages),
	), nil
}

func (p *DeepAI) ChatCompletionStream(ctx context.Context, messages []Message, model string, callback func(chunk string)) error {
	model = p.resolveModel(model)

	history, err := json.Marshal(messages)
	if err != nil {
		return err
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	fields := [][2]string{
		{"chat_style", "chat"},
		{"chatHistory", string(history)},
		{"model", model},
		{"session_uuid", uuid.New().String()},
		{"sensitivity_request_id", uuid.New().String()},
		{"hacker_is_stinky", "very_stinky"},
		{"enabled_tools", `["image_generator","image_editor"]`},
	}
	for _, f := range fields {
		if err := writer.WriteField(f[0], f[1]); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.deepai.org/hacking_is_a_serious_crime", &body)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("api-key", generateDeepAIKey(deepaiUA))
	req.Header.Set("User-Agent", deepaiUA)
	req.Header.Set("Origin", "https://deepai.org")
	req.Header.Set("Referer", "https://deepai.org/chat")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return app.WrapNetworkFailure(err, "deepai", model)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		utils.Debug("deepai: status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
		return app.NewProviderFailure(classifyStatus(resp.StatusCode), "deepai", model, resp.StatusCode, nil)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var pending strings.Builder
	flushing := false

	for scanner.Scan() {
		line := scanner.Text()
		if strings.ContainsRune(line, 0x1c) {
			flushing = true
		}
		if flushing {
			pending.WriteString(line)
			continue
		}
		if line != "" {
			callback(line)
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	if flushing {
		parts := strings.Split(pending.String(), "\x1c")
		for i, part := range parts {
			if strings.TrimSpace(part) == "" {
				continue
			}
			if i == 0 {
				callback(part)
				continue
			}
			var probe map[string]interface{}
			if json.Unmarshal([]byte(part), &probe) == nil {
				continue
			}
			callback(part)
		}
	}

	return nil
}
