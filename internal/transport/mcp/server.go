package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	healthToolName             = "health"
)

var toolNames = []string{listModelsToolName, listProvidersToolName, chatCompletionToolName, createConversationToolName, listConversationsToolName, getConversationToolName, deleteConversationToolName, healthToolName}

type emptyInput struct{}

type listModelsInput struct {
	Tier     string `json:"tier,omitempty" jsonschema:"optional filter: free or paid; omit for all"`
	Provider string `json:"provider,omitempty" jsonschema:"optional provider name filter"`
}

// modelItem is the shared app.ModelInfo so MCP, CLI and HTTP expose the same
// tier/price object and metadata fields.
type modelItem = app.ModelInfo

type modelGroup struct {
	Name   string          `json:"name"`
	Models []app.ModelInfo `json:"models"`
}

type modelsOutput struct {
	Models []app.ModelInfo `json:"models"`
	Groups []modelGroup    `json:"groups"`
}

type providersOutput struct {
	Providers []app.ProviderInfo `json:"providers"`
}

type chatCompletionInput struct {
	Model          string                      `json:"model,omitempty" jsonschema:"optional model ID for classic requests; omitted for auto or ensemble"`
	Messages       []app.Message               `json:"messages" jsonschema:"conversation messages"`
	Provider       string                      `json:"provider,omitempty" jsonschema:"optional provider name"`
	Strategy       app.Strategy                `json:"strategy,omitempty" jsonschema:"classic, auto, or ensemble"`
	Task           string                      `json:"task,omitempty" jsonschema:"optional routing profile name for automatic model selection; explicit ensemble models take precedence"`
	Models         []string                    `json:"models,omitempty" jsonschema:"legacy optional explicit ensemble model IDs; prefer ensemble.models"`
	Ensemble       *app.EnsembleRequestOptions `json:"ensemble,omitempty" jsonschema:"ensemble options: mode, candidate_count from 2 to 5, exact models list, chain_agents for chain compatibility, and debate_turns from 0 to 5 for debate"`
	Fallback       *app.FallbackOverride       `json:"fallback,omitempty" jsonschema:"override the global fallback policy for this request: enabled, max_retries, max_fallback_models, or an explicit models list that replaces configured candidates"`
	ConversationID string                      `json:"conversation_id,omitempty" jsonschema:"optional local conversation ID"`
	Stream         bool                        `json:"stream,omitempty" jsonschema:"optional; when true, collect streamed chunks into one result"`
}

type chatCompletionOutput struct {
	Error          *app.ErrorInfo   `json:"error,omitempty"`
	Text           string           `json:"text"`
	Model          string           `json:"model"`
	Provider       string           `json:"provider"`
	ConversationID string           `json:"conversation_id,omitempty"`
	Routing        *app.RoutingInfo `json:"routing,omitempty"`
}

type conversationListInput struct {
	Limit  int `json:"limit,omitempty" jsonschema:"optional maximum number of conversations, default 100"`
	Offset int `json:"offset,omitempty" jsonschema:"optional number of conversations to skip"`
}

type conversationIDInput struct {
	ID string `json:"id" jsonschema:"conversation ID"`
}

func NewServer(service *app.Service) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: brand.Name, Version: brand.Version}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name: listModelsToolName, Description: "List supported model IDs and their providers.",
	}, logToolCall(listModelsToolName, func(_ context.Context, _ *mcp.CallToolRequest, input listModelsInput) (*mcp.CallToolResult, modelsOutput, error) {
		tier := app.ModelTier(strings.ToLower(strings.TrimSpace(input.Tier)))
		if tier != "" && tier != app.TierFree && tier != app.TierPaid {
			return nil, modelsOutput{}, fmt.Errorf("tier must be free or paid")
		}
		provider := strings.TrimSpace(input.Provider)
		if provider != "" && !service.HasProvider(provider) {
			return nil, modelsOutput{}, fmt.Errorf("unknown provider %q", provider)
		}
		models := service.ListModelsFiltered(provider, tier)
		output := modelsOutput{Models: make([]app.ModelInfo, 0, len(models))}
		byGroup := make(map[string][]app.ModelInfo)
		for _, model := range models {
			group := model.Group
			if group == "" {
				group = model.OwnedBy
			}
			item := model
			item.Group = group
			output.Models = append(output.Models, item)
			byGroup[group] = append(byGroup[group], item)
		}
		names := make([]string, 0, len(byGroup))
		for name := range byGroup {
			names = append(names, name)
		}
		sort.Strings(names)
		var lines []string
		for _, name := range names {
			lines = append(lines, fmt.Sprintf("%s:", name))
			for _, item := range byGroup[name] {
				price := ""
				if item.Price != nil {
					if item.Price.Label != "" {
						price = " — " + item.Price.Label
					} else if item.Price.PromptPerToken != "" {
						price = " — " + item.Price.Currency + " " + item.Price.PromptPerToken + "/" + item.Price.CompletionPerToken
					}
				}
				provider, _ := item.Meta["provider"].(string)
				lines = append(lines, fmt.Sprintf("  %s (%s) [%s]%s", item.ID, provider, item.Tier, price))
			}
			output.Groups = append(output.Groups, modelGroup{Name: name, Models: byGroup[name]})
		}
		return textToolResult("Supported models by provider:\n" + strings.Join(lines, "\n")), output, nil
	}))
	mcp.AddTool(server, &mcp.Tool{
		Name: listProvidersToolName, Description: "List providers and check whether their URLs are reachable over HTTP.",
	}, logToolCall(listProvidersToolName, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, providersOutput, error) {
		providers, err := service.ListProviders(ctx)
		if err != nil {
			return nil, providersOutput{}, err
		}
		output := providersOutput{Providers: make([]app.ProviderInfo, 0, len(providers))}
		var lines []string
		for _, provider := range providers {
			output.Providers = append(output.Providers, provider)
			state := "unreachable"
			if provider.Alive {
				state = "reachable"
			}
			lines = append(lines, fmt.Sprintf("%s (%s): %s", provider.Name, provider.Label, state))
		}
		return textToolResult("Providers (HTTP reachability):\n" + strings.Join(lines, "\n")), output, nil
	}))
	mcp.AddTool(server, &mcp.Tool{
		Name: chatCompletionToolName, Description: "Generate a completion using classic, automatic, or ensemble model routing. Set stream to collect streamed chunks into one result.",
	}, logToolCall(chatCompletionToolName, func(ctx context.Context, _ *mcp.CallToolRequest, input chatCompletionInput) (*mcp.CallToolResult, chatCompletionOutput, error) {
		request := app.CompletionRequest{Model: input.Model, Provider: input.Provider, Strategy: input.Strategy, Task: input.Task, Models: input.Models, Ensemble: input.Ensemble, Fallback: input.Fallback, ConversationID: input.ConversationID, Messages: input.Messages}
		if input.Stream {
			var text strings.Builder
			var routing *app.RoutingInfo
			err := service.CompleteStreamWithInfo(ctx, request, func(chunk string, info *app.RoutingInfo) {
				text.WriteString(chunk)
				routing = info
			})
			if err != nil {
				return mcpChatError(err)
			}
			output := chatCompletionOutput{Text: text.String(), Model: input.Model, Routing: routing}
			if routing != nil && len(routing.Attempts) > 0 {
				last := routing.Attempts[len(routing.Attempts)-1]
				output.Model, output.Provider = last.Model, last.Provider
			}
			return textToolResult(output.Text), output, nil
		}
		response, err := service.Complete(ctx, request)
		if err != nil {
			return mcpChatError(err)
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
	mcp.AddTool(server, &mcp.Tool{Name: listConversationsToolName, Description: "List local conversations without transcript contents."}, logToolCall(listConversationsToolName, func(ctx context.Context, _ *mcp.CallToolRequest, input conversationListInput) (*mcp.CallToolResult, struct {
		Conversations []app.ConversationSummary `json:"conversations"`
	}, error) {
		limit, offset := input.Limit, input.Offset
		if limit < 1 || limit > 200 {
			limit = 100
		}
		if offset < 0 {
			offset = 0
		}
		items, err := service.ListConversations(ctx, limit, offset)
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
	mcp.AddTool(server, &mcp.Tool{Name: healthToolName, Description: "Report local server readiness and catalogue sizes."}, logToolCall(healthToolName, func(context.Context, *mcp.CallToolRequest, emptyInput) (*mcp.CallToolResult, struct {
		Status    string `json:"status"`
		Providers int    `json:"providers"`
		Models    int    `json:"models"`
	}, error) {
		out := struct {
			Status    string `json:"status"`
			Providers int    `json:"providers"`
			Models    int    `json:"models"`
		}{Status: "ok", Providers: len(service.ListProviderMetadata()), Models: len(service.ListModels())}
		return textToolResult("ok"), out, nil
	}))
	return server
}

func Run(ctx context.Context, service *app.Service) error {
	utils.Info("Starting %s MCP server version %s; tools: %s", brand.Name, brand.Version, strings.Join(toolNames, ", "))
	err := NewServer(service).Run(ctx, &mcp.StdioTransport{})
	if ctx.Err() != nil && errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func mcpChatError(err error) (*mcp.CallToolResult, chatCompletionOutput, error) {
	info := app.DescribeError(err)
	result := textToolResult(info.Message)
	result.IsError = true
	return result, chatCompletionOutput{Error: &info}, nil
}

func textToolResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}
