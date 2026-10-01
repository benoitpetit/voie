package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/internal/brand"
)

func TestHTTPRoutesPreserveJSONAndSSEShapes(t *testing.T) {
	service, provider := httpTestService(t)
	handler := NewHandler(service, &config.Config{Host: "127.0.0.1", Port: "8080", Timeout: time.Minute})
	chatBody := `{"model":"model","messages":[{"role":"user","content":"hi"}]}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody)))
	if response.Code != http.StatusOK {
		t.Fatalf("chat status = %d, body %s", response.Code, response.Body.String())
	}
	var completion app.ChatCompletionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &completion); err != nil {
		t.Fatal(err)
	}
	if completion.Object != "chat.completion" || completion.Choices[0].Message.Content != "reply" {
		t.Fatalf("completion = %+v", completion)
	}

	streamBody := `{"model":"model","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	stream := httptest.NewRecorder()
	handler.ServeHTTP(stream, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(streamBody)))
	if stream.Code != http.StatusOK || !strings.Contains(stream.Body.String(), `"role":"assistant"`) || !strings.Contains(stream.Body.String(), `"content":"chunk"`) || !strings.Contains(stream.Body.String(), "data: [DONE]") {
		t.Fatalf("stream response = %d %s", stream.Code, stream.Body.String())
	}
	var streamID string
	for _, line := range strings.Split(stream.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: {") {
			continue
		}
		var event struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		if streamID == "" {
			streamID = event.ID
		} else if event.ID != streamID {
			t.Fatalf("stream event id = %q, want stable id %q", event.ID, streamID)
		}
	}
	if !provider.streamCalled {
		t.Fatal("provider stream method was not called")
	}
}

func TestHTTPDiscoveryAndTypedErrors(t *testing.T) {
	service, _ := httpTestService(t)
	handler := NewHandler(service, &config.Config{Host: "127.0.0.1", Port: "8080", Timeout: time.Minute})
	models := httptest.NewRecorder()
	handler.ServeHTTP(models, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if models.Code != http.StatusOK || !strings.Contains(models.Body.String(), `"object":"list"`) || !strings.Contains(models.Body.String(), `"id":"model"`) {
		t.Fatalf("models = %d %s", models.Code, models.Body.String())
	}
	providers := httptest.NewRecorder()
	handler.ServeHTTP(providers, httptest.NewRequest(http.MethodGet, "/v1/providers", nil))
	if providers.Code != http.StatusOK || !strings.Contains(providers.Body.String(), `"alive":true`) || !strings.Contains(providers.Body.String(), `"count":1`) {
		t.Fatalf("providers = %d %s", providers.Code, providers.Body.String())
	}

	cases := []struct {
		name, body string
		status     int
	}{
		{"unknown model", `{"model":"typo","messages":[{"role":"user","content":"hi"}]}`, 400},
		{"unknown provider", `{"provider":"missing","model":"openai","messages":[{"role":"user","content":"hi"}]}`, 404},
		{"mismatch", `{"provider":"test","model":"other","messages":[{"role":"user","content":"hi"}]}`, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRecorder()
			handler.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tc.body)))
			if r.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", r.Code, tc.status, r.Body.String())
			}
		})
	}
}

func TestHTTPRouteMethodsAndHealth(t *testing.T) {
	service, _ := httpTestService(t)
	handler := NewHandler(service, &config.Config{Host: "127.0.0.1", Port: "8080", Timeout: time.Minute})
	method := httptest.NewRecorder()
	handler.ServeHTTP(method, httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil))
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method status = %d", method.Code)
	}
	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusOK || !strings.Contains(health.Body.String(), `"providers_total":1`) {
		t.Fatalf("health = %d %s", health.Code, health.Body.String())
	}
}

func TestHTTPMetadataUsesBuildBrandAndVersion(t *testing.T) {
	service, _ := httpTestService(t)
	handler := NewHandler(service, &config.Config{Host: "127.0.0.1", Port: "8080", Timeout: time.Minute})

	root := httptest.NewRecorder()
	handler.ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/", nil))
	var rootMetadata struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(root.Body.Bytes(), &rootMetadata); err != nil {
		t.Fatal(err)
	}
	if rootMetadata.Name != brand.Name || rootMetadata.Version != brand.Version {
		t.Fatalf("root metadata = %+v, want name=%q version=%q", rootMetadata, brand.Name, brand.Version)
	}

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	var healthMetadata struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(health.Body.Bytes(), &healthMetadata); err != nil {
		t.Fatal(err)
	}
	if healthMetadata.Version != brand.Version {
		t.Fatalf("health version = %q, want %q", healthMetadata.Version, brand.Version)
	}
}

func TestHTTPMapsTimeoutAndUpstreamErrors(t *testing.T) {
	service, provider := httpTestService(t)
	handler := NewHandler(service, &config.Config{Host: "127.0.0.1", Port: "8080", Timeout: time.Minute})
	body := `{"model":"model","messages":[{"role":"user","content":"hi"}]}`
	provider.chatErr = context.DeadlineExceeded
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	if r.Code != http.StatusGatewayTimeout {
		t.Fatalf("timeout status = %d", r.Code)
	}
	provider.chatErr = errors.New("upstream down")
	r = httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	if r.Code != http.StatusBadGateway {
		t.Fatalf("upstream status = %d", r.Code)
	}
}

func httpTestService(t *testing.T) (*app.Service, *httpTestProvider) {
	t.Helper()
	registry := app.NewRegistry()
	provider := &httpTestProvider{}
	registry.Register("test", provider)
	service, err := app.NewService(registry, app.ServiceOptions{Timeout: time.Minute, HealthProbe: func(context.Context, string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	return service, provider
}

type httpTestProvider struct {
	chatErr      error
	streamCalled bool
}

func (*httpTestProvider) GetInfo() app.ProviderInfo {
	return app.ProviderInfo{Name: "test", Label: "Test", URL: "https://example.invalid", Working: true, DefaultModel: "model", SupportedModels: []string{"model"}, SupportsStream: true}
}
func (*httpTestProvider) SupportsModel(model string) bool { return model == "model" }
func (p *httpTestProvider) ChatCompletion(ctx context.Context, _ []app.Message, model string) (*app.ChatCompletionResponse, error) {
	if p.chatErr != nil {
		return nil, p.chatErr
	}
	return &app.ChatCompletionResponse{Object: "chat.completion", Model: model, Choices: []app.Choice{{Message: app.Message{Role: "assistant", Content: "reply"}}}}, nil
}
func (p *httpTestProvider) ChatCompletionStream(_ context.Context, _ []app.Message, _ string, callback func(string)) error {
	p.streamCalled = true
	callback("chunk")
	return nil
}
