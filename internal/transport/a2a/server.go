package a2a

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/google/uuid"
)

const (
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
	codeTaskNotFound   = -32001
	codeUnsupported    = -32002
	codeContentType    = -32003
	codeInvalidState   = -32004
	codeUnavailable    = -32005
	maxA2ARequestBytes = 2 << 20
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
	r.Body = http.MaxBytesReader(w, r.Body, maxA2ARequestBytes)
	var request RPCRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&request); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "request body is too large", http.StatusRequestEntityTooLarge)
			return
		}
		writeRPC(w, r, RPCResponse{JSONRPC: "2.0", Error: &RPCError{Code: codeInvalidParams, Message: "invalid JSON-RPC request"}})
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "request body is too large", http.StatusRequestEntityTooLarge)
			return
		}
		writeRPCError(w, request.ID, &RPCError{Code: codeInvalidRequest, Message: "request must contain one JSON-RPC value"})
		return
	}
	if request.JSONRPC != "2.0" {
		writeRPCError(w, request.ID, &RPCError{Code: codeInvalidRequest, Message: "jsonrpc must be 2.0"})
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
		task, rpcErr := h.send(r.Context(), params.Message, owner)
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
			PageSize  int    `json:"pageSize"`
			PageToken string `json:"pageToken"`
		}
		_ = json.Unmarshal(request.Params, &query)
		tasks := h.store.list(owner, query.ContextID, query.State)
		if query.PageSize < 1 || query.PageSize > 100 {
			query.PageSize = 50
		}
		start := 0
		if query.PageToken != "" {
			start, _ = strconv.Atoi(query.PageToken)
			if start < 0 || start > len(tasks) {
				start = 0
			}
		}
		end := min(start+query.PageSize, len(tasks))
		next := ""
		if end < len(tasks) {
			next = strconv.Itoa(end)
		}
		return map[string]any{"tasks": tasks[start:end], "nextPageToken": next, "pageSize": query.PageSize, "totalSize": len(tasks)}, nil, false
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
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, _ := w.(http.Flusher)
		writeSSE(w, flusher, statusEvent(task.ID, task.ContextID, task.Status.State))
		return nil, nil, true
	default:
		return nil, &RPCError{Code: codeMethodNotFound, Message: "method not found: " + request.Method}, false
	}
}

func ownerFor(r *http.Request) string {
	sum := sha256.Sum256([]byte(r.Header.Get("Authorization") + "|" + r.Header.Get("X-Voie-Conversation-Scope")))
	return hex.EncodeToString(sum[:])
}

func (h *Handler) send(ctx context.Context, message Message, owner string) (Task, *RPCError) {
	request, err := h.completionRequest(message)
	if err != nil {
		return Task{}, err
	}
	contextID, realConversation, ctxErr := h.ensureContext(ctx, message)
	if ctxErr != nil {
		return Task{}, &RPCError{Code: codeUnavailable, Message: ctxErr.Error()}
	}
	if realConversation {
		request.ConversationID = contextID
	}
	id := message.MessageID
	if id == "" {
		id = uuid.NewString()
	}
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if _, ok := h.store.create(id, owner, callCtx, cancel); !ok {
		return Task{}, &RPCError{Code: codeUnavailable, Message: "task ID is already present or task store is at capacity"}
	}
	h.store.appendHistory(id, owner, message)
	h.store.setStatus(id, owner, "TASK_STATE_WORKING")
	var answer string
	response, callErr := h.service.Complete(callCtx, request)
	if callErr != nil {
		h.store.setStatus(id, owner, "TASK_STATE_FAILED")
		return Task{}, failureFor(callErr)
	}
	if len(response.Choices) > 0 {
		answer = response.Choices[0].Message.Content
		request.ConversationID = response.ConversationID
	}
	if answer == "" {
		h.store.setStatus(id, owner, "TASK_STATE_FAILED")
		return Task{}, &RPCError{Code: codeUnavailable, Message: "upstream returned no text"}
	}
	task := Task{
		ID:        id,
		ContextID: contextID,
		Status:    TaskStatus{State: "TASK_STATE_COMPLETED", Timestamp: timestamp()},
		Artifacts: []Artifact{{ArtifactID: uuid.NewString(), Parts: []Part{{Text: answer, MediaType: "text/plain"}}}},
	}
	h.store.setTask(id, owner, task)
	return task, nil
}

func (h *Handler) stream(w http.ResponseWriter, r *http.Request, message Message, owner string) *RPCError {
	request, rpcErr := h.completionRequest(message)
	if rpcErr != nil {
		return rpcErr
	}
	contextID, realConversation, ctxErr := h.ensureContext(r.Context(), message)
	if ctxErr != nil {
		return &RPCError{Code: codeUnavailable, Message: ctxErr.Error()}
	}
	if realConversation {
		request.ConversationID = contextID
	}
	taskID := message.MessageID
	if taskID == "" {
		taskID = uuid.NewString()
	}
	callCtx, cancel := context.WithCancel(r.Context())
	defer cancel()
	if _, ok := h.store.create(taskID, owner, callCtx, cancel); !ok {
		return &RPCError{Code: codeUnavailable, Message: "task ID is already present or task store is at capacity"}
	}
	h.store.appendHistory(taskID, owner, message)
	h.store.setStatus(taskID, owner, "TASK_STATE_WORKING")

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
		h.store.setStatus(taskID, owner, "TASK_STATE_FAILED")
		writeSSE(w, flusher, statusEvent(taskID, contextID, "TASK_STATE_FAILED"))
		return nil
	}
	task := Task{
		ID:        taskID,
		ContextID: contextID,
		Status:    TaskStatus{State: "TASK_STATE_COMPLETED", Timestamp: timestamp()},
		Artifacts: []Artifact{{ArtifactID: taskID + "-artifact", Parts: []Part{{Text: answer.String(), MediaType: "text/plain"}}}},
	}
	h.store.setTask(taskID, owner, task)
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

// ensureContext resolves a client contextId to a real voie conversation,
// creating one when the client did not supply one.
func (h *Handler) ensureContext(ctx context.Context, message Message) (string, bool, error) {
	if id := strings.TrimSpace(message.ContextID); id != "" {
		return id, true, nil
	}
	conversation, err := h.service.CreateConversation(ctx)
	if err != nil {
		// A service without conversation storage still needs a stable id, but
		// must not force it into CompletionRequest.ConversationID.
		return uuid.NewString(), false, nil
	}
	return conversation.ID, true, nil
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
	if strings.EqualFold(strategy, string(app.StrategyClassic)) && model == "" && metaString(meta, "skill") == "" {
		return app.CompletionRequest{}, &RPCError{Code: codeInvalidParams, Message: "classic requests require metadata.model or skill"}
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
			if part.Data != nil || part.URL != "" || part.Raw != "" {
				return app.CompletionRequest{}, &RPCError{Code: codeContentType, Message: "unsupported content type"}
			}
			continue
		}
		role := strings.TrimPrefix(strings.ToLower(message.Role), "role_")
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
