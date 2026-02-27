package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestRunPromptServerInvokesRunner(t *testing.T) {
	called := false
	runner := func(ctx context.Context, server *mcp.Server) error {
		called = true
		return nil
	}

	commands := []config.Skill{
		{Name: "alpha", Description: "desc", Body: "body"},
	}
	if err := runPromptServer(context.Background(), "v1", commands, runner); err != nil {
		t.Fatalf("RunPromptServer error: %v", err)
	}
	if !called {
		t.Fatalf("expected runner to be called")
	}
}

func TestRunPromptServerPropagatesError(t *testing.T) {
	runner := func(ctx context.Context, server *mcp.Server) error {
		return errors.New("boom")
	}

	err := runPromptServer(context.Background(), "v1", nil, runner)
	if err == nil || !strings.Contains(err.Error(), "failed to run MCP prompt server") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestRunPromptServerNilRunner(t *testing.T) {
	err := runPromptServer(context.Background(), "v1", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "failed to run MCP prompt server") {
		t.Fatalf("expected wrapped nil-runner error, got %v", err)
	}
}

func TestPromptHandler(t *testing.T) {
	cmd := config.Skill{
		Name:        "alpha",
		Description: "desc",
		Body:        "body",
	}
	handler := promptHandler(cmd)
	result, err := handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Description != "desc" {
		t.Fatalf("unexpected description: %q", result.Description)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if text, ok := result.Messages[0].Content.(*mcp.TextContent); !ok || text.Text != "body" {
		t.Fatalf("unexpected message content: %#v", result.Messages[0].Content)
	}
}
