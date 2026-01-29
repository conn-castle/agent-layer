package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

type promptServerRunner func(ctx context.Context, server *mcp.Server) error

// RunPromptServer starts an MCP prompt server over stdio.
func RunPromptServer(ctx context.Context, version string, commands []config.SlashCommand) error {
	return runPromptServer(ctx, version, commands, defaultPromptServerRunner)
}

// runPromptServer builds the MCP prompt server and runs it using the provided runner.
func runPromptServer(ctx context.Context, version string, commands []config.SlashCommand, runner promptServerRunner) error {
	if runner == nil {
		return fmt.Errorf(messages.McpRunPromptServerFailedFmt, errors.New("prompt server runner is nil"))
	}
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

	if err := runner(ctx, server); err != nil {
		return fmt.Errorf(messages.McpRunPromptServerFailedFmt, err)
	}

	return nil
}

// defaultPromptServerRunner runs the MCP prompt server over stdio.
func defaultPromptServerRunner(ctx context.Context, server *mcp.Server) error {
	return server.Run(ctx, &mcp.StdioTransport{})
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
