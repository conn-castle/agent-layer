package sync

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
)

const codexHeader = `# GENERATED FILE — MAY CONTAIN SECRETS
# This file is gitignored. Do not commit or share it.
# Source: .agent-layer/config.toml
# Regenerate: al sync

`

// WriteCodexConfig generates .codex/config.toml.
func WriteCodexConfig(sys System, root string, project *config.ProjectConfig) error {
	content, err := buildCodexConfig(project)
	if err != nil {
		return err
	}

	codexDir := filepath.Join(root, ".codex")
	if err := sys.MkdirAll(codexDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, codexDir, err)
	}

	path := filepath.Join(codexDir, "config.toml")
	if err := sys.WriteFileAtomic(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}

	return nil
}

// WriteCodexRules generates .codex/rules/default.rules.
func WriteCodexRules(sys System, root string, project *config.ProjectConfig) error {
	content := buildCodexRules(project)
	rulesDir := filepath.Join(root, ".codex", "rules")
	if err := sys.MkdirAll(rulesDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, rulesDir, err)
	}
	path := filepath.Join(rulesDir, "default.rules")
	if err := sys.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}
	return nil
}

func buildCodexConfig(project *config.ProjectConfig) (string, error) {
	var builder strings.Builder
	builder.WriteString(codexHeader)

	if project.Config.Agents.Codex.Model != "" && !config.HasAgentSpecificKey(project.Config.Agents.Codex.AgentSpecific, "model") {
		fmt.Fprintf(&builder, "model = %q\n", project.Config.Agents.Codex.Model)
	}
	if project.Config.Agents.Codex.ReasoningEffort != "" && !config.HasAgentSpecificKey(project.Config.Agents.Codex.AgentSpecific, "model_reasoning_effort") {
		fmt.Fprintf(&builder, "model_reasoning_effort = %q\n", project.Config.Agents.Codex.ReasoningEffort)
	}
	if project.Config.Approvals.Mode == "yolo" {
		if !config.HasAgentSpecificKey(project.Config.Agents.Codex.AgentSpecific, "approval_policy") {
			builder.WriteString("approval_policy = \"never\"\n")
		}
		if !config.HasAgentSpecificKey(project.Config.Agents.Codex.AgentSpecific, "sandbox_mode") {
			builder.WriteString("sandbox_mode = \"danger-full-access\"\n")
		}
		if !config.HasAgentSpecificKey(project.Config.Agents.Codex.AgentSpecific, "web_search") {
			builder.WriteString("web_search = \"live\"\n")
		}
	}

	// Write agent-specific root keys/tables before managed MCP tables so any
	// scalar overrides remain at the TOML root.
	if err := appendCodexAgentSpecific(&builder, project.Config.Agents.Codex.AgentSpecific); err != nil {
		return "", err
	}

	if !config.HasAgentSpecificKey(project.Config.Agents.Codex.AgentSpecific, "mcp_servers") {
		// Use placeholder syntax for initial resolution (needed for bearer_token_env_var extraction).
		resolved, err := projection.ResolveMCPServers(
			project.Config.MCP.Servers,
			project.Env,
			"codex",
			projection.ClientPlaceholderResolver("${%s}"),
		)
		if err != nil {
			return "", err
		}

		for i, server := range resolved {
			if i > 0 {
				builder.WriteString("\n")
			}
			fmt.Fprintf(&builder, "[mcp_servers.%q]\n", server.ID)
			switch server.Transport {
			case config.TransportHTTP:
				if err := writeCodexHTTPServer(&builder, server, project.Env); err != nil {
					return "", err
				}
			case config.TransportStdio:
				if err := writeCodexStdioServer(&builder, server, project.Env); err != nil {
					return "", err
				}
			default:
				return "", fmt.Errorf(messages.MCPServerUnsupportedTransportFmt, server.ID, server.Transport)
			}
		}
	}

	return builder.String(), nil
}

func writeCodexHTTPServer(builder *strings.Builder, server projection.ResolvedMCPServer, env map[string]string) error {
	if len(server.Headers) > 0 {
		headerSpec, err := splitCodexHeaders(server.Headers)
		if err != nil {
			return fmt.Errorf(messages.SyncMCPServerErrorFmt, server.ID, err)
		}
		if headerSpec.BearerTokenEnvVar != "" {
			fmt.Fprintf(builder, "bearer_token_env_var = %q\n", headerSpec.BearerTokenEnvVar)
		}
		if len(headerSpec.EnvHeaders) > 0 {
			fmt.Fprintf(builder, "env_http_headers = %s\n", tomlInlineTable(headerSpec.EnvHeaders))
		}
		if len(headerSpec.HTTPHeaders) > 0 {
			fmt.Fprintf(builder, "http_headers = %s\n", tomlInlineTable(headerSpec.HTTPHeaders))
		}
	}
	// Resolve actual values in the URL (Codex doesn't support ${VAR} placeholders in URLs).
	resolvedURL, err := config.SubstituteEnvVars(server.URL, env)
	if err != nil {
		return fmt.Errorf(messages.MCPServerURLFmt, server.ID, err)
	}
	fmt.Fprintf(builder, "url = %q\n", resolvedURL)
	return nil
}

func writeCodexStdioServer(builder *strings.Builder, server projection.ResolvedMCPServer, env map[string]string) error {
	// Resolve actual values in command (Codex doesn't support ${VAR} placeholders).
	resolvedCommand, err := config.SubstituteEnvVars(server.Command, env)
	if err != nil {
		return fmt.Errorf(messages.MCPServerCommandFmt, server.ID, err)
	}
	fmt.Fprintf(builder, "command = %q\n", resolvedCommand)

	if len(server.Args) > 0 {
		resolvedArgs := make([]string, 0, len(server.Args))
		for _, arg := range server.Args {
			resolvedArg, err := config.SubstituteEnvVars(arg, env)
			if err != nil {
				return fmt.Errorf(messages.SyncMCPServerArgFailedFmt, server.ID, err)
			}
			resolvedArgs = append(resolvedArgs, resolvedArg)
		}
		fmt.Fprintf(builder, "args = %s\n", tomlStringArray(resolvedArgs))
	}

	if len(server.Env) > 0 {
		// Resolve actual values in env vars (Codex doesn't support ${VAR} placeholders).
		resolvedEnv := make(map[string]string, len(server.Env))
		for key, value := range server.Env {
			resolvedValue, err := config.SubstituteEnvVars(value, env)
			if err != nil {
				return fmt.Errorf(messages.MCPServerEnvFmt, server.ID, key, err)
			}
			resolvedEnv[key] = resolvedValue
		}
		fmt.Fprintf(builder, "env = %s\n", tomlInlineTable(resolvedEnv))
	}

	return nil
}

type codexHeaderSpec struct {
	BearerTokenEnvVar string
	EnvHeaders        map[string]string
	HTTPHeaders       map[string]string
}

// splitCodexHeaders classifies headers into Codex-supported groups.
// headers are raw header values with ${VAR} placeholders preserved.
func splitCodexHeaders(headers map[string]string) (codexHeaderSpec, error) {
	spec := codexHeaderSpec{
		EnvHeaders:  make(map[string]string),
		HTTPHeaders: make(map[string]string),
	}
	for key, value := range headers {
		if strings.EqualFold(key, "Authorization") {
			if envVar, ok := extractBearerEnvPlaceholder(value); ok {
				spec.BearerTokenEnvVar = envVar
				continue
			}
			if envVar, ok := extractExactEnvPlaceholder(value); ok {
				spec.EnvHeaders[key] = envVar
				continue
			}
			if !hasEnvPlaceholders(value) {
				spec.HTTPHeaders[key] = value
				continue
			}
			return codexHeaderSpec{}, fmt.Errorf(messages.SyncCodexAuthorizationPlaceholderUnsupportedFmt)
		}

		if envVar, ok := extractExactEnvPlaceholder(value); ok {
			spec.EnvHeaders[key] = envVar
			continue
		}
		if !hasEnvPlaceholders(value) {
			spec.HTTPHeaders[key] = value
			continue
		}
		return codexHeaderSpec{}, fmt.Errorf(messages.SyncCodexHeaderPlaceholderUnsupportedFmt, key)
	}
	if len(spec.EnvHeaders) == 0 {
		spec.EnvHeaders = nil
	}
	if len(spec.HTTPHeaders) == 0 {
		spec.HTTPHeaders = nil
	}
	return spec, nil
}

func extractExactEnvPlaceholder(value string) (string, bool) {
	names := config.ExtractEnvVarNames(value)
	if len(names) != 1 {
		return "", false
	}
	placeholder := fmt.Sprintf("${%s}", names[0])
	if value != placeholder {
		return "", false
	}
	return names[0], true
}

func extractBearerEnvPlaceholder(value string) (string, bool) {
	const prefix = "Bearer "
	if len(value) <= len(prefix) {
		return "", false
	}
	if !strings.EqualFold(value[:len(prefix)], prefix) {
		return "", false
	}
	tokenPart := value[len(prefix):]
	return extractExactEnvPlaceholder(tokenPart)
}

func hasEnvPlaceholders(value string) bool {
	return len(config.ExtractEnvVarNames(value)) > 0
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
	builder.WriteString("# Regenerate: al sync\n")
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
		fmt.Fprintf(&builder,
			"prefix_rule(pattern=[%s], decision=\"allow\", justification=\"agent-layer allowlist\")\n",
			strings.Join(parts, ", "),
		)
	}

	return builder.String()
}
