package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/internal/brand"
	"github.com/benoitpetit/voie/utils"
	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	listModelsToolName         = "list_models"
	listProvidersToolName      = "list_providers"
	chatCompletionToolName     = "chat_completion"
	createConversationToolName = "create_conversation"
	listConversationsToolName  = "list_conversations"
	getConversationToolName    = "get_conversation"
	deleteConversationToolName = "delete_conversation"
)

var toolNames = []string{listModelsToolName, listProvidersToolName, chatCompletionToolName, createConversationToolName, listConversationsToolName, getConversationToolName, deleteConversationToolName}

type emptyInput struct{}

type modelItem struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	OwnedBy  string `json:"owned_by"`
}

type modelsOutput struct {
	Models []modelItem `json:"models"`
}

type providerItem struct {
	Name            string   `json:"name"`
	Label           string   `json:"label"`
	Alive           bool     `json:"alive"`
	DefaultModel    string   `json:"default_model"`
	SupportedModels []string `json:"supported_models"`
}

type providersOutput struct {
	Providers []providerItem `json:"providers"`
}

type chatCompletionInput struct {
	Model          string                `json:"model,omitempty" jsonschema:"optional model ID for classic requests; omitted for auto or ensemble"`
	Messages       []app.Message         `json:"messages" jsonschema:"conversation messages"`
	Provider       string                `json:"provider,omitempty" jsonschema:"optional provider name"`
	Strategy       app.Strategy          `json:"strategy,omitempty" jsonschema:"classic, auto, or ensemble"`
	Task           string                `json:"task,omitempty" jsonschema:"optional task hint for automatic selection: coding, reasoning, writing, translation, summarization, or general; explicit ensemble models take precedence"`
	Models         []string              `json:"models,omitempty" jsonschema:"optional explicit ensemble model IDs"`
	Fallback       *app.FallbackOverride `json:"fallback,omitempty" jsonschema:"override the global fallback policy for this request: enabled, max_retries, max_fallback_models, or an explicit models list that replaces configured candidates"`
	ConversationID string                `json:"conversation_id,omitempty" jsonschema:"optional local conversation ID"`
}

type chatCompletionOutput struct {
	Text           string           `json:"text"`
	Model          string           `json:"model"`
	Provider       string           `json:"provider"`
	ConversationID string           `json:"conversation_id,omitempty"`
	Routing        *app.RoutingInfo `json:"routing,omitempty"`
}

type conversationIDInput struct {
	ID string `json:"id" jsonschema:"conversation ID"`
}

func NewServer(service *app.Service) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: brand.Name, Version: brand.Version}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name: listModelsToolName, Description: "List supported model IDs and their providers.",
	}, logToolCall(listModelsToolName, func(context.Context, *mcp.CallToolRequest, emptyInput) (*mcp.CallToolResult, modelsOutput, error) {
		models := service.ListModels()
		output := modelsOutput{Models: make([]modelItem, 0, len(models))}
		var lines []string
		for _, model := range models {
			provider, _ := model.Meta["provider"].(string)
			output.Models = append(output.Models, modelItem{ID: model.ID, Provider: provider, OwnedBy: model.OwnedBy})
			lines = append(lines, fmt.Sprintf("%s (%s)", model.ID, provider))
		}
		return textToolResult("Supported models:\n" + strings.Join(lines, "\n")), output, nil
	}))
	mcp.AddTool(server, &mcp.Tool{
		Name: listProvidersToolName, Description: "List providers and check whether their URLs are reachable over HTTP.",
	}, logToolCall(listProvidersToolName, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, providersOutput, error) {
		providers, err := service.ListProviders(ctx)
		if err != nil {
			return nil, providersOutput{}, err
		}
		output := providersOutput{Providers: make([]providerItem, 0, len(providers))}
		var lines []string
		for _, provider := range providers {
			output.Providers = append(output.Providers, providerItem{
				Name: provider.Name, Label: provider.Label, Alive: provider.Alive,
				DefaultModel: provider.DefaultModel, SupportedModels: provider.SupportedModels,
			})
			state := "unreachable"
			if provider.Alive {
				state = "reachable"
			}
			lines = append(lines, fmt.Sprintf("%s (%s): %s", provider.Name, provider.Label, state))
		}
		return textToolResult("Providers (HTTP reachability):\n" + strings.Join(lines, "\n")), output, nil
	}))
	mcp.AddTool(server, &mcp.Tool{
		Name: chatCompletionToolName, Description: "Generate a non-streaming completion using classic, automatic, or ensemble model routing.",
	}, logToolCall(chatCompletionToolName, func(ctx context.Context, _ *mcp.CallToolRequest, input chatCompletionInput) (*mcp.CallToolResult, chatCompletionOutput, error) {
		response, err := service.Complete(ctx, app.CompletionRequest{Model: input.Model, Provider: input.Provider, Strategy: input.Strategy, Task: input.Task, Models: input.Models, Fallback: input.Fallback, ConversationID: input.ConversationID, Messages: input.Messages})
		if err != nil {
			return nil, chatCompletionOutput{}, err
		}
		output := chatCompletionOutput{
			Text:           response.Choices[0].Message.Content,
			Model:          response.Model,
			Provider:       response.Provider,
			ConversationID: response.ConversationID,
			Routing:        response.Routing,
		}
		return textToolResult(output.Text), output, nil
	}))
	mcp.AddTool(server, &mcp.Tool{Name: createConversationToolName, Description: "Create a local persistent conversation."}, logToolCall(createConversationToolName, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, app.Conversation, error) {
		c, err := service.CreateConversation(ctx)
		if err != nil {
			return nil, app.Conversation{}, err
		}
		return textToolResult(c.ID), c, nil
	}))
	mcp.AddTool(server, &mcp.Tool{Name: listConversationsToolName, Description: "List local conversations without transcript contents."}, logToolCall(listConversationsToolName, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, struct {
		Conversations []app.ConversationSummary `json:"conversations"`
	}, error) {
		items, err := service.ListConversations(ctx)
		out := struct {
			Conversations []app.ConversationSummary `json:"conversations"`
		}{items}
		if err != nil {
			return nil, out, err
		}
		return textToolResult(fmt.Sprintf("%d conversations", len(items))), out, nil
	}))
	mcp.AddTool(server, &mcp.Tool{Name: getConversationToolName, Description: "Read a local conversation and its turns."}, logToolCall(getConversationToolName, func(ctx context.Context, _ *mcp.CallToolRequest, input conversationIDInput) (*mcp.CallToolResult, app.Conversation, error) {
		c, err := service.GetConversation(ctx, input.ID)
		if err != nil {
			return nil, app.Conversation{}, err
		}
		return textToolResult(input.ID), c, nil
	}))
	mcp.AddTool(server, &mcp.Tool{Name: deleteConversationToolName, Description: "Delete a local conversation and its expiration tombstone."}, logToolCall(deleteConversationToolName, func(ctx context.Context, _ *mcp.CallToolRequest, input conversationIDInput) (*mcp.CallToolResult, emptyInput, error) {
		err := service.DeleteConversation(ctx, input.ID)
		if err != nil {
			return nil, emptyInput{}, err
		}
		return textToolResult("Conversation deleted."), emptyInput{}, nil
	}))
	return server
}

func Run(ctx context.Context, service *app.Service) error {
	utils.Info("Starting %s MCP server version %s; tools: %s", brand.Name, brand.Version, strings.Join(toolNames, ", "))
	return NewServer(service).Run(ctx, &mcp.StdioTransport{})
}

func textToolResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}
