package clinebridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Cline hooks are JavaScript plugins exporting a beforeTool function that
// returns {skip:true}. A JSON config is not loaded by Cline.
var denyHook = []byte(`export default {
  hooks: {
    beforeTool: async () => ({
      skip: true,
      reason: "voie bridge serves chat completions only"
    })
  }
};
`)

// Run starts the ACP child and the OpenAI-compatible HTTP endpoint.
func Run(ctx context.Context, cfg Config) error {
	currentFreeFeedURL = cfg.FreeFeedURL
	if err := os.MkdirAll(cfg.ScratchDir, 0o700); err != nil {
		return err
	}
	if cfg.HookPath != "" {
		if err := writeDenyHook(cfg.HookPath); err != nil {
			return err
		}
	}
	args := cfg.AgentArgs
	if len(args) == 0 {
		args = []string{"--acp", "--auto-approve", "false", "--cwd", cfg.ScratchDir, "--hooks-dir", cfg.ScratchDir}
	}
	command := exec.CommandContext(ctx, cfg.ClinePath, args...)
	command.Env = append(os.Environ(), "CLINE_PROVIDER=cline", "CLINE_MODEL="+cfg.Model)
	stdin, err := command.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return err
	}
	defer func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = os.RemoveAll(cfg.ScratchDir)
	}()

	if cfg.StartupDelay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cfg.StartupDelay):
		}
	}
	client := NewClient(stdin, stdout)
	sessionID, err := client.Handshake(ctx, cfg.ScratchDir, cfg.Model)
	if err != nil {
		return err
	}

	// ACP sessions are sequential: serialize prompts so concurrent HTTP
	// requests cannot interleave agent message chunks.
	var promptMu sync.Mutex

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if cfg.LocalKey != "" && r.Header.Get("Authorization") != "Bearer "+cfg.LocalKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		ids, err := freeModels(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		data := make([]map[string]any, 0, len(ids))
		for _, id := range ids {
			data = append(data, map[string]any{"id": id, "object": "model"})
		}
		writeJSON(w, map[string]any{"object": "list", "data": data})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if cfg.LocalKey != "" && r.Header.Get("Authorization") != "Bearer "+cfg.LocalKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var request struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&request); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		prompt := flattenMessages(request.Messages)
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			promptMu.Lock()
			defer promptMu.Unlock()
			_, promptErr := client.Prompt(r.Context(), sessionID, prompt, func(token string) {
				payload, _ := json.Marshal(map[string]any{
					"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": token}}},
				})
				_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
				if flusher != nil {
					flusher.Flush()
				}
			})
			if promptErr != nil {
				payload, _ := json.Marshal(map[string]any{"error": map[string]any{"message": promptErr.Error()}})
				_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			}
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		var answer bytes.Buffer
		promptMu.Lock()
		defer promptMu.Unlock()
		reason, err := client.Prompt(r.Context(), sessionID, prompt, func(token string) { answer.WriteString(token) })
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, map[string]any{
			"object": "chat.completion",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": answer.String()},
				"finish_reason": finishReason(reason),
			}},
		})
	})
	server := &http.Server{Addr: cfg.Listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	return server.ListenAndServe()
}

func finishReason(reason string) string {
	switch reason {
	case "max_tokens":
		return "length"
	case "refusal":
		return "content_filter"
	case "cancelled":
		return "stop"
	default:
		return "stop"
	}
}

func writeDenyHook(path string) error {
	return os.WriteFile(path, denyHook, 0o600)
}

func flattenMessages(messages []struct {
	Content json.RawMessage `json:"content"`
}) string {
	var out strings.Builder
	for _, message := range messages {
		var text string
		if json.Unmarshal(message.Content, &text) == nil {
			out.WriteString(text)
			out.WriteString("\n")
			continue
		}
		var parts []struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(message.Content, &parts) == nil {
			for _, part := range parts {
				out.WriteString(part.Text)
			}
			out.WriteString("\n")
		}
	}
	return out.String()
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
