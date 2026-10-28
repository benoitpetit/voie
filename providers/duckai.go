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
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/emulation"
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
	duckAIDefault          = "gpt-6-luna"
	duckAIBootstrap        = "https://duck.ai/"
	duckAIName             = "duckai"
	duckAIBootstrapTimeout = 60 * time.Second
	duckAIHTTPTimeout      = 180 * time.Second
	duckAISSEMaxLine       = 4 << 20
)

var duckAIModels = []string{
	"gpt-6-luna",
	"gpt-5.6-luna",
	"gpt-5.4-nano",
	"gpt-5.4-mini",
	"claude-haiku-4-5",
	"mistral-small-2603",
	"tinfoil/gpt-oss-120b",
	"tinfoil/gemma4-31b",
}

var duckAIModelAliases = map[string]string{
	"gpt-4o-mini":     "gpt-5.6-luna",
	"claude-3-haiku":  "claude-haiku-4-5",
	"o4mini":          "gpt-5.4-mini",
	"mistral-small-4": "mistral-small-2603",
	"gpt-oss-120b":    "tinfoil/gpt-oss-120b",
	"gemma-4-31b":     "tinfoil/gemma4-31b",
	"llama":           "tinfoil/gpt-oss-120b",
	"mixtral":         "mistral-small-2603",
}

type DuckAI struct {
	endpoint       string
	client         *http.Client
	cookieJar      http.CookieJar
	captureHeaders func(context.Context) (duckAIHeaders, error)
}

type duckAIHeaders struct {
	VQDHash1       string
	FESignals      string
	FEVersion      string
	UserAgent      string
	AcceptLanguage string
	JourneyID      string
	BrowserHeaders map[string]string
	BrowserCookies []*http.Cookie
}

type duckAIHeaderCaptureResult struct {
	headers duckAIHeaders
	err     error
}

type duckAIChatPayload struct {
	Model                string               `json:"model"`
	Messages             []Message            `json:"messages"`
	CanUseTools          bool                 `json:"canUseTools"`
	CanUseApproxLocation *bool                `json:"canUseApproxLocation"`
	CanShowGreeting      bool                 `json:"canShowGreeting"`
	CanDelegateImage     *bool                `json:"canDelegateImageGeneration"`
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
	jar, _ := cookiejar.New(nil)
	return &DuckAI{
		endpoint:  duckAIChatURL,
		client:    &http.Client{Timeout: duckAIHTTPTimeout, Jar: jar},
		cookieJar: jar,
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
	if headers.VQDHash1 == "" {
		return fmt.Errorf("duckai: browser returned no X-Vqd-Hash-1 proof")
	}
	reasoningEffort := "none"
	if resolvedModel == "tinfoil/gpt-oss-120b" || resolvedModel == "tinfoil/gemma4-31b" {
		reasoningEffort = "low"
	}
	durableStream, err := newDuckAIDurableStream()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(duckAIChatPayload{
		Model:                resolvedModel,
		Messages:             messages,
		CanUseTools:          false,
		CanUseApproxLocation: nil,
		CanShowGreeting:      true,
		ReasoningEffort:      reasoningEffort,
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
	if headers.AcceptLanguage != "" {
		req.Header.Set("Accept-Language", headers.AcceptLanguage)
	} else {
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", duckAIOrigin)
	req.Header.Set("Referer", duckAIReferer)
	if headers.UserAgent != "" {
		req.Header.Set("User-Agent", headers.UserAgent)
	}
	for name, value := range headers.BrowserHeaders {
		req.Header.Set(name, value)
	}
	req.Header.Set("X-Vqd-Hash-1", headers.VQDHash1)
	if headers.FESignals != "" {
		req.Header.Set("x-fe-signals", headers.FESignals)
	}
	if headers.FEVersion != "" {
		req.Header.Set("x-fe-version", headers.FEVersion)
	}
	if headers.JourneyID != "" {
		req.Header.Set("x-ddg-journey-id", headers.JourneyID)
	}
	client := p.client
	if client == nil {
		client = &http.Client{Timeout: duckAIHTTPTimeout}
		if p.cookieJar != nil {
			client.Jar = p.cookieJar
		}
	}
	jar := p.cookieJar
	if jar == nil {
		jar = client.Jar
	}
	if len(headers.BrowserCookies) > 0 {
		if jar != nil {
			jar.SetCookies(req.URL, headers.BrowserCookies)
		} else {
			for _, cookie := range headers.BrowserCookies {
				req.AddCookie(cookie)
			}
		}
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
		if data == "[DONE]" || strings.HasPrefix(data, "[DONE]") {
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
	headers := duckAIHeaders{BrowserHeaders: make(map[string]string)}
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
		case "accept-language":
			headers.AcceptLanguage = text
		case "x-ddg-journey-id":
			headers.JourneyID = text
		case "sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform", "sec-fetch-dest", "sec-fetch-mode", "sec-fetch-site", "sec-gpc", "dnt", "priority", "x-duckai-continuation", "x-duckai-response-id", "x-duckai-tool-approval":
			headers.BrowserHeaders[strings.ToLower(name)] = text
		}
	}
	if len(headers.BrowserHeaders) == 0 {
		headers.BrowserHeaders = nil
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
	const pageReady = `(function() {
  const body = (document.body?.innerText || '').toLowerCase();
  if (body.includes('too many requests') || body.includes('trop de requêtes') || body.includes('trop de requetes') || body.includes('err_rate_limit')) return 'rate-limited';
  const textarea = document.querySelector('textarea[name="user-prompt"]');
  return textarea && textarea.getClientRects().length ? 'ready' : '';
})()`
	const submitState = `(function() {
  const body = (document.body?.innerText || '').toLowerCase();
  if (body.includes('too many requests') || body.includes('trop de requêtes') || body.includes('trop de requetes') || body.includes('err_rate_limit')) return 'rate-limited';
  const button = document.querySelector('button[type="submit"]');
  return button && !button.disabled ? 'ready' : '';
})()`
	var result, state, browserUserAgent string
	err = chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(`Object.defineProperty(navigator, 'webdriver', { get: () => undefined });`).Do(ctx)
			return err
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, _, _, userAgent, _, err := cdpbrowser.GetVersion().Do(ctx)
			if err != nil {
				return err
			}
			browserUserAgent = normalDuckAIBrowserUserAgent(userAgent)
			return emulation.SetUserAgentOverride(browserUserAgent).Do(ctx)
		}),
		network.Enable(),
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*duckchat/v1/chat*", RequestStage: fetch.RequestStageRequest}}),
		chromedp.Navigate(duckAIBootstrap),
		chromedp.Poll(pageReady, &state, chromedp.WithPollingInterval(100*time.Millisecond)),
		chromedp.ActionFunc(func(context.Context) error {
			if state == "rate-limited" {
				return fmt.Errorf("duckai: browser session is rate-limited by Duck.ai")
			}
			return nil
		}),
		chromedp.Evaluate(setPrompt, &result),
		chromedp.Poll(submitState, &state, chromedp.WithPollingInterval(100*time.Millisecond)),
		chromedp.ActionFunc(func(context.Context) error {
			if state == "rate-limited" {
				return fmt.Errorf("duckai: browser session is rate-limited by Duck.ai")
			}
			return nil
		}),
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
		if headers.VQDHash1 == "" {
			return duckAIHeaders{}, fmt.Errorf("duckai: browser request had no X-Vqd-Hash-1 proof")
		}
		if headers.UserAgent == "" {
			headers.UserAgent = browserUserAgent
		}
		var cookies []*network.Cookie
		if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			cookies, err = network.GetCookies().WithURLs([]string{duckAIChatURL}).Do(ctx)
			return err
		})); err != nil {
			return duckAIHeaders{}, fmt.Errorf("duckai: read browser cookies: %w", err)
		}
		for _, cookie := range cookies {
			if cookie == nil {
				continue
			}
			browserCookie := &http.Cookie{
				Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain,
				Path: cookie.Path, Secure: cookie.Secure, HttpOnly: cookie.HTTPOnly,
			}
			if !cookie.Session && cookie.Expires > 0 {
				browserCookie.Expires = time.Unix(int64(cookie.Expires), 0)
			}
			headers.BrowserCookies = append(headers.BrowserCookies, browserCookie)
		}
		return headers, nil
	case <-ctx.Done():
		return duckAIHeaders{}, fmt.Errorf("duckai: timed out waiting for chat headers: %w", ctx.Err())
	}
}

func findDuckAIBrowser() (string, error) {
	var names []string
	switch runtime.GOOS {
	case "darwin":
		names = []string{"google-chrome", "chromium", "Google Chrome", "Chromium"}
	case "windows":
		names = []string{"chrome.exe", "chromium.exe", "chrome", "chromium"}
	default:
		names = []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"}
	}
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	for _, path := range duckAIBrowserInstallPaths(runtime.GOOS) {
		if executable, err := exec.LookPath(path); err == nil {
			return executable, nil
		}
	}
	return "", fmt.Errorf("duckai: Chrome/Chromium is required for chat requests; install Chrome or Chromium and make sure it is available on PATH")
}

func duckAIBrowserInstallPaths(goos string) []string {
	var paths []string
	switch goos {
	case "darwin":
		paths = append(paths,
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		)
		if userHome, err := os.UserHomeDir(); err == nil {
			paths = append(paths,
				filepath.Join(userHome, "Applications", "Google Chrome.app", "Contents", "MacOS", "Google Chrome"),
				filepath.Join(userHome, "Applications", "Chromium.app", "Contents", "MacOS", "Chromium"),
			)
		}
	case "windows":
		for _, programFiles := range []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")} {
			if programFiles == "" {
				continue
			}
			paths = append(paths,
				filepath.Join(programFiles, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(programFiles, "Chromium", "Application", "chrome.exe"),
			)
		}
	}
	return paths
}

func normalDuckAIBrowserUserAgent(userAgent string) string {
	return strings.Replace(userAgent, "HeadlessChrome/", "Chrome/", 1)
}
