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

// CohereCommand implémente le provider CohereForAI C4AI Command (HuggingFace Space)
// Utilise le pattern SvelteKit/HuggingChat (conversation + FormData)
type CohereCommand struct{}

const cohereBaseURL = "https://coherelabs-c4ai-command.hf.space"

func (p *CohereCommand) GetInfo() ProviderInfo {
	return ProviderInfo{
		Name:         "cohere",
		Label:        "Cohere Command",
		URL:          cohereBaseURL,
		Working:      true,
		DefaultModel: "command-a-03-2025",
		SupportedModels: []string{
			"command-a-03-2025", "command-r-plus-08-2024", "command-r-08-2024",
			"command-r-plus", "command-r", "command-r7b-12-2024",
		},
		SupportsStream:     true,
		NeedsAuth:          false,
		Description:        "Cohere Command A & R models via HuggingFace Space",
		ReverseEngineering: true,
	}
}

func (p *CohereCommand) SupportsModel(model string) bool {
	aliases := map[string]string{
		"command-a":      "command-a-03-2025",
		"command-r-plus": "command-r-plus-08-2024",
		"command-r":      "command-r-08-2024",
		"command-r7b":    "command-r7b-12-2024",
		"cohere":         "command-a-03-2025",
	}
	if _, ok := aliases[model]; ok {
		return true
	}
	for _, m := range p.GetInfo().SupportedModels {
		if m == model {
			return true
		}
	}
	return false
}

func (p *CohereCommand) resolveModel(model string) string {
	aliases := map[string]string{
		"command-a":      "command-a-03-2025",
		"command-r-plus": "command-r-plus-08-2024",
		"command-r":      "command-r-08-2024",
		"command-r7b":    "command-r7b-12-2024",
		"cohere":         "command-a-03-2025",
	}
	if resolved, ok := aliases[model]; ok {
		return resolved
	}
	return model
}

// cohereConversation stocke l'état d'une conversation
type cohereConversation struct {
	conversationID string
	cookies        map[string]string
	messageID      string
}

func (p *CohereCommand) createConversation(ctx context.Context, model, systemPrompt string) (*cohereConversation, error) {
	data := map[string]string{
		"model":     model,
		"preprompt": systemPrompt,
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", cohereBaseURL+"/conversation", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", cohereBaseURL)
	req.Header.Set("Referer", cohereBaseURL+"/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:133.0) Gecko/20100101 Firefox/133.0")

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cohere: create conversation failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cohere: conversation status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("cohere: decode conversation: %w", err)
	}

	convID, _ := result["conversationId"].(string)
	if convID == "" {
		return nil, fmt.Errorf("cohere: no conversationId in response")
	}

	// Extraire les cookies
	cookies := make(map[string]string)
	for _, c := range resp.Cookies() {
		cookies[c.Name] = c.Value
	}

	return &cohereConversation{
		conversationID: convID,
		cookies:        cookies,
	}, nil
}

func (p *CohereCommand) getMessageID(ctx context.Context, conv *cohereConversation) error {
	url := fmt.Sprintf("%s/conversation/%s/__data.json?x-sveltekit-invalidated=11", cohereBaseURL, conv.conversationID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Referer", cohereBaseURL+"/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:133.0) Gecko/20100101 Firefox/133.0")

	// Ajouter les cookies
	for name, value := range conv.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("cohere: get data failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("cohere: read data: %w", err)
	}

	// Parse SvelteKit JSON format
	// Format: un objet JSON par ligne, on prend la première
	lines := strings.Split(string(body), "\n")
	if len(lines) == 0 {
		return fmt.Errorf("cohere: empty data response")
	}

	var svData map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &svData); err != nil {
		return fmt.Errorf("cohere: parse sveltekit data: %w", err)
	}

	// Naviguer dans la structure SvelteKit pour trouver le message_id
	nodes, ok := svData["nodes"].([]interface{})
	if !ok || len(nodes) < 2 {
		return fmt.Errorf("cohere: invalid nodes structure")
	}

	node1, ok := nodes[1].(map[string]interface{})
	if !ok {
		return fmt.Errorf("cohere: invalid node[1]")
	}

	// Vérifier si c'est une erreur
	if nodeType, _ := node1["type"].(string); nodeType == "error" {
		errMsg, _ := node1["error"].(string)
		return fmt.Errorf("cohere: server error: %s", errMsg)
	}

	data, ok := node1["data"].([]interface{})
	if !ok || len(data) == 0 {
		return fmt.Errorf("cohere: invalid data array")
	}

	// Le message_id est profondément imbriqué dans la structure SvelteKit
	// On cherche une string qui ressemble à un UUID dans les données
	messageID := p.findMessageID(data)
	if messageID == "" {
		return fmt.Errorf("cohere: could not find message_id")
	}

	conv.messageID = messageID
	return nil
}

// findMessageID cherche le message_id dans la structure SvelteKit
func (p *CohereCommand) findMessageID(data []interface{}) string {
	// La structure SvelteKit utilise des indices numériques récursifs
	// On cherche l'objet qui a un champ "messages", puis accède au dernier message
	// et extrait son "id"

	// Approche: parcourir les données et chercher une map avec "messages"
	for _, item := range data {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		// Chercher un champ qui pointe vers un index contenant des messages
		if messagesRef, ok := itemMap["messages"]; ok {
			// messagesRef est un index dans data
			if idx, ok := messagesRef.(float64); ok {
				intIdx := int(idx)
				if intIdx < len(data) {
					if msgArr, ok := data[intIdx].([]interface{}); ok && len(msgArr) > 0 {
						// Dernier message
						lastMsgIdx := msgArr[len(msgArr)-1]
						if msgIdxFloat, ok := lastMsgIdx.(float64); ok {
							msgIdx := int(msgIdxFloat)
							if msgIdx < len(data) {
								if msgMap, ok := data[msgIdx].(map[string]interface{}); ok {
									if idRef, ok := msgMap["id"]; ok {
										if idIdx, ok := idRef.(float64); ok {
											intIdIdx := int(idIdx)
											if intIdIdx < len(data) {
												if idStr, ok := data[intIdIdx].(string); ok {
													return idStr
												}
											}
										}
										if idStr, ok := idRef.(string); ok {
											return idStr
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// Fallback: chercher un UUID dans les strings
	for _, item := range data {
		if s, ok := item.(string); ok {
			if len(s) == 36 && strings.Count(s, "-") == 4 {
				return s
			}
		}
	}

	return ""
}

func (p *CohereCommand) sendMessage(ctx context.Context, conv *cohereConversation, input, model string) (io.ReadCloser, error) {
	// Préparer le FormData
	jsonPayload, err := json.Marshal(map[string]interface{}{
		"inputs":      input,
		"id":          conv.messageID,
		"is_retry":    false,
		"is_continue": false,
		"web_search":  false,
		"tools":       []interface{}{},
	})
	if err != nil {
		return nil, err
	}

	// Construire le multipart form data
	boundary := fmt.Sprintf("----WebKitFormBoundary%d", time.Now().UnixNano())
	var body bytes.Buffer
	body.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	body.WriteString("Content-Disposition: form-data; name=\"data\"\r\n")
	body.WriteString("Content-Type: application/json\r\n\r\n")
	body.Write(jsonPayload)
	body.WriteString(fmt.Sprintf("\r\n--%s--\r\n", boundary))

	url := fmt.Sprintf("%s/conversation/%s", cohereBaseURL, conv.conversationID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", fmt.Sprintf("multipart/form-data; boundary=%s", boundary))
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", cohereBaseURL)
	req.Header.Set("Referer", cohereBaseURL+"/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:133.0) Gecko/20100101 Firefox/133.0")

	// Ajouter les cookies
	for name, value := range conv.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, app.WrapNetworkFailure(err, "cohere", model)
	}

	if resp.StatusCode != http.StatusOK {
		body2, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		utils.Debug("cohere: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body2)))
		return nil, app.NewProviderFailure(classifyStatus(resp.StatusCode), "cohere", model, resp.StatusCode, nil)
	}

	return resp.Body, nil
}

func (p *CohereCommand) ChatCompletion(ctx context.Context, messages []Message, model string) (*ChatCompletionResponse, error) {
	model = p.resolveModel(model)

	systemPrompt := extractSystemPrompt(messages)
	userMessages := filterNonSystemMessages(messages)
	prompt := formatPrompt(userMessages)

	// Step 1: Create conversation
	conv, err := p.createConversation(ctx, model, systemPrompt)
	if err != nil {
		return nil, err
	}

	// Step 2: Get message ID
	if err := p.getMessageID(ctx, conv); err != nil {
		return nil, err
	}

	// Step 3: Send message and read response
	respBody, err := p.sendMessage(ctx, conv, prompt, model)
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	// Parse response chunks
	var fullResponse strings.Builder
	scanner := bufio.NewScanner(respBody)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}

		chunkType, _ := chunk["type"].(string)
		if chunkType == "stream" {
			token, _ := chunk["token"].(string)
			token = strings.ReplaceAll(token, "\u0000", "")
			fullResponse.WriteString(token)
		} else if chunkType == "finalAnswer" {
			break
		}
	}

	content := fullResponse.String()
	if content == "" {
		return nil, fmt.Errorf("cohere: empty response")
	}

	promptTokens := EstimatePromptTokens(messages)
	return CreateResponse(
		fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		model,
		content,
		promptTokens,
	), nil
}

func (p *CohereCommand) ChatCompletionStream(ctx context.Context, messages []Message, model string, callback func(chunk string)) error {
	model = p.resolveModel(model)

	systemPrompt := extractSystemPrompt(messages)
	userMessages := filterNonSystemMessages(messages)
	prompt := formatPrompt(userMessages)

	// Step 1: Create conversation
	conv, err := p.createConversation(ctx, model, systemPrompt)
	if err != nil {
		return err
	}

	// Step 2: Get message ID
	if err := p.getMessageID(ctx, conv); err != nil {
		return err
	}

	// Step 3: Send message and stream response
	respBody, err := p.sendMessage(ctx, conv, prompt, model)
	if err != nil {
		return err
	}
	defer respBody.Close()

	scanner := bufio.NewScanner(respBody)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}

		chunkType, _ := chunk["type"].(string)
		if chunkType == "stream" {
			token, _ := chunk["token"].(string)
			token = strings.ReplaceAll(token, "\u0000", "")
			if token != "" {
				callback(token)
			}
		} else if chunkType == "finalAnswer" {
			break
		}
	}

	return nil
}

// filterNonSystemMessages filtre les messages système
func filterNonSystemMessages(messages []Message) []Message {
	var result []Message
	for _, msg := range messages {
		if msg.Role != "system" {
			result = append(result, msg)
		}
	}
	return result
}

// Extrait le prompt système
func extractSystemPrompt(messages []Message) string {
	for _, msg := range messages {
		if msg.Role == "system" {
			return msg.Content
		}
	}
	return ""
}

// Formate les messages en prompt simple
func formatPrompt(messages []Message) string {
	var prompt strings.Builder
	for _, msg := range messages {
		if msg.Role == "user" || msg.Role == "assistant" {
			prompt.WriteString(msg.Role + ": " + msg.Content + "\n")
		}
	}
	return prompt.String()
}
