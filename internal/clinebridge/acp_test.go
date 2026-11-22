package clinebridge

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
)

type fakeAgent struct {
	mu        sync.Mutex
	written   bytes.Buffer
	responses chan string
	script    []string
}

func newFakeAgent() *fakeAgent {
	return &fakeAgent{responses: make(chan string, 32)}
}

func (a *fakeAgent) Write(p []byte) (int, error) {
	a.mu.Lock()
	n, err := a.written.Write(p)
	if err == nil && strings.Contains(string(p), `"method"`) && len(a.script) > 0 {
		a.responses <- a.script[0] + "\n"
		a.script = a.script[1:]
	}
	a.mu.Unlock()
	return n, err
}

func (a *fakeAgent) Read(p []byte) (int, error) {
	line, ok := <-a.responses
	if !ok {
		return 0, context.Canceled
	}
	return copy(p, line), nil
}

// push queues one or more responses, delivered in order of incoming requests.
func (a *fakeAgent) push(lines ...string) {
	a.mu.Lock()
	a.script = append(a.script, lines...)
	a.mu.Unlock()
}

func (a *fakeAgent) pushNow(line string) { a.responses <- line + "\n" }

func (a *fakeAgent) sent() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.written.String()
}

func TestHandshakeSequence(t *testing.T) {
	agent := newFakeAgent()
	client := NewClient(agent, agent)
	agent.push(`{"jsonrpc":"2.0","id":1,"result":{}}`)
	agent.push(`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"s1"}}`)
	agent.push(`{"jsonrpc":"2.0","id":3,"result":{}}`)

	sessionID, err := client.Handshake(context.Background(), "/tmp/scratch", "cline-free/test-model")
	if err != nil {
		t.Fatal(err)
	}
	if sessionID != "s1" {
		t.Errorf("sessionID = %q", sessionID)
	}
	sent := agent.sent()
	for _, method := range []string{"initialize", "session/new", "session/setConfigOption"} {
		if !strings.Contains(sent, method) {
			t.Errorf("request sequence missing %q:\n%s", method, sent)
		}
	}
	if strings.Contains(sent, `"authenticate"`) {
		t.Errorf("authenticate must be skipped when credentials already exist:\n%s", sent)
	}
}

func TestHandshakeToleratesMissingSetConfigOption(t *testing.T) {
	agent := newFakeAgent()
	client := NewClient(agent, agent)
	agent.push(`{"jsonrpc":"2.0","id":1,"result":{}}`)
	agent.push(`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"s1"}}`)
	agent.push(`{"jsonrpc":"2.0","id":3,"error":{"code":-32601,"message":"Method not found"}}`)
	sessionID, err := client.Handshake(context.Background(), "/tmp/scratch", "cline-free/model-a")
	if err != nil {
		t.Fatalf("handshake failed on an unsupported optional method: %v", err)
	}
	if sessionID != "s1" {
		t.Errorf("sessionID = %q", sessionID)
	}
}

func TestCollectsAgentMessageChunks(t *testing.T) {
	agent := newFakeAgent()
	client := NewClient(agent, agent)
	agent.push(`{"jsonrpc":"2.0","id":1,"result":{}}`)
	agent.push(`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"s1"}}`)
	agent.push(`{"jsonrpc":"2.0","id":3,"result":{}}`)
	if _, err := client.Handshake(context.Background(), "/tmp/scratch", "m"); err != nil {
		t.Fatal(err)
	}

	agent.pushNow(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"text":"Hel"}}}}`)
	agent.pushNow(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"text":"lo"}}}}`)
	agent.push(`{"jsonrpc":"2.0","id":4,"result":{"stopReason":"end_turn"}}`)

	var answer strings.Builder
	reason, err := client.Prompt(context.Background(), "s1", "hi", func(token string) { answer.WriteString(token) })
	if err != nil {
		t.Fatal(err)
	}
	if reason != "end_turn" {
		t.Errorf("stopReason = %q, want end_turn", reason)
	}
	if got, want := answer.String(), "Hello"; got != want {
		t.Errorf("answer = %q, want %q", got, want)
	}
}

func TestPermissionRequestsAreDenied(t *testing.T) {
	agent := newFakeAgent()
	client := NewClient(agent, agent)
	agent.pushNow(`{"jsonrpc":"2.0","id":9,"method":"session/request_permission","params":{"sessionId":"s1","options":[{"optionId":"opt-allow","kind":"allow_once"},{"optionId":"opt-reject","kind":"reject_once"}]}}`)
	agent.push(`{"jsonrpc":"2.0","id":1,"result":{}}`)
	agent.push(`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"s1"}}`)
	agent.push(`{"jsonrpc":"2.0","id":3,"result":{}}`)
	if _, err := client.Handshake(context.Background(), "/tmp/scratch", "m"); err != nil {
		t.Fatal(err)
	}
	sent := agent.sent()
	if !strings.Contains(sent, `"optionId":"opt-reject"`) {
		t.Errorf("permission denial did not use the offered reject option:\n%s", sent)
	}
	if strings.Contains(sent, `"optionId":"reject_once"`) {
		t.Errorf("hardcoded optionId leaked:\n%s", sent)
	}
}
