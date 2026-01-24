package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

var runServer = func(ctx context.Context, server *mcp.Server) error {
	return server.Run(ctx, &mcp.StdioTransport{})
}

// RunPromptServer starts an MCP prompt server over stdio.
func RunPromptServer(ctx context.Context, version string, commands []config.SlashCommand) error {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "agent-layer",
		Version: version,
	}, nil)

	for _, cmd := range commands {
		cmd := cmd
		prompt := &mcp.Prompt{
			Name:        cmd.Name,
			Description: cmd.Description,
		}
		server.AddPrompt(prompt, promptHandler(cmd))
	}

	if err := runServer(ctx, server); err != nil {
		return fmt.Errorf(messages.McpRunPromptServerFailedFmt, err)
	}

	return nil
}

func promptHandler(cmd config.SlashCommand) func(context.Context, *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: cmd.Description,
			Messages: []*mcp.PromptMessage{
				{
					Role:    "user",
					Content: &mcp.TextContent{Text: cmd.Body},
				},
			},
		}, nil
	}
}
