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
	listModelsToolName     = "list_models"
	listProvidersToolName  = "list_providers"
	chatCompletionToolName = "chat_completion"
)

var toolNames = []string{listModelsToolName, listProvidersToolName, chatCompletionToolName}

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
	Model    string        `json:"model" jsonschema:"explicit model ID from list_models"`
	Messages []app.Message `json:"messages" jsonschema:"conversation messages"`
	Provider string        `json:"provider,omitempty" jsonschema:"optional provider name"`
}

type chatCompletionOutput struct {
	Text     string `json:"text"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
}

func NewServer(service *app.Service) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: brand.Name, Version: brand.Version}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name: listModelsToolName, Description: "List supported model IDs and their providers.",
	}, func(context.Context, *mcp.CallToolRequest, emptyInput) (*mcp.CallToolResult, modelsOutput, error) {
		models := service.ListModels()
		output := modelsOutput{Models: make([]modelItem, 0, len(models))}
		var lines []string
		for _, model := range models {
			provider, _ := model.Meta["provider"].(string)
			output.Models = append(output.Models, modelItem{ID: model.ID, Provider: provider, OwnedBy: model.OwnedBy})
			lines = append(lines, fmt.Sprintf("%s (%s)", model.ID, provider))
		}
		return textToolResult("Supported models:\n" + strings.Join(lines, "\n")), output, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: listProvidersToolName, Description: "List providers and check whether their URLs are reachable over HTTP.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, providersOutput, error) {
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
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: chatCompletionToolName, Description: "Generate a non-streaming completion using an explicit supported model ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input chatCompletionInput) (*mcp.CallToolResult, chatCompletionOutput, error) {
		response, err := service.Complete(ctx, app.CompletionRequest{Model: input.Model, Provider: input.Provider, Messages: input.Messages})
		if err != nil {
			return nil, chatCompletionOutput{}, err
		}
		output := chatCompletionOutput{
			Text:     response.Choices[0].Message.Content,
			Model:    response.Model,
			Provider: response.Provider,
		}
		return textToolResult(output.Text), output, nil
	})
	return server
}

func Run(ctx context.Context, service *app.Service) error {
	utils.Info("Starting %s MCP server version %s; tools: %s", brand.Name, brand.Version, strings.Join(toolNames, ", "))
	return NewServer(service).Run(ctx, &mcp.StdioTransport{})
}

func textToolResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}
