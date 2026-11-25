// Package a2a implements the Agent2Agent JSON-RPC binding on top of the
// shared voie service. It exposes voie as an A2A server; external agents are
// called by providers.A2AProvider.
package a2a

import "encoding/json"

// AgentInterface declares one way to reach the agent.
type AgentInterface struct {
	URL             string `json:"url"`
	ProtocolBinding string `json:"protocolBinding"`
	ProtocolVersion string `json:"protocolVersion"`
}

// AgentCapabilities declares optional A2A features.
type AgentCapabilities struct {
	Streaming         bool `json:"streaming"`
	PushNotifications bool `json:"pushNotifications,omitempty"`
	ExtendedAgentCard bool `json:"extendedAgentCard,omitempty"`
}

// AgentSkill describes one voie capability offered to A2A clients.
type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes"`
	OutputModes []string `json:"outputModes"`
}

// AgentCard is the public discovery document.
type AgentCard struct {
	Name                string            `json:"name"`
	Description         string            `json:"description"`
	Provider            map[string]string `json:"provider,omitempty"`
	SupportedInterfaces []AgentInterface  `json:"supportedInterfaces"`
	Capabilities        AgentCapabilities `json:"capabilities"`
	Skills              []AgentSkill      `json:"skills"`
	DefaultInputModes   []string          `json:"defaultInputModes"`
	DefaultOutputModes  []string          `json:"defaultOutputModes"`
}

// AgentCardFor returns the card advertised at /.well-known/agent-card.json.
func AgentCardFor(publicURL string) AgentCard {
	base := publicURL
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	return AgentCard{
		Name:                "voie",
		Description:         "OpenAI-compatible model relay exposing classic, auto and ensemble completion through CLI, HTTP, MCP and A2A.",
		Provider:            map[string]string{"name": "voie", "url": "https://github.com/benoitpetit/voie"},
		SupportedInterfaces: []AgentInterface{{URL: base + "/a2a", ProtocolBinding: "JSONRPC", ProtocolVersion: "1.0"}},
		Capabilities:        AgentCapabilities{Streaming: true, ExtendedAgentCard: true},
		Skills: []AgentSkill{
			{ID: "voie.chat.classic", Name: "Classic completion", Description: "Call one explicit model ID, with shared retry and fallback policy.", InputModes: []string{"text/plain"}, OutputModes: []string{"text/plain"}, Examples: []string{"Summarize this text"}},
			{ID: "voie.chat.auto", Name: "Automatic model selection", Description: "Select one eligible model from a routing profile.", InputModes: []string{"text/plain"}, OutputModes: []string{"text/plain"}, Examples: []string{"Review this code"}},
			{ID: "voie.chat.ensemble.independent", Name: "Independent ensemble", Description: "Run parallel agents and synthesize their answers.", InputModes: []string{"text/plain"}, OutputModes: []string{"text/plain"}},
			{ID: "voie.chat.ensemble.chain", Name: "Chain ensemble", Description: "Each agent reviews the previous draft before synthesis.", InputModes: []string{"text/plain"}, OutputModes: []string{"text/plain"}},
			{ID: "voie.chat.ensemble.debate", Name: "Debate ensemble", Description: "Agents revise peers' drafts for a bounded number of rounds.", InputModes: []string{"text/plain"}, OutputModes: []string{"text/plain"}},
			{ID: "voie.models.discover", Name: "Model discovery", Description: "List current model IDs, tiers, prices and providers.", InputModes: []string{"text/plain"}, OutputModes: []string{"application/json"}},
			{ID: "voie.conversation.manage", Name: "Conversation management", Description: "Create, resume and inspect local conversation history.", InputModes: []string{"text/plain"}, OutputModes: []string{"application/json"}},
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
	}
}

// RPCRequest is a JSON-RPC 2.0 request.
type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// RPCResponse is a JSON-RPC 2.0 response.
type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError carries a JSON-RPC error code and an A2A reason in Data.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Part is a unified A2A 1.0 content part.
type Part struct {
	Text      string `json:"text,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
}

// Message is one A2A conversation turn.
type Message struct {
	Role      string         `json:"role"`
	MessageID string         `json:"messageId,omitempty"`
	Parts     []Part         `json:"parts"`
	ContextID string         `json:"contextId,omitempty"`
	TaskID    string         `json:"taskId,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TaskStatus describes the current task state.
type TaskStatus struct {
	State     string   `json:"state"`
	Timestamp string   `json:"timestamp"`
	Message   *Message `json:"message,omitempty"`
}

// Artifact is an agent output composed of parts.
type Artifact struct {
	ArtifactID string `json:"artifactId"`
	Parts      []Part `json:"parts"`
}

// Task is the A2A unit of work.
type Task struct {
	ID        string     `json:"id"`
	ContextID string     `json:"contextId"`
	Status    TaskStatus `json:"status"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
	History   []Message  `json:"history,omitempty"`
}

// SendParams is the payload of SendMessage and SendStreamingMessage.
type SendParams struct {
	Message       Message        `json:"message"`
	Configuration map[string]any `json:"configuration,omitempty"`
}

// StatusUpdateEvent is the A2A 1.0 status event wrapper.
type StatusUpdateEvent struct {
	StatusUpdate struct {
		TaskID    string     `json:"taskId"`
		ContextID string     `json:"contextId"`
		Status    TaskStatus `json:"status"`
	} `json:"statusUpdate"`
}

// ArtifactUpdateEvent is the A2A 1.0 artifact event wrapper.
type ArtifactUpdateEvent struct {
	ArtifactUpdate struct {
		TaskID    string   `json:"taskId"`
		ContextID string   `json:"contextId"`
		Artifact  Artifact `json:"artifact"`
		Index     int      `json:"index"`
	} `json:"artifactUpdate"`
}
