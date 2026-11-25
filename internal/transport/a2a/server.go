package a2a

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/google/uuid"
)

const (
	codeInvalidParams  = -32602
	codeMethodNotFound = -32601
	codeInternal       = -32603
	codeTaskNotFound   = -32001
	codeUnavailable    = -32002
)

// Handler serves A2A JSON-RPC requests using the shared service.
type Handler struct {
	service *app.Service
	store   *taskStore
	card    AgentCard
}

// NewHandler builds the A2A handler for an existing service.
func NewHandler(service *app.Service) *Handler {
	return &Handler{service: service, store: newTaskStore(), card: AgentCardFor("")}
}

// SetPublicURL sets the base URL advertised in the agent card.
func (h *Handler) SetPublicURL(url string) { h.card = AgentCardFor(url) }

// Card returns the advertised agent card.
func (h *Handler) Card() AgentCard { return h.card }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request RPCRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeRPC(w, r, RPCResponse{JSONRPC: "2.0", Error: &RPCError{Code: codeInvalidParams, Message: "invalid JSON-RPC request"}})
		return
	}
	id := request.ID
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	result, rpcErr, stream := h.dispatch(w, r, request)
	if rpcErr != nil {
		writeRPCError(w, id, rpcErr)
		return
	}
	if stream {
		return
	}
	writeRPC(w, r, RPCResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (h *Handler) dispatch(w http.ResponseWriter, r *http.Request, request RPCRequest) (any, *RPCError, bool) {
	var params SendParams
	if len(request.Params) > 0 {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, &RPCError{Code: codeInvalidParams, Message: "invalid params"}, false
		}
	}
	owner := ownerFor(r)
	switch request.Method {
	case "GetExtendedAgentCard", "agent/getAuthenticatedExtendedCard":
		return h.card, nil, false
	case "SendMessage", "message/send":
		task, rpcErr := h.send(r.Context(), params.Message, owner, nil)
		if rpcErr != nil {
			return nil, rpcErr, false
		}
		return task, nil, false
	case "SendStreamingMessage", "message/stream":
		return nil, h.stream(w, r, params.Message, owner), true
	case "GetTask", "tasks/get":
		var query struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(request.Params, &query)
		task, ok := h.store.get(query.ID, owner)
		if !ok {
			return nil, &RPCError{Code: codeTaskNotFound, Message: "task not found"}, false
		}
		return task, nil, false
	case "ListTasks", "tasks/list":
		var query struct {
			ContextID string `json:"contextId"`
			State     string `json:"status"`
		}
		_ = json.Unmarshal(request.Params, &query)
		tasks := h.store.list(owner, query.ContextID, query.State)
		return map[string]any{"tasks": tasks, "nextPageToken": "", "pageSize": len(tasks), "totalSize": len(tasks)}, nil, false
	case "CancelTask", "tasks/cancel":
		var query struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(request.Params, &query)
		task, ok := h.store.cancel(query.ID, owner)
		if !ok {
			return nil, &RPCError{Code: codeTaskNotFound, Message: "task not found"}, false
		}
		return task, nil, false
	case "SubscribeToTask", "tasks/resubscribe":
		var query struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(request.Params, &query)
		task, ok := h.store.get(query.ID, owner)
		if !ok {
			return nil, &RPCError{Code: codeTaskNotFound, Message: "task not found"}, false
		}
		return task, nil, false
	default:
		return nil, &RPCError{Code: codeMethodNotFound, Message: "method not found: " + request.Method}, false
	}
}

func ownerFor(r *http.Request) string {
	sum := sha256.Sum256([]byte(r.Header.Get("Authorization") + "|" + r.Header.Get("X-Voie-Conversation-Scope")))
	return hex.EncodeToString(sum[:])
}

func (h *Handler) send(ctx context.Context, message Message, owner string, onChunk func(string)) (Task, *RPCError) {
	request, err := h.completionRequest(message)
	if err != nil {
		return Task{}, err
	}
	id := message.MessageID
	if id == "" {
		id = uuid.NewString()
	}
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	h.store.create(id, owner, callCtx, cancel)
	h.store.appendHistory(id, message)
	h.store.setStatus(id, "TASK_STATE_WORKING")
	var answer string
	if onChunk == nil {
		response, callErr := h.service.Complete(callCtx, request)
		if callErr != nil {
			h.store.setStatus(id, "TASK_STATE_FAILED")
			return Task{}, failureFor(callErr)
		}
		if len(response.Choices) > 0 {
			answer = response.Choices[0].Message.Content
			request.ConversationID = response.ConversationID
		}
	} else {
		callErr := h.service.CompleteStreamWithInfo(callCtx, request, func(chunk string, _ *app.RoutingInfo) {
			answer += chunk
			onChunk(chunk)
		})
		if callErr != nil {
			h.store.setStatus(id, "TASK_STATE_FAILED")
			return Task{}, failureFor(callErr)
		}
	}
	if answer == "" {
		h.store.setStatus(id, "TASK_STATE_FAILED")
		return Task{}, &RPCError{Code: codeUnavailable, Message: "upstream returned no text"}
	}
	contextID := message.ContextID
	if contextID == "" {
		contextID = id
	}
	task := Task{
		ID:        id,
		ContextID: contextID,
		Status:    TaskStatus{State: "TASK_STATE_COMPLETED", Timestamp: timestamp()},
		Artifacts: []Artifact{{ArtifactID: uuid.NewString(), Parts: []Part{{Text: answer, MediaType: "text/plain"}}}},
	}
	h.store.setTask(id, task)
	return task, nil
}

func (h *Handler) stream(w http.ResponseWriter, r *http.Request, message Message, owner string) *RPCError {
	request, rpcErr := h.completionRequest(message)
	if rpcErr != nil {
		return rpcErr
	}
	taskID := message.MessageID
	if taskID == "" {
		taskID = uuid.NewString()
	}
	contextID := message.ContextID
	if contextID == "" {
		contextID = taskID
	}
	callCtx, cancel := context.WithCancel(r.Context())
	defer cancel()
	h.store.create(taskID, owner, callCtx, cancel)
	h.store.appendHistory(taskID, message)
	h.store.setStatus(taskID, "TASK_STATE_WORKING")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	writeSSE(w, flusher, statusEvent(taskID, contextID, "TASK_STATE_WORKING"))

	var answer strings.Builder
	callErr := h.service.CompleteStreamWithInfo(callCtx, request, func(chunk string, _ *app.RoutingInfo) {
		answer.WriteString(chunk)
		event := ArtifactUpdateEvent{}
		event.ArtifactUpdate.TaskID = taskID
		event.ArtifactUpdate.ContextID = contextID
		event.ArtifactUpdate.Artifact = Artifact{ArtifactID: taskID + "-artifact", Parts: []Part{{Text: chunk, MediaType: "text/plain"}}}
		writeSSE(w, flusher, event)
	})
	if callErr != nil {
		h.store.setStatus(taskID, "TASK_STATE_FAILED")
		writeSSE(w, flusher, statusEvent(taskID, contextID, "TASK_STATE_FAILED"))
		return nil
	}
	task := Task{
		ID:        taskID,
		ContextID: contextID,
		Status:    TaskStatus{State: "TASK_STATE_COMPLETED", Timestamp: timestamp()},
		Artifacts: []Artifact{{ArtifactID: taskID + "-artifact", Parts: []Part{{Text: answer.String(), MediaType: "text/plain"}}}},
	}
	h.store.setTask(taskID, task)
	writeSSE(w, flusher, statusEvent(taskID, contextID, "TASK_STATE_COMPLETED"))
	return nil
}

func statusEvent(taskID, contextID, state string) StatusUpdateEvent {
	event := StatusUpdateEvent{}
	event.StatusUpdate.TaskID = taskID
	event.StatusUpdate.ContextID = contextID
	event.StatusUpdate.Status = TaskStatus{State: state, Timestamp: timestamp()}
	return event
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return
	}
	_, _ = w.Write([]byte("data: " + string(encoded) + "\n\n"))
	if flusher != nil {
		flusher.Flush()
	}
}

func (h *Handler) completionRequest(message Message) (app.CompletionRequest, *RPCError) {
	meta := message.Metadata
	strategy := metaString(meta, "strategy")
	if skill := metaString(meta, "skill"); skill != "" {
		strategy = strategyForSkill(skill)
	}
	if strategy == "" {
		strategy = string(app.StrategyClassic)
	}
	model := metaString(meta, "model")
	if strings.EqualFold(strategy, string(app.StrategyClassic)) && model == "" {
		return app.CompletionRequest{}, &RPCError{Code: codeInvalidParams, Message: "classic requests require metadata.model"}
	}
	request := app.CompletionRequest{Strategy: app.Strategy(strategy), Model: model, Task: metaString(meta, "task"), ConversationID: message.ContextID}
	if raw, ok := meta["ensemble"]; ok {
		encoded, err := json.Marshal(raw)
		if err != nil {
			return app.CompletionRequest{}, &RPCError{Code: codeInvalidParams, Message: "invalid ensemble metadata"}
		}
		var options app.EnsembleRequestOptions
		if err := json.Unmarshal(encoded, &options); err != nil {
			return app.CompletionRequest{}, &RPCError{Code: codeInvalidParams, Message: "invalid ensemble metadata"}
		}
		request.Ensemble = &options
	}
	for _, part := range message.Parts {
		if part.Text == "" {
			continue
		}
		role := strings.ToLower(message.Role)
		if strings.HasPrefix(role, "role_") {
			role = strings.TrimPrefix(role, "role_")
		}
		if role != "user" && role != "assistant" {
			role = "user"
		}
		request.Messages = append(request.Messages, app.Message{Role: role, Content: part.Text})
	}
	if len(request.Messages) == 0 {
		return app.CompletionRequest{}, &RPCError{Code: codeInvalidParams, Message: "message has no text parts"}
	}
	return request, nil
}

func strategyForSkill(skill string) string {
	switch {
	case strings.Contains(skill, "ensemble.independent"):
		return string(app.StrategyEnsemble)
	case strings.Contains(skill, "ensemble.chain"):
		return string(app.StrategyEnsemble)
	case strings.Contains(skill, "ensemble.debate"):
		return string(app.StrategyEnsemble)
	case strings.Contains(skill, ".auto"):
		return string(app.StrategyAuto)
	default:
		return string(app.StrategyClassic)
	}
}

func metaString(meta map[string]any, key string) string {
	value, _ := meta[key].(string)
	return strings.TrimSpace(value)
}

func failureFor(err error) *RPCError {
	if errors.Is(err, app.ErrInvalidInput) || errors.Is(err, app.ErrUnknownModel) {
		return &RPCError{Code: codeInvalidParams, Message: err.Error()}
	}
	return &RPCError{Code: codeUnavailable, Message: err.Error()}
}

func writeRPC(w http.ResponseWriter, r *http.Request, response RPCResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
	_ = r
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, failure *RPCError) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(RPCResponse{JSONRPC: "2.0", ID: id, Error: failure})
}
