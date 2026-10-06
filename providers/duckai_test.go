package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

func TestDuckAIModelResolution(t *testing.T) {
	provider := NewDuckAI()
	want := map[string]string{
		"gpt-5.6-luna":         "gpt-5.6-luna",
		"gpt-5.4-mini":         "gpt-5.4-mini",
		"claude-haiku-4-5":     "claude-haiku-4-5",
		"gpt-5.4-nano":         "gpt-5.4-nano",
		"mistral-small-2603":   "mistral-small-2603",
		"tinfoil/gpt-oss-120b": "tinfoil/gpt-oss-120b",
		"tinfoil/gemma4-31b":   "tinfoil/gemma4-31b",
		"mistral-small-4":      "mistral-small-2603",
		"gpt-oss-120b":         "tinfoil/gpt-oss-120b",
		"gemma-4-31b":          "tinfoil/gemma4-31b",
		"gpt-4o-mini":          "gpt-5.6-luna",
		"claude-3-haiku":       "claude-haiku-4-5",
		"o4mini":               "gpt-5.4-mini",
	}
	for model, expected := range want {
		t.Run(model, func(t *testing.T) {
			if !provider.SupportsModel(model) {
				t.Fatalf("SupportsModel(%q) = false, want true", model)
			}
			got, err := resolveDuckAIModel(model, false)
			if err != nil {
				t.Fatalf("resolveDuckAIModel(%q) error = %v", model, err)
			}
			if got != expected {
				t.Fatalf("resolveDuckAIModel(%q) = %q, want %q", model, got, expected)
			}
		})
	}

	if provider.SupportsModel("unknown-model") {
		t.Fatal("SupportsModel(unknown-model) = true, want false")
	}
	if provider.SupportsModel("openai") {
		t.Fatal("SupportsModel(openai) = true, want false for automatic routing")
	}
	for _, model := range []string{
		"gpt-5.6-terra", "claude-sonnet-4-6", "claude-opus-4-8", "gpt-5.6-sol",
		"llama", "mixtral",
	} {
		if provider.SupportsModel(model) {
			t.Errorf("SupportsModel(%q) = true, want false for removed model", model)
		}
		if _, err := resolveDuckAIModel(model, false); err == nil {
			t.Errorf("resolveDuckAIModel(%q) error = nil, want unsupported model error", model)
		}
	}
	wantCatalog := []string{"gpt-5.6-luna", "gpt-5.4-nano", "gpt-5.4-mini", "claude-haiku-4-5", "mistral-small-2603", "tinfoil/gpt-oss-120b", "tinfoil/gemma4-31b"}
	if got := provider.GetInfo().SupportedModels; strings.Join(got, ",") != strings.Join(wantCatalog, ",") {
		t.Fatalf("Duck.ai supported catalog = %v, want %v", got, wantCatalog)
	}
	if got, err := resolveDuckAIModel("openai", true); err != nil || got != "gpt-5.6-luna" {
		t.Fatalf("explicit resolveDuckAIModel(openai) = %q, %v; want gpt-5.6-luna", got, err)
	}
	if _, err := resolveDuckAIModel("unknown-model", false); err == nil {
		t.Fatal("resolveDuckAIModel(unknown-model) error = nil, want error")
	}
}

func TestDuckAIDurableStreamBuildsFreshJWK(t *testing.T) {
	first, err := newDuckAIDurableStream()
	if err != nil {
		t.Fatalf("newDuckAIDurableStream() error = %v", err)
	}
	second, err := newDuckAIDurableStream()
	if err != nil {
		t.Fatalf("second newDuckAIDurableStream() error = %v", err)
	}
	if first.MessageID == "" || first.ConversationID == "" {
		t.Fatalf("durable stream IDs must be nonempty: %#v", first)
	}
	if first.MessageID == second.MessageID || first.ConversationID == second.ConversationID {
		t.Fatalf("durable stream IDs must be fresh: first=%#v second=%#v", first, second)
	}

	if first.PublicKey.Kty != "RSA" || first.PublicKey.Alg != "RSA-OAEP-256" || first.PublicKey.E != "AQAB" {
		t.Fatalf("unexpected JWK fields: %#v", first.PublicKey)
	}
	if first.PublicKey.N == "" {
		t.Fatal("JWK modulus is empty")
	}
}

type duckAIRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn duckAIRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestDuckAIRequestAndSSE(t *testing.T) {
	var gotRequest *http.Request
	var gotPayload map[string]interface{}
	provider := &DuckAI{
		endpoint: duckAIChatURL,
		client: &http.Client{Transport: duckAIRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotRequest = req
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(body, &gotPayload); err != nil {
				return nil, err
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(
				"data: [PING]\n" +
					"data: [CHAT_TITLE:Friendly title]\n" +
					"data: {\"role\":\"assistant\",\"message\":\"hello\"}\n" +
					"data: {\"role\":\"source\",\"source\":{\"url\":\"https://example.org\"}}\n" +
					"data: [DONE]\n" +
					"data: {\"role\":\"assistant\",\"message\":\"late\"}\n",
			))}, nil
		})},
		captureHeaders: func(context.Context) (duckAIHeaders, error) {
			return duckAIHeaders{VQDHash1: "proof", FESignals: "signals", FEVersion: "version", UserAgent: "browser"}, nil
		},
	}

	var chunks []string
	err := provider.ChatCompletionStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, "gpt-4o-mini", func(chunk string) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("ChatCompletionStream() error = %v", err)
	}
	if got := strings.Join(chunks, ""); got != "hello" {
		t.Fatalf("assistant text = %q, want hello", got)
	}
	if gotRequest == nil {
		t.Fatal("transport did not receive a request")
	}
	if gotRequest.Method != http.MethodPost || gotRequest.URL.Path != "/duckchat/v1/chat" {
		t.Fatalf("request = %s %s, want POST /duckchat/v1/chat", gotRequest.Method, gotRequest.URL.Path)
	}
	for key, expected := range map[string]string{
		"Origin":           duckAIOrigin,
		"Referer":          duckAIReferer,
		"Accept-Language":  "en-US,en;q=0.9",
		"X-Vqd-Hash-1":     "proof",
		"X-Fe-Signals":     "signals",
		"X-Fe-Version":     "version",
		"User-Agent":       "browser",
		"X-Ddg-Journey-Id": "",
	} {
		if key == "X-Ddg-Journey-Id" {
			if gotRequest.Header.Get(key) == "" {
				t.Fatalf("header %s is empty", key)
			}
			continue
		}
		if got := gotRequest.Header.Get(key); got != expected {
			t.Errorf("header %s = %q, want %q", key, got, expected)
		}
	}
	for _, name := range []string{"5", "dcm", "dcs"} {
		if got := gotRequest.Header.Get("Cookie"); !strings.Contains(got, name+"=") {
			t.Errorf("Cookie header %q is missing required Duck.ai cookie %q", got, name)
		}
	}
	if gotPayload["model"] != "gpt-5.6-luna" || gotPayload["canUseTools"] != false || gotPayload["canUseApproxLocation"] != true || gotPayload["reasoningEffort"] != "none" {
		t.Fatalf("unexpected Duck.ai payload fields: %#v", gotPayload)
	}
	if gotPayload["durableStream"] == nil || gotPayload["messages"] == nil {
		t.Fatalf("payload is missing durableStream or messages: %#v", gotPayload)
	}
}

func TestDuckAITinfoilModelsUseSupportedIDsAndLowReasoning(t *testing.T) {
	for _, model := range []string{"tinfoil/gpt-oss-120b", "tinfoil/gemma4-31b"} {
		t.Run(model, func(t *testing.T) {
			var payload map[string]interface{}
			provider := duckAITestProvider(func(req *http.Request) (*http.Response, error) {
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: [DONE]\n"))}, nil
			})
			if err := provider.ChatCompletionStream(context.Background(), []Message{{Role: "user", Content: "hello"}}, model, func(string) {}); err != nil {
				t.Fatalf("ChatCompletionStream(%q) error = %v", model, err)
			}
			if payload["model"] != model {
				t.Errorf("payload model = %v, want %q", payload["model"], model)
			}
			if payload["reasoningEffort"] != "low" {
				t.Errorf("payload reasoningEffort = %v, want low", payload["reasoningEffort"])
			}
		})
	}
}

func TestDuckAINonStreamingAggregatesAssistantText(t *testing.T) {
	provider := duckAITestProvider(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(
			"data: {\"role\":\"assistant\",\"message\":\"one\"}\n" +
				"data: {\"role\":\"assistant\",\"message\":\" two\"}\n" +
				"data: [DONE]\n",
		))}, nil
	})
	response, err := provider.ChatCompletion(context.Background(), []Message{{Role: "user", Content: "hi"}}, "openai")
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	if response.Model != duckAIDefault || response.Choices[0].Message.Content != "one two" {
		t.Fatalf("ChatCompletion() response = model %q content %q", response.Model, response.Choices[0].Message.Content)
	}
}

func TestDuckAIErrors(t *testing.T) {
	t.Run("browser bootstrap error", func(t *testing.T) {
		provider := duckAITestProvider(func(*http.Request) (*http.Response, error) {
			t.Fatal("HTTP transport called after browser bootstrap failed")
			return nil, nil
		})
		provider.captureHeaders = func(context.Context) (duckAIHeaders, error) {
			return duckAIHeaders{}, errors.New("Chrome/Chromium is required")
		}
		if err := provider.ChatCompletionStream(context.Background(), nil, "gpt-5.6-luna", func(string) {}); err == nil || !strings.Contains(err.Error(), "Chrome/Chromium") {
			t.Fatalf("ChatCompletionStream() error = %v, want browser bootstrap error", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		cause := errors.New("offline")
		provider := duckAITestProvider(func(*http.Request) (*http.Response, error) {
			return nil, cause
		})
		err := provider.ChatCompletionStream(context.Background(), nil, "gpt-5.6-luna", func(string) {})
		if err == nil {
			t.Fatal("ChatCompletionStream() error = nil, want transport error")
		}
		var pf *app.ProviderFailure
		if !errors.As(err, &pf) || pf.Category != app.FailureTransient {
			t.Fatalf("ChatCompletionStream() error = %v, want transient ProviderFailure", err)
		}
		if !errors.Is(err, cause) {
			t.Fatalf("ChatCompletionStream() error = %v did not preserve the cause", err)
		}
	})

	t.Run("upstream status", func(t *testing.T) {
		provider := duckAITestProvider(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader("rate limited")), Header: make(http.Header)}, nil
		})
		if err := provider.ChatCompletionStream(context.Background(), nil, "gpt-5.6-luna", func(string) {}); err == nil || !strings.Contains(err.Error(), "429") {
			t.Fatalf("ChatCompletionStream() error = %v, want HTTP 429", err)
		}
	})

	t.Run("malformed event", func(t *testing.T) {
		provider := duckAITestProvider(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: {broken json}\n")), Header: make(http.Header)}, nil
		})
		if err := provider.ChatCompletionStream(context.Background(), nil, "gpt-5.6-luna", func(string) {}); err == nil {
			t.Fatal("ChatCompletionStream() error = nil, want malformed SSE error")
		} else if !strings.Contains(err.Error(), "{broken json}") {
			t.Fatalf("malformed SSE error %q does not identify the bad event", err)
		}
	})

	t.Run("scanner error", func(t *testing.T) {
		provider := duckAITestProvider(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(&duckAIFailingReader{}), Header: make(http.Header)}, nil
		})
		if err := provider.ChatCompletionStream(context.Background(), nil, "gpt-5.6-luna", func(string) {}); err == nil || !strings.Contains(err.Error(), "read failed") {
			t.Fatalf("ChatCompletionStream() error = %v, want scanner error", err)
		}
	})
}

func duckAITestProvider(roundTrip duckAIRoundTripFunc) *DuckAI {
	return &DuckAI{
		endpoint: duckAIChatURL,
		client:   &http.Client{Transport: roundTrip},
		captureHeaders: func(context.Context) (duckAIHeaders, error) {
			return duckAIHeaders{VQDHash1: "proof", FESignals: "signals", FEVersion: "version", UserAgent: "browser"}, nil
		},
	}
}

type duckAIFailingReader struct{ sent bool }

func (r *duckAIFailingReader) Read(buffer []byte) (int, error) {
	if r.sent {
		return 0, fmt.Errorf("read failed")
	}
	r.sent = true
	return bytes.NewBufferString("data: {\"role\":\"assistant\",\"message\":\"partial\"}\n").Read(buffer)
}

func TestDuckAIPausedRequestHandlerAbortsCalibration(t *testing.T) {
	var captured duckAIHeaders
	aborted, continued := false, false
	err := handleDuckAIPausedRequest(true, map[string]interface{}{
		"X-Vqd-Hash-1": "proof", "x-fe-signals": "signals", "x-fe-version": "version", "User-Agent": "browser",
	}, func(headers duckAIHeaders) { captured = headers }, func() error { aborted = true; return nil }, func() error { continued = true; return nil })
	if err != nil {
		t.Fatalf("handleDuckAIPausedRequest() error = %v", err)
	}
	if captured.VQDHash1 != "proof" || captured.FESignals != "signals" || captured.FEVersion != "version" || captured.UserAgent != "browser" {
		t.Fatalf("captured headers = %+v", captured)
	}
	if !aborted || continued {
		t.Fatalf("calibration request aborted=%t continued=%t, want aborted only", aborted, continued)
	}

	aborted, continued = false, false
	if err := handleDuckAIPausedRequest(false, nil, func(duckAIHeaders) {}, func() error { aborted = true; return nil }, func() error { continued = true; return nil }); err != nil {
		t.Fatalf("handle unrelated request error = %v", err)
	}
	if aborted || !continued {
		t.Fatalf("unrelated request aborted=%t continued=%t, want continue only", aborted, continued)
	}

	var capturedAfterAbortFailure bool
	abortErr := errors.New("abort failed")
	err = handleDuckAIPausedRequest(true, map[string]interface{}{"X-Vqd-Hash-1": "proof"}, func(duckAIHeaders) {
		capturedAfterAbortFailure = true
	}, func() error { return abortErr }, func() error { return nil })
	if !errors.Is(err, abortErr) {
		t.Fatalf("abort error = %v, want %v", err, abortErr)
	}
	if capturedAfterAbortFailure {
		t.Fatal("headers were published before the calibration request was successfully aborted")
	}
}
