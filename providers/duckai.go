package providers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/utils"
)

const (
	duckAIChatURL          = "https://duck.ai/duckchat/v1/chat"
	duckAIOrigin           = "https://duck.ai"
	duckAIReferer          = "https://duck.ai/"
	duckAIDefault          = "gpt-5.6-luna"
	duckAIBootstrap        = "https://duck.ai/"
	duckAIName             = "duckai"
	duckAIUserAgent        = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36"
	duckAIRequiredCookies  = "5=1; dcm=3; dcs=1"
	duckAIBootstrapTimeout = 60 * time.Second
	duckAIHTTPTimeout      = 180 * time.Second
	duckAISSEMaxLine       = 4 << 20
)

var duckAIModels = []string{
	"gpt-5.6-luna",
	"gpt-5.4-nano",
	"gpt-5.4-mini",
	"claude-haiku-4-5",
}

var duckAIModelAliases = map[string]string{
	"gpt-4o-mini":    "gpt-5.6-luna",
	"claude-3-haiku": "claude-haiku-4-5",
	"o4mini":         "gpt-5.4-mini",
}

type DuckAI struct {
	endpoint       string
	client         *http.Client
	captureHeaders func(context.Context) (duckAIHeaders, error)
}

type duckAIHeaders struct {
	VQDHash1  string
	FESignals string
	FEVersion string
	UserAgent string
}

type duckAIHeaderCaptureResult struct {
	headers duckAIHeaders
	err     error
}

type duckAIChatPayload struct {
	Model                string               `json:"model"`
	Messages             []Message            `json:"messages"`
	CanUseTools          bool                 `json:"canUseTools"`
	CanUseApproxLocation bool                 `json:"canUseApproxLocation"`
	ReasoningEffort      string               `json:"reasoningEffort"`
	DurableStream        *duckAIDurableStream `json:"durableStream"`
}

type duckAIStreamEvent struct {
	Role    string `json:"role"`
	Message string `json:"message"`
}

type duckAIDurableStream struct {
	MessageID      string          `json:"messageId"`
	ConversationID string          `json:"conversationId"`
	PublicKey      duckAIPublicJWK `json:"publicKey"`
}

type duckAIPublicJWK struct {
	Alg    string   `json:"alg"`
	E      string   `json:"e"`
	Ext    bool     `json:"ext"`
	KeyOps []string `json:"key_ops"`
	Kty    string   `json:"kty"`
	N      string   `json:"n"`
	Use    string   `json:"use"`
}

func NewDuckAI() *DuckAI {
	return &DuckAI{
		endpoint: duckAIChatURL,
		client:   &http.Client{Timeout: duckAIHTTPTimeout},
		captureHeaders: func(ctx context.Context) (duckAIHeaders, error) {
			return captureDuckAIHeaders(ctx)
		},
	}
}

var _ Provider = (*DuckAI)(nil)

func (p *DuckAI) GetInfo() ProviderInfo {
	return ProviderInfo{
		Name:               duckAIName,
		Label:              "Duck.ai",
		URL:                duckAIOrigin,
		Working:            true,
		DefaultModel:       duckAIDefault,
		SupportedModels:    append([]string(nil), duckAIModels...),
		SupportsStream:     true,
		NeedsAuth:          false,
		Description:        "Duck.ai models through the DuckDuckGo chat frontend",
		ReverseEngineering: true,
	}
}

func (p *DuckAI) SupportsModel(model string) bool {
	_, err := resolveDuckAIModel(model, false)
	return err == nil
}

func resolveDuckAIModel(model string, explicitProvider bool) (string, error) {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" || (explicitProvider && model == "openai") {
		return duckAIDefault, nil
	}
	if resolved, ok := duckAIModelAliases[model]; ok {
		return resolved, nil
	}
	for _, supported := range duckAIModels {
		if model == supported {
			return supported, nil
		}
	}
	return "", fmt.Errorf("duckai: unsupported model %q", model)
}

func newDuckAIDurableStream() (*duckAIDurableStream, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("duckai: generate durable stream key: %w", err)
	}
	messageID, err := duckAIRandomID()
	if err != nil {
		return nil, err
	}
	conversationID, err := duckAIRandomID()
	if err != nil {
		return nil, err
	}
	return &duckAIDurableStream{
		MessageID:      messageID,
		ConversationID: conversationID,
		PublicKey: duckAIPublicJWK{
			Alg:    "RSA-OAEP-256",
			E:      base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}),
			Ext:    true,
			KeyOps: []string{"encrypt"},
			Kty:    "RSA",
			N:      base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
			Use:    "enc",
		},
	}, nil
}

func duckAIRandomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("duckai: generate request ID: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func (p *DuckAI) ChatCompletion(ctx context.Context, messages []Message, model string) (*ChatCompletionResponse, error) {
	var content strings.Builder
	if err := p.ChatCompletionStream(ctx, messages, model, func(chunk string) { content.WriteString(chunk) }); err != nil {
		return nil, err
	}
	resolved, err := resolveDuckAIModel(model, true)
	if err != nil {
		return nil, err
	}
	id, err := duckAIRandomID()
	if err != nil {
		return nil, err
	}
	return CreateResponse("chatcmpl-"+id, resolved, content.String(), EstimatePromptTokens(messages)), nil
}

func (p *DuckAI) ChatCompletionStream(ctx context.Context, messages []Message, model string, callback func(chunk string)) error {
	resolvedModel, err := resolveDuckAIModel(model, true)
	if err != nil {
		return err
	}
	if p.captureHeaders == nil {
		return fmt.Errorf("duckai: header capture is not configured")
	}
	headers, err := p.captureHeaders(ctx)
	if err != nil {
		return fmt.Errorf("duckai: capture browser proof: %w", err)
	}
	if headers.VQDHash1 == "" || headers.FESignals == "" || headers.FEVersion == "" || headers.UserAgent == "" {
		return fmt.Errorf("duckai: browser returned incomplete chat headers")
	}
	durableStream, err := newDuckAIDurableStream()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(duckAIChatPayload{
		Model:                resolvedModel,
		Messages:             messages,
		CanUseTools:          false,
		CanUseApproxLocation: true,
		ReasoningEffort:      "none",
		DurableStream:        durableStream,
	})
	if err != nil {
		return fmt.Errorf("duckai: encode request: %w", err)
	}
	endpoint := p.endpoint
	if endpoint == "" {
		endpoint = duckAIChatURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("duckai: build request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", duckAIRequiredCookies)
	req.Header.Set("Origin", duckAIOrigin)
	req.Header.Set("Referer", duckAIReferer)
	req.Header.Set("User-Agent", headers.UserAgent)
	req.Header.Set("X-Vqd-Hash-1", headers.VQDHash1)
	req.Header.Set("x-fe-signals", headers.FESignals)
	req.Header.Set("x-fe-version", headers.FEVersion)
	req.Header.Set("x-ddg-journey-id", durableStream.ConversationID)
	client := p.client
	if client == nil {
		client = &http.Client{Timeout: duckAIHTTPTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return app.WrapNetworkFailure(err, "duckai", resolvedModel)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		utils.Debug("duckai: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		return app.NewProviderFailure(classifyStatus(resp.StatusCode), "duckai", resolvedModel, resp.StatusCode, nil)
	}
	return parseDuckAISSE(resp.Body, callback)
}

func parseDuckAISSE(body io.Reader, callback func(string)) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), duckAISSEMaxLine)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return nil
		}
		if data == "" || data == "ping" || data == "[PING]" || strings.HasPrefix(data, "[CHAT_TITLE:") {
			continue
		}
		var event duckAIStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			preview := []rune(data)
			if len(preview) > 120 {
				preview = preview[:120]
			}
			return fmt.Errorf("duckai: malformed SSE event %q: %w", string(preview), err)
		}
		if event.Role == "assistant" && event.Message != "" && callback != nil {
			callback(event.Message)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("duckai: read SSE response: %w", err)
	}
	return nil
}

func handleDuckAIPausedRequest(isChatRequest bool, rawHeaders map[string]interface{}, capture func(duckAIHeaders), abort func() error, continueRequest func() error) error {
	if !isChatRequest {
		return continueRequest()
	}
	headers := duckAIHeadersFromMap(rawHeaders)
	if err := abort(); err != nil {
		return err
	}
	if headers.VQDHash1 != "" && capture != nil {
		capture(headers)
	}
	return nil
}

func duckAIHeadersFromMap(raw map[string]interface{}) duckAIHeaders {
	var headers duckAIHeaders
	for name, value := range raw {
		text := fmt.Sprint(value)
		switch strings.ToLower(name) {
		case "x-vqd-hash-1":
			headers.VQDHash1 = text
		case "x-fe-signals":
			headers.FESignals = text
		case "x-fe-version":
			headers.FEVersion = text
		case "user-agent":
			headers.UserAgent = text
		}
	}
	return headers
}

func captureDuckAIHeaders(parent context.Context) (duckAIHeaders, error) {
	executable, err := findDuckAIBrowser()
	if err != nil {
		return duckAIHeaders{}, err
	}
	allocatorOptions := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	allocatorOptions = append(allocatorOptions,
		chromedp.ExecPath(executable),
		chromedp.Flag("headless", "new"),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("user-agent", duckAIUserAgent),
	)
	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(parent, allocatorOptions...)
	defer cancelAllocator()
	ctx, cancel := chromedp.NewContext(allocatorCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, duckAIBootstrapTimeout)
	defer cancelTimeout()

	captured := make(chan duckAIHeaderCaptureResult, 1)
	chromedp.ListenTarget(ctx, func(event interface{}) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		isChat := strings.Contains(paused.Request.URL, "/duckchat/v1/chat")
		// ListenTarget handles events synchronously. Dispatch the Fetch action
		// separately so it has a CDP executor and cannot block event delivery.
		go func() {
			var result duckAIHeaderCaptureResult
			result.err = chromedp.Run(ctx, chromedp.ActionFunc(func(actionCtx context.Context) error {
				return handleDuckAIPausedRequest(isChat, paused.Request.Headers, func(headers duckAIHeaders) {
					result.headers = headers
				}, func() error {
					return fetch.FailRequest(paused.RequestID, network.ErrorReasonAborted).Do(actionCtx)
				}, func() error {
					return fetch.ContinueRequest(paused.RequestID).Do(actionCtx)
				})
			}))
			if isChat {
				select {
				case captured <- result:
				default:
				}
			}
		}()
	})
	setPrompt := `(function() {
  const ta = document.querySelector('textarea[name="user-prompt"]');
  if (!ta) return 'missing textarea';
  const setter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value').set;
  setter.call(ta, 'apiai-duckai-calibration-' + Date.now());
  ta.dispatchEvent(new Event('input', { bubbles: true }));
  return 'ok';
})()`
	clickPrompt := `(function() {
  const button = document.querySelector('button[type="submit"]');
  if (!button || button.disabled) return 'submit unavailable';
  button.click();
  return 'clicked';
})()`
	var result string
	err = chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(`
Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
Object.defineProperty(navigator, 'languages', { get: () => ['en-US', 'en'] });
Object.defineProperty(navigator, 'platform', { get: () => 'Linux x86_64' });
			`).Do(ctx)
			return err
		}),
		network.Enable(),
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*duckchat/v1/chat*", RequestStage: fetch.RequestStageRequest}}),
		chromedp.Navigate(duckAIBootstrap),
		chromedp.WaitVisible(`textarea[name="user-prompt"]`),
		chromedp.Evaluate(setPrompt, &result),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(clickPrompt, &result),
	)
	if err != nil {
		return duckAIHeaders{}, fmt.Errorf("duckai: browser bootstrap failed: %w", err)
	}
	select {
	case result := <-captured:
		if result.err != nil {
			return duckAIHeaders{}, fmt.Errorf("duckai: handle paused calibration request: %w", result.err)
		}
		headers := result.headers
		if headers.VQDHash1 == "" || headers.FESignals == "" || headers.FEVersion == "" || headers.UserAgent == "" {
			return duckAIHeaders{}, fmt.Errorf("duckai: browser returned incomplete chat headers")
		}
		return headers, nil
	case <-ctx.Done():
		return duckAIHeaders{}, fmt.Errorf("duckai: timed out waiting for chat headers: %w", ctx.Err())
	}
}

func findDuckAIBrowser() (string, error) {
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("duckai: Chrome/Chromium is required for chat requests")
}
