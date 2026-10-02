package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/internal/brand"
	"github.com/benoitpetit/voie/utils"
)

type chatRequest struct {
	Model          string                `json:"model"`
	Provider       string                `json:"provider,omitempty"`
	Strategy       app.Strategy          `json:"strategy,omitempty"`
	Task           string                `json:"task,omitempty"`
	Models         []string              `json:"models,omitempty"`
	Fallback       *app.FallbackOverride `json:"fallback,omitempty"`
	ConversationID string                `json:"conversation_id,omitempty"`
	Messages       []app.Message         `json:"messages"`
	Stream         bool                  `json:"stream,omitempty"`
}

func NewHandler(service *app.Service, cfg *config.Config) http.Handler {
	mux := http.NewServeMux()
	handler := &apiHandler{service: service}
	mux.HandleFunc("/", handler.root)
	mux.HandleFunc("/health", handler.health)
	mux.HandleFunc("/v1/chat/completions", handler.chatCompletions)
	mux.HandleFunc("/v1/models", handler.models)
	mux.HandleFunc("/v1/providers", handler.providers)
	mux.HandleFunc("/v1/conversations", handler.conversations)
	mux.HandleFunc("/v1/conversations/", handler.conversation)
	var token string
	if cfg != nil {
		token = cfg.APIToken
	}
	return requestLogging(cors(bearerAuth(token, mux)))
}

type apiHandler struct{ service *app.Service }

func (h *apiHandler) chatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.Warn("chat failed category=method_not_allowed")
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed. Use POST.")
		return
	}
	var request chatRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		utils.Warn("chat failed category=invalid_json")
		writeError(w, http.StatusBadRequest, "Bad request: invalid JSON — "+err.Error())
		return
	}
	appRequest := app.CompletionRequest{Model: request.Model, Provider: request.Provider, Strategy: request.Strategy, Task: request.Task, Models: request.Models, Fallback: request.Fallback, ConversationID: request.ConversationID, Messages: request.Messages}
	if !request.Stream {
		response, err := h.service.Complete(r.Context(), appRequest)
		if err != nil {
			logChatOutcome(appRequest, nil, err)
			writeAppError(w, err)
			return
		}
		logChatOutcome(appRequest, response, nil)
		writeJSON(w, http.StatusOK, response)
		return
	}
	h.streamCompletion(w, r, appRequest)
}

func (h *apiHandler) streamCompletion(w http.ResponseWriter, r *http.Request, request app.CompletionRequest) {
	started := false
	var finalRouting *app.RoutingInfo
	completionID := "chatcmpl-" + randomID()
	resolvedModel := request.Model
	start := func(routing *app.RoutingInfo) {
		if started {
			return
		}
		started = true
		if resolvedModel == "" && routing != nil && len(routing.Models) > 0 {
			resolvedModel = routing.Models[0].Model
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		writeSSE(w, app.StreamChunk{
			ID: completionID, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: resolvedModel,
			ConversationID: request.ConversationID, Routing: routing,
			Choices: []app.Choice{{Index: 0, Delta: &app.Delta{Role: "assistant"}}},
		})
	}
	err := h.service.CompleteStreamWithInfo(r.Context(), request, func(chunk string, routing *app.RoutingInfo) {
		if routing != nil {
			finalRouting = routing
		}
		start(routing)
		writeSSE(w, app.StreamChunk{
			ID: completionID, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: resolvedModel,
			ConversationID: request.ConversationID, Routing: routing,
			Choices: []app.Choice{{Index: 0, Delta: &app.Delta{Content: chunk}}},
		})
	})
	if err != nil && !started {
		logChatOutcome(request, nil, err, finalRouting)
		writeAppError(w, err)
		return
	}
	logChatOutcome(request, nil, err, finalRouting)
	start(nil)
	if err != nil {
		writeSSE(w, map[string]interface{}{"error": map[string]string{"message": err.Error(), "type": "provider_error"}})
	} else {
		writeSSE(w, app.StreamChunk{
			ID: completionID, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: resolvedModel,
			Choices: []app.Choice{{Index: 0, Delta: &app.Delta{}, FinishReason: "stop"}},
		})
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

func logChatOutcome(request app.CompletionRequest, response *app.ChatCompletionResponse, err error, streamRouting ...*app.RoutingInfo) {
	strategy := request.Strategy
	if strategy == "" {
		strategy = app.StrategyClassic
	}
	if err != nil {
		utils.Warn("chat strategy=%s failed category=%s routing=%q", strategy, chatErrorCategory(err), chatRoutingSummary(response, streamRouting...))
		return
	}
	model, provider := request.Model, request.Provider
	if response != nil {
		if response.Model != "" {
			model = response.Model
		}
		if response.Provider != "" {
			provider = response.Provider
		}
	}
	utils.Info("chat strategy=%s model=%s provider=%s routing=%q status=succeeded", strategy, model, provider, chatRoutingSummary(response, streamRouting...))
}

func chatRoutingSummary(response *app.ChatCompletionResponse, streamRouting ...*app.RoutingInfo) string {
	var routing *app.RoutingInfo
	if response != nil {
		routing = response.Routing
	}
	if routing == nil && len(streamRouting) > 0 {
		routing = streamRouting[0]
	}
	if routing == nil {
		return ""
	}
	parts := make([]string, 0, len(routing.Models)+2)
	if routing.Task != "" {
		parts = append(parts, "task="+routing.Task)
	}
	for _, model := range routing.Models {
		parts = append(parts, fmt.Sprintf("%s/%s:%s", model.Model, model.Provider, model.Status))
	}
	if len(parts) == 0 {
		parts = append(parts, "strategy="+string(routing.Strategy))
	}
	return strings.Join(parts, ",")
}

func chatErrorCategory(err error) string {
	switch {
	case errors.Is(err, app.ErrInvalidInput):
		return "invalid_input"
	case errors.Is(err, app.ErrUnknownModel):
		return "unknown_model"
	case errors.Is(err, app.ErrUnknownProvider):
		return "unknown_provider"
	case errors.Is(err, app.ErrProviderDisabled):
		return "provider_disabled"
	case errors.Is(err, app.ErrRouting):
		return "routing"
	case errors.Is(err, app.ErrTimeout):
		return "timeout"
	case errors.Is(err, app.ErrCanceled):
		return "canceled"
	case errors.Is(err, app.ErrConversationConflict):
		return "conversation_conflict"
	case errors.Is(err, app.ErrConversationExpired):
		return "conversation_expired"
	case errors.Is(err, app.ErrConversationNotFound):
		return "conversation_not_found"
	case errors.Is(err, app.ErrConversationStore):
		return "conversation_store"
	case errors.Is(err, app.ErrUpstream):
		return "upstream"
	default:
		return "internal"
	}
}

func (h *apiHandler) models(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed. Use GET.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"object": "list", "data": h.service.ListModels()})
}

func (h *apiHandler) providers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed. Use GET.")
		return
	}
	list, err := h.service.ListProviders(r.Context())
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"object": "list", "data": list, "count": len(list), "timestamp": time.Now().Unix()})
}

func (h *apiHandler) health(w http.ResponseWriter, _ *http.Request) {
	providers := h.service.ListProviderMetadata()
	working := make([]string, 0, len(providers))
	disabled := make([]string, 0)
	modelCount := 0
	for _, provider := range providers {
		if provider.Working {
			working = append(working, provider.Name)
			modelCount += len(provider.SupportedModels)
		} else {
			disabled = append(disabled, provider.Name)
		}
	}
	sort.Strings(working)
	sort.Strings(disabled)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok", "timestamp": time.Now().Unix(), "version": brand.Version,
		"providers_total": len(providers), "providers_working": len(working),
		"working_providers": working, "total_models": modelCount,
	})
}

func (h *apiHandler) root(w http.ResponseWriter, _ *http.Request) {
	providers := h.service.ListProviderMetadata()
	workingCount, modelCount := 0, 0
	for _, provider := range providers {
		if provider.Working {
			workingCount++
			modelCount += len(provider.SupportedModels)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name": brand.Name, "version": brand.Version,
		"description":     "A local OpenAI-compatible API for discovering and calling supported models.",
		"endpoints":       map[string]string{"chat_completions": "POST /v1/chat/completions", "models": "GET  /v1/models", "providers": "GET  /v1/providers", "health": "GET  /health"},
		"providers_count": workingCount, "models_count": modelCount, "openai_compatible": true,
	})
}

func writeAppError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, app.ErrInvalidInput), errors.Is(err, app.ErrUnknownModel), errors.Is(err, app.ErrProviderModelMismatch):
		status = http.StatusBadRequest
	case errors.Is(err, app.ErrRouting), errors.Is(err, app.ErrEnsembleInsufficient):
		status = http.StatusBadRequest
	case errors.Is(err, app.ErrConversationNotFound):
		status = http.StatusNotFound
	case errors.Is(err, app.ErrConversationConflict):
		status = http.StatusConflict
	case errors.Is(err, app.ErrConversationExpired):
		status = http.StatusGone
	case errors.Is(err, app.ErrConversationStore):
		status = http.StatusInternalServerError
	case errors.Is(err, app.ErrUnknownProvider):
		status = http.StatusNotFound
	case errors.Is(err, app.ErrProviderDisabled):
		status = http.StatusServiceUnavailable
	case errors.Is(err, app.ErrTimeout):
		status = http.StatusGatewayTimeout
	case errors.Is(err, app.ErrCanceled):
		status = http.StatusRequestTimeout
	case errors.Is(err, app.ErrUpstream):
		status = http.StatusBadGateway
	}
	writeError(w, status, err.Error())
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]interface{}{"error": map[string]string{"message": message, "type": "error", "code": fmt.Sprint(status)}})
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeSSE(w http.ResponseWriter, value interface{}) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func randomID() string {
	return strings.ReplaceAll(fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Unix()), "-", "")
}
