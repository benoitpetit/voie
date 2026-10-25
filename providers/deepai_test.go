package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDeepAIResolvesCurrentAndLegacyModels(t *testing.T) {
	provider := &DeepAI{}
	for _, model := range deepaiCanonicalModels {
		if !provider.SupportsModel(model) {
			t.Errorf("SupportsModel(%q) = false", model)
		}
	}
	for legacy, canonical := range deepaiModelAliases {
		resolved, err := provider.resolveModel(legacy)
		if err != nil || resolved != canonical {
			t.Errorf("resolveModel(%q) = %q, %v; want %q", legacy, resolved, err, canonical)
		}
	}
	for _, unsupported := range []string{"deepseek-v3.999", "gemini-2.5-pro", "made-up-model"} {
		if provider.SupportsModel(unsupported) {
			t.Errorf("SupportsModel(%q) = true for unsupported model", unsupported)
		}
		if _, err := provider.resolveModel(unsupported); err == nil {
			t.Errorf("resolveModel(%q) returned no error", unsupported)
		}
	}
	if got, err := provider.resolveModel(""); err != nil || got != "standard" {
		t.Errorf("resolveModel(empty) = %q, %v; want standard", got, err)
	}
}

func TestDeepAIRequestUsesCurrentFormAndCanonicalModel(t *testing.T) {
	var requestErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			requestErr = fmt.Errorf("method = %q, want POST", r.Method)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			requestErr = err
			return
		}
		for field, want := range map[string]string{
			"chat_style": "chat", "language": "en", "model": "claude-haiku-5-5",
			"tool_activity_support": "1", "thinking_image_tool_support": "1",
		} {
			if got := r.FormValue(field); got != want {
				requestErr = fmt.Errorf("field %s = %q, want %q", field, got, want)
				return
			}
		}
		if r.Header.Get("api-key") == "" || r.Header.Get("User-Agent") != deepaiUA {
			requestErr = fmt.Errorf("expected anonymous API key and browser user agent")
			return
		}
		_, _ = io.WriteString(w, "PONG")
	}))
	defer server.Close()

	provider := &DeepAI{endpoint: server.URL, client: &http.Client{Timeout: time.Second}}
	var response strings.Builder
	err := provider.ChatCompletionStream(context.Background(), []Message{{Role: "user", Content: "ping"}}, "gemini-2.5-flash-lite", func(chunk string) {
		response.WriteString(chunk)
	})
	if err != nil {
		t.Fatal(err)
	}
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	if response.String() != "PONG" {
		t.Fatalf("response = %q, want PONG", response.String())
	}
}

func TestDeepAIInfoListsOnlyCanonicalModels(t *testing.T) {
	info := (&DeepAI{}).GetInfo()
	if len(info.SupportedModels) != len(deepaiCanonicalModels) {
		t.Fatalf("supported models = %v, want %v", info.SupportedModels, deepaiCanonicalModels)
	}
	for _, model := range info.SupportedModels {
		if strings.Contains(model, "gemini-2.5") || model == "online" || model == "deepseek-v3.2" {
			t.Errorf("legacy model %q still advertised", model)
		}
	}
}

func TestDeepAILiveModelProbe(t *testing.T) {
	if testing.Short() || os.Getenv("DEEPAI_LIVE_TEST") != "1" {
		t.Skip("set DEEPAI_LIVE_TEST=1 to send a live, account-free DeepAI chat request")
	}
	model := strings.TrimSpace(os.Getenv("DEEPAI_LIVE_MODEL"))
	if model == "" {
		model = "standard"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	response, err := (&DeepAI{}).ChatCompletion(ctx, []Message{{Role: "user", Content: "Reply with only the word PONG."}}, model)
	if err != nil {
		t.Fatal(err)
	}
	content := strings.TrimSpace(response.Choices[0].Message.Content)
	if content == "" {
		t.Fatal("live DeepAI response was empty")
	}
	t.Logf("model=%s resolved=%s response=%q", model, response.Model, content)
}
