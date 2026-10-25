package providers

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDuckAILiveChatCompletion(t *testing.T) {
	if os.Getenv("DUCKAI_LIVE_TEST") != "1" {
		t.Skip("set DUCKAI_LIVE_TEST=1 to send one live request to Duck.ai")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	response, err := NewDuckAI().ChatCompletion(ctx, []Message{{
		Role: "user", Content: "Reply with exactly PONG and nothing else.",
	}}, "gpt-6-luna")
	if err != nil {
		t.Fatalf("live Duck.ai completion failed: %v", err)
	}
	if len(response.Choices) == 0 {
		t.Fatal("live Duck.ai completion returned no choices")
	}
	if content := strings.TrimSpace(response.Choices[0].Message.Content); !strings.Contains(strings.ToUpper(content), "PONG") {
		t.Fatalf("live Duck.ai completion = %q, want a reply containing PONG", content)
	}
}
