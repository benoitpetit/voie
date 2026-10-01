package mcpserver

import (
	"context"
	"time"

	"github.com/benoitpetit/voie/utils"
	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func logToolCall[In, Out any](name string, handler mcp.ToolHandlerFor[In, Out]) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, request *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
		id := utils.GenerateID()[:8]
		started := time.Now()
		utils.Info("mcp request_id=%s tool=%s status=started", id, name)
		result, output, err := handler(ctx, request, input)
		status := "succeeded"
		if err != nil {
			status = "failed"
			utils.Warn("mcp request_id=%s tool=%s status=%s duration=%s", id, name, status, time.Since(started))
		} else {
			utils.Info("mcp request_id=%s tool=%s status=%s duration=%s", id, name, status, time.Since(started))
		}
		return result, output, err
	}
}
