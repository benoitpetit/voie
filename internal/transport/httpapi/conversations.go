package httpapi

import (
	"net/http"
	"strconv"
	"strings"
)

func (h *apiHandler) conversations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		conversation, err := h.service.CreateConversation(r.Context())
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, conversation)
	case http.MethodGet:
		limit, offset := 100, 0
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 && parsed <= 100 {
				limit = parsed
			}
		}
		if raw := r.URL.Query().Get("offset"); raw != "" {
			if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed >= 0 {
				offset = parsed
			}
		}
		items, err := h.service.ListConversations(r.Context(), limit, offset)
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"object": "list", "data": items})
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed. Use GET or POST.")
	}
}

func (h *apiHandler) conversation(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/conversations/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "Conversation not found.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		conversation, err := h.service.GetConversation(r.Context(), id)
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, conversation)
	case http.MethodDelete:
		if err := h.service.DeleteConversation(r.Context(), id); err != nil {
			writeAppError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed. Use GET or DELETE.")
	}
}
