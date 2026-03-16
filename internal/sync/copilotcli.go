package sync

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
)

// copilotMCPConfig mirrors the Copilot CLI mcp-config.json structure.
type copilotMCPConfig struct {
	Servers OrderedMap[copilotMCPServer] `json:"mcpServers,omitempty"`
}

type copilotMCPServer struct {
	Type    string             `json:"type"`
	Command string             `json:"command,omitempty"`
	Args    []string           `json:"args,omitempty"`
	Env     OrderedMap[string] `json:"env,omitempty"`
	URL     string             `json:"url,omitempty"`
	Headers OrderedMap[string] `json:"headers,omitempty"`
	Tools   []string           `json:"tools,omitempty"`
}

// WriteCopilotMCPConfig generates .copilot/mcp-config.json for GitHub Copilot CLI.
// Unlike Claude's .mcp.json, this does NOT include the internal agent-layer prompt
// server because Copilot CLI already reads AGENTS.md and .github/copilot-instructions.md.
func WriteCopilotMCPConfig(sys System, root string, project *config.ProjectConfig) error {
	cfg, err := buildCopilotMCPConfig(project)
	if err != nil {
		return err
	}

	data, err := sys.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf(messages.SyncMarshalCopilotMCPConfigFailedFmt, err)
	}
	data = append(data, '\n')

	copilotDir := filepath.Join(root, ".copilot")
	if err := sys.MkdirAll(copilotDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, copilotDir, err)
	}

	path := filepath.Join(copilotDir, "mcp-config.json")
	if err := sys.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}

	return nil
}

func buildCopilotMCPConfig(project *config.ProjectConfig) (*copilotMCPConfig, error) {
	cfg := &copilotMCPConfig{
		Servers: make(OrderedMap[copilotMCPServer]),
	}

	resolved, err := projection.ResolveMCPServers(
		project.Config.MCP.Servers,
		project.Env,
		"copilot",
		projection.ClientPlaceholderResolver("${%s}"),
	)
	if err != nil {
		return nil, err
	}

	for _, server := range resolved {
		entry := copilotMCPServer{
			Type:    server.Transport,
			Command: server.Command,
			Args:    server.Args,
			URL:     server.URL,
			Tools:   []string{"*"},
		}
		if len(server.Headers) > 0 {
			headers := make(OrderedMap[string], len(server.Headers))
			for key, value := range server.Headers {
				headers[key] = value
			}
			entry.Headers = headers
		}
		if len(server.Env) > 0 {
			envMap := make(OrderedMap[string], len(server.Env))
			for key, value := range server.Env {
				envMap[key] = value
			}
			entry.Env = envMap
		}
		cfg.Servers[server.ID] = entry
	}

	return cfg, nil
}

// WriteCopilotSkills generates skill files in .github/skills/<name>/SKILL.md.
func WriteCopilotSkills(sys System, root string, skills []config.Skill) error {
	skillsDir := filepath.Join(root, ".github", "skills")
	return writeSkillFiles(sys, skillsDir, skills, buildCopilotSkill)
}

// buildCopilotSkill returns the Copilot CLI SKILL.md content.
// Uses the same format as Codex/Antigravity: YAML frontmatter + body.
func buildCopilotSkill(cmd config.Skill) (string, error) {
	var builder strings.Builder
	frontMatter, err := buildSkillFrontMatter(cmd)
	if err != nil {
		return "", err
	}
	builder.WriteString(frontMatter)
	fmt.Fprintf(&builder, promptHeaderTemplate, generatedSkillSourcePath(cmd))
	if cmd.Body != "" {
		builder.WriteString("\n")
		builder.WriteString(cmd.Body)
		if !strings.HasSuffix(cmd.Body, "\n") {
			builder.WriteString("\n")
		}
	}
	return builder.String(), nil
}
