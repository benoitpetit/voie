package httpapi

import (
	"bytes"
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
	sqlitestore "github.com/benoitpetit/voie/internal/storage/sqlite"
	"github.com/benoitpetit/voie/utils"
)

func TestRequestLoggingRedactsValuesAndPreservesFlush(t *testing.T) {
	var logs bytes.Buffer
	utils.SetOutput(&logs)
	defer utils.SetOutput(nil)
	utils.SetNoColor(true)
	defer utils.SetNoColor(true)
	underlying := &flushTestWriter{header: make(http.Header)}
	handler := requestLogging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("body-secret"))
		w.(http.Flusher).Flush()
	}))
	request := httptest.NewRequest(http.MethodPost, "/private?token=query-secret", strings.NewReader("prompt-secret"))
	request.Header.Set("Authorization", "Bearer header-secret")
	handler.ServeHTTP(underlying, request)
	if underlying.status != http.StatusAccepted || underlying.flushes != 1 || string(underlying.body) != "body-secret" {
		t.Fatalf("response status=%d flushes=%d body=%q", underlying.status, underlying.flushes, underlying.body)
	}
	for _, required := range []string{"POST", "/private", "202", "request_id="} {
		if !strings.Contains(logs.String(), required) {
			t.Fatalf("logs %q missing %q", logs.String(), required)
		}
	}
	for _, secret := range []string{"query-secret", "header-secret", "prompt-secret", "body-secret"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("logs leaked %q: %s", secret, logs.String())
		}
	}
}

func TestRequestLoggingIncludesAuthenticationFailuresAndFallbackStatus(t *testing.T) {
	var logs bytes.Buffer
	utils.SetOutput(&logs)
	defer utils.SetOutput(nil)
	utils.SetNoColor(true)
	service, _ := httpTestService(t)
	handler := NewHandler(service, &config.Config{APIToken: "expected-token"})
	request := httptest.NewRequest(http.MethodGet, "/health?secret=query", nil)
	request.Header.Set("Authorization", "Bearer rejected-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("authentication status=%d, want 401", response.Code)
	}
	quiet := requestLogging(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	quiet.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/quiet", nil))
	for _, expected := range []string{"GET /health", "401", "GET /quiet", "500"} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("logs %q missing %q", logs.String(), expected)
		}
	}
	if strings.Contains(logs.String(), "secret=query") || strings.Contains(logs.String(), "rejected-token") {
		t.Fatalf("logs include request secrets: %s", logs.String())
	}
}

type flushTestWriter struct {
	header  http.Header
	status  int
	flushes int
	body    []byte
}

func (w *flushTestWriter) Header() http.Header { return w.header }
func (w *flushTestWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *flushTestWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.body = append(w.body, body...)
	return len(body), nil
}
func (w *flushTestWriter) Flush() { w.flushes++ }

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

func TestHTTPConversationLifecycle(t *testing.T) {
	service, _ := httpTestService(t)
	store, err := sqlitestore.NewSQLiteStore(t.TempDir()+"/conversations.db", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// A fresh service uses the same fake provider with the concrete local store.
	registry := app.NewRegistry()
	provider := &httpTestProvider{}
	registry.Register("test", provider)
	service, err = app.NewService(registry, app.ServiceOptions{Conversations: store})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(service, &config.Config{})
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/v1/conversations", nil))
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var conversation app.Conversation
	if err = json.Unmarshal(created.Body.Bytes(), &conversation); err != nil {
		t.Fatal(err)
	}
	if conversation.ID == "" || conversation.CreatedAt.IsZero() || conversation.ExpiresAt.IsZero() {
		t.Fatalf("conversation=%+v", conversation)
	}
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/v1/conversations", nil))
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), "turns") {
		t.Fatalf("list=%d %s", list.Code, list.Body.String())
	}
	show := httptest.NewRecorder()
	handler.ServeHTTP(show, httptest.NewRequest(http.MethodGet, "/v1/conversations/"+conversation.ID, nil))
	if show.Code != http.StatusOK {
		t.Fatalf("show=%d %s", show.Code, show.Body.String())
	}
	deleted := httptest.NewRecorder()
	handler.ServeHTTP(deleted, httptest.NewRequest(http.MethodDelete, "/v1/conversations/"+conversation.ID, nil))
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete=%d", deleted.Code)
	}
}

func TestHTTPConversationCompletionReturnsConversationID(t *testing.T) {
	registry := app.NewRegistry()
	provider := &httpTestProvider{}
	registry.Register("test", provider)
	store, err := sqlitestore.NewSQLiteStore(t.TempDir()+"/conversations.db", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := app.NewService(registry, app.ServiceOptions{Conversations: store})
	if err != nil {
		t.Fatal(err)
	}
	c, err := service.CreateConversation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(service, &config.Config{})
	body := `{"model":"model","conversation_id":"` + c.ID + `","messages":[{"role":"user","content":"hi"}]}`
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"conversation_id":"`+c.ID+`"`) {
		t.Fatalf("completion=%d %s", r.Code, r.Body.String())
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
