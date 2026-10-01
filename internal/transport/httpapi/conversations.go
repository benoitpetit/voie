package httpapi

import (
	"net/http"
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
		items, err := h.service.ListConversations(r.Context())
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
