package clinebridge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// Client is a minimal JSON-RPC 2.0 client for an ACP agent over NDJSON.
type Client struct {
	in      io.Writer
	out     io.Reader
	nextID  atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan rpcMessage
	updates chan map[string]any
	writeMu sync.Mutex
}

type rpcMessage struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      *int64         `json:"id,omitempty"`
	Method  string         `json:"method,omitempty"`
	Params  map[string]any `json:"params,omitempty"`
	Result  any            `json:"result,omitempty"`
	Error   any            `json:"error,omitempty"`
}

func NewClient(in io.Writer, out io.Reader) *Client {
	c := &Client{
		in:      in,
		out:     out,
		pending: make(map[int64]chan rpcMessage),
		updates: make(chan map[string]any, 256),
	}
	go c.readLoop()
	return c
}

func (c *Client) send(message rpcMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(c.in, string(data))
	return err
}

func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.out)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var message rpcMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			continue
		}
		if message.ID != nil && message.Method != "" {
			// Agent-initiated request: deny tool permissions so a chat
			// completion can never modify the workspace. ACP option IDs are
			// agent-chosen; select the option by its stable `kind`.
			reply := map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}
			if message.Method == "session/request_permission" {
				options, _ := message.Params["options"].([]any)
				for _, raw := range options {
					option, _ := raw.(map[string]any)
					if option["kind"] == "reject_once" {
						if id, ok := option["optionId"].(string); ok && id != "" {
							reply = map[string]any{"outcome": map[string]any{
								"outcome": "selected", "optionId": id,
							}}
						}
						break
					}
				}
			}
			_ = c.send(rpcMessage{JSONRPC: "2.0", ID: message.ID, Result: reply})
			continue
		}
		if message.ID != nil {
			c.mu.Lock()
			ch := c.pending[*message.ID]
			c.mu.Unlock()
			if ch != nil {
				ch <- message
			}
			continue
		}
		if message.Method != "" {
			c.updates <- message.Params
		}
	}
	close(c.updates)
}

func (c *Client) call(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	id := c.nextID.Add(1)
	ch := make(chan rpcMessage, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	if err := c.send(rpcMessage{JSONRPC: "2.0", ID: &id, Method: method, Params: params}); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case message := <-ch:
		if message.Error != nil {
			return nil, fmt.Errorf("%s: %v", method, message.Error)
		}
		result, _ := message.Result.(map[string]any)
		return result, nil
	}
}

// Handshake initializes an ACP session and pins the requested model.
func (c *Client) Handshake(ctx context.Context, cwd, model string) (string, error) {
	if _, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{"fs": map[string]any{}},
		"clientInfo":         map[string]any{"name": "voie-cline-bridge", "version": "0.1.0"},
	}); err != nil {
		return "", err
	}
	result, err := c.call(ctx, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
	if err != nil {
		return "", err
	}
	sessionID, _ := result["sessionId"].(string)
	if sessionID == "" {
		return "", fmt.Errorf("ACP session/new returned no sessionId")
	}
	// A missing optional method must not break the handshake.
	_ = c.SetModel(ctx, sessionID, model)
	return sessionID, nil
}

// SetModel pins the ACP session to a model. Not every agent implements this
// optional method; callers decide whether to tolerate the error.
func (c *Client) SetModel(ctx context.Context, sessionID, model string) error {
	if model == "" || model == "auto" {
		return nil
	}
	_, err := c.call(ctx, "session/setConfigOption", map[string]any{
		"sessionId": sessionID,
		"configId":  "model",
		"value":     model,
	})
	return err
}

// Prompt sends one turn and collects agent message chunks until the turn ends.
func (c *Client) Prompt(ctx context.Context, sessionID, text string, onChunk func(string)) (string, error) {
	handle := func(params map[string]any) {
		update, _ := params["update"].(map[string]any)
		if update == nil || update["sessionUpdate"] != "agent_message_chunk" {
			return
		}
		content, _ := update["content"].(map[string]any)
		token, _ := content["text"].(string)
		if token != "" && onChunk != nil {
			onChunk(token)
		}
	}
	type promptResult struct {
		reason string
		err    error
	}
	result := make(chan promptResult, 1)
	go func() {
		response, err := c.call(ctx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt":    []any{map[string]any{"type": "text", "text": text}},
		})
		reason, _ := response["stopReason"].(string)
		result <- promptResult{reason: reason, err: err}
	}()
	for {
		select {
		case res := <-result:
			for {
				select {
				case params := <-c.updates:
					handle(params)
				default:
					return res.reason, res.err
				}
			}
		case params, ok := <-c.updates:
			if !ok {
				res := <-result
				return res.reason, res.err
			}
			handle(params)
		}
	}
}
