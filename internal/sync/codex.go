package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nicholasjconn/agent-layer/internal/config"
	"github.com/nicholasjconn/agent-layer/internal/fsutil"
	"github.com/nicholasjconn/agent-layer/internal/projection"
)

const codexHeader = `# GENERATED FILE — MAY CONTAIN SECRETS
# This file is gitignored. Do not commit or share it.
# Source: .agent-layer/config.toml
# Regenerate: ./al sync

`

// WriteCodexConfig generates .codex/config.toml.
func WriteCodexConfig(root string, project *config.ProjectConfig) error {
	content, err := buildCodexConfig(project)
	if err != nil {
		return err
	}

	codexDir := filepath.Join(root, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", codexDir, err)
	}

	path := filepath.Join(codexDir, "config.toml")
	if err := fsutil.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	return nil
}

// WriteCodexRules generates .codex/rules/default.rules.
func WriteCodexRules(root string, project *config.ProjectConfig) error {
	content := buildCodexRules(project)
	rulesDir := filepath.Join(root, ".codex", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", rulesDir, err)
	}
	path := filepath.Join(rulesDir, "default.rules")
	if err := fsutil.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

func buildCodexConfig(project *config.ProjectConfig) (string, error) {
	var builder strings.Builder
	if project.Config.Agents.Codex.Model != "" {
		builder.WriteString(fmt.Sprintf("model = %q\n", project.Config.Agents.Codex.Model))
	}
	if project.Config.Agents.Codex.ReasoningEffort != "" {
		builder.WriteString(fmt.Sprintf("model_reasoning_effort = %q\n", project.Config.Agents.Codex.ReasoningEffort))
	}
	builder.WriteString(codexHeader)

	// Use placeholder syntax for initial resolution (needed for bearer_token_env_var extraction).
	resolved, err := projection.ResolveMCPServers(
		project.Config.MCP.Servers,
		project.Env,
		"codex",
		func(name string, _ string) string {
			return fmt.Sprintf("${%s}", name)
		},
	)
	if err != nil {
		return "", err
	}

	for i, server := range resolved {
		if i > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(fmt.Sprintf("[mcp_servers.%s]\n", server.ID))
		switch server.Transport {
		case "http":
			if err := writeCodexHTTPServer(&builder, server, project.Env); err != nil {
				return "", err
			}
		case "stdio":
			if err := writeCodexStdioServer(&builder, server, project.Env); err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("mcp server %s: unsupported transport %s", server.ID, server.Transport)
		}
	}

	return builder.String(), nil
}

func writeCodexHTTPServer(builder *strings.Builder, server projection.ResolvedMCPServer, env map[string]string) error {
	if len(server.Headers) > 0 {
		bearerEnv, err := extractBearerEnvVar(server.Headers)
		if err != nil {
			return fmt.Errorf("mcp server %s: %w", server.ID, err)
		}
		if bearerEnv != "" {
			builder.WriteString(fmt.Sprintf("bearer_token_env_var = %q\n", bearerEnv))
		}
	}
	// Resolve actual values in the URL (Codex doesn't support ${VAR} placeholders in URLs).
	resolvedURL, err := config.SubstituteEnvVars(server.URL, env)
	if err != nil {
		return fmt.Errorf("mcp server %s url: %w", server.ID, err)
	}
	builder.WriteString(fmt.Sprintf("url = %q\n", resolvedURL))
	return nil
}

func writeCodexStdioServer(builder *strings.Builder, server projection.ResolvedMCPServer, env map[string]string) error {
	// Resolve actual values in command (Codex doesn't support ${VAR} placeholders).
	resolvedCommand, err := config.SubstituteEnvVars(server.Command, env)
	if err != nil {
		return fmt.Errorf("mcp server %s command: %w", server.ID, err)
	}
	builder.WriteString(fmt.Sprintf("command = %q\n", resolvedCommand))

	if len(server.Args) > 0 {
		resolvedArgs := make([]string, 0, len(server.Args))
		for _, arg := range server.Args {
			resolvedArg, err := config.SubstituteEnvVars(arg, env)
			if err != nil {
				return fmt.Errorf("mcp server %s arg: %w", server.ID, err)
			}
			resolvedArgs = append(resolvedArgs, resolvedArg)
		}
		builder.WriteString(fmt.Sprintf("args = %s\n", tomlStringArray(resolvedArgs)))
	}

	if len(server.Env) > 0 {
		// Resolve actual values in env vars (Codex doesn't support ${VAR} placeholders).
		resolvedEnv := make(map[string]string, len(server.Env))
		for key, value := range server.Env {
			resolvedValue, err := config.SubstituteEnvVars(value, env)
			if err != nil {
				return fmt.Errorf("mcp server %s env %s: %w", server.ID, key, err)
			}
			resolvedEnv[key] = resolvedValue
		}
		builder.WriteString(fmt.Sprintf("env = %s\n", tomlInlineTable(resolvedEnv)))
	}

	return nil
}

func extractBearerEnvVar(headers map[string]string) (string, error) {
	var bearerValue string
	for key, value := range headers {
		if strings.EqualFold(key, "Authorization") {
			bearerValue = value
			continue
		}
		return "", fmt.Errorf("unsupported header %s for codex http server", key)
	}
	if bearerValue == "" {
		return "", nil
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(bearerValue, prefix) {
		return "", fmt.Errorf("authorization header must use Bearer token")
	}
	token := strings.TrimPrefix(bearerValue, prefix)
	if strings.HasPrefix(token, "${") && strings.HasSuffix(token, "}") {
		return strings.TrimSuffix(strings.TrimPrefix(token, "${"), "}"), nil
	}
	return "", fmt.Errorf("authorization header must use env var placeholder")
}

func tomlStringArray(values []string) string {
	escaped := make([]string, 0, len(values))
	for _, value := range values {
		escaped = append(escaped, fmt.Sprintf("%q", value))
	}
	return "[" + strings.Join(escaped, ", ") + "]"
}

func tomlInlineTable(values map[string]string) string {
	if len(values) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s = %q", key, values[key]))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func buildCodexRules(project *config.ProjectConfig) string {
	var builder strings.Builder
	builder.WriteString("# GENERATED FILE\n")
	builder.WriteString("# Source: .agent-layer/commands.allow\n")
	builder.WriteString("# Regenerate: ./al sync\n")
	builder.WriteString("\n")

	approvals := projection.BuildApprovals(project.Config, project.CommandsAllow)
	if !approvals.AllowCommands {
		return builder.String()
	}

	for _, cmd := range approvals.Commands {
		fields := strings.Fields(cmd)
		if len(fields) == 0 {
			continue
		}
		parts := make([]string, 0, len(fields))
		for _, field := range fields {
			parts = append(parts, fmt.Sprintf("%q", field))
		}
		builder.WriteString(fmt.Sprintf(
			"prefix_rule(pattern=[%s], decision=\"allow\", justification=\"agent-layer allowlist\")\n",
			strings.Join(parts, ", "),
		))
	}

	return builder.String()
}
