package utils

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	mathrand "math/rand"
	"net/http"
	"strings"
	"time"
)

func GenerateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func GenerateMachineID() string {
	mathrand.Seed(time.Now().UnixNano())
	digits := "0123456789"
	part1 := make([]byte, 16)
	for i := range part1 {
		part1[i] = digits[mathrand.Intn(len(digits))]
	}
	part2 := make([]byte, 25)
	for i := range part2 {
		part2[i] = digits[mathrand.Intn(len(digits))]
	}
	return string(part1) + "." + string(part2)
}

func ExtractLastUserMessage(messages interface{}) string {
	if msgs, ok := messages.([]interface{}); ok {
		for i := len(msgs) - 1; i >= 0; i-- {
			if msg, ok := msgs[i].(map[string]interface{}); ok {
				if msg["role"] == "user" {
					if content, ok := msg["content"].(string); ok {
						return content
					}
				}
			}
		}
	}
	return ""
}

func FormatPrompt(messages interface{}) string {
	var prompt strings.Builder
	if msgs, ok := messages.([]interface{}); ok {
		for _, m := range msgs {
			if msg, ok := m.(map[string]interface{}); ok {
				role := msg["role"]
				content := msg["content"]
				prompt.WriteString(fmt.Sprintf("%s: %s\n", role, content))
			}
		}
	}
	return prompt.String()
}

func SendJSONError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{
			"message": message,
			"type":    "error",
			"code":    fmt.Sprintf("%d", code),
		},
	})
}

func LogDebug(debug bool, format string, v ...interface{}) {
	if debug {
		log.Printf("[DEBUG] "+format, v...)
	}
}

func SendSSEChunk(w http.ResponseWriter, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}
