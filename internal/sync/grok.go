package sync

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
)

const (
	grokMCPServersKey       = "mcp_servers"
	grokGeneratedMarker     = "# GENERATED FILE\n"
	grokLegacyGeneratedLine = "# GENERATED FILE — MAY CONTAIN SECRETS\n"
	grokHeader              = grokGeneratedMarker + `# MAY CONTAIN SECRETS. This file is gitignored; do not commit or share it.
# Source: .agent-layer/config.toml
# Regenerate: al sync

`
)

// writeGrokConfig generates .grok/config.toml for Grok Build CLI.
func writeGrokConfig(sys System, root string, project *config.ProjectConfig) error {
	content, err := buildGrokConfig(project)
	if err != nil {
		return err
	}

	grokDir := filepath.Join(root, ".grok")
	if err := ensureGrokConfigTarget(sys, grokDir, filepath.Join(grokDir, "config.toml")); err != nil {
		return err
	}
	if err := sys.MkdirAll(grokDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, grokDir, err)
	}

	path := filepath.Join(grokDir, "config.toml")
	if err := sys.WriteFileAtomic(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}

	return nil
}

func ensureGrokConfigTarget(sys System, grokDir, path string) error {
	if info, err := sys.Lstat(grokDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("grok config directory must be a real directory: %s", grokDir)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf(messages.InstallFailedStatFmt, grokDir, err)
	}

	info, err := sys.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf(messages.InstallFailedStatFmt, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("grok config target must be a regular file, not a symlink or special file: %s", path)
	}
	existing, err := sys.ReadFile(path)
	if err != nil {
		return fmt.Errorf(messages.SyncReadFailedFmt, path, err)
	}
	if !grokConfigIsManaged(existing) {
		return fmt.Errorf("refusing to overwrite user-owned Grok config %s; move managed settings into .agent-layer/config.toml or agents.grok.agent_specific first", path)
	}
	return nil
}

func grokConfigIsManaged(data []byte) bool {
	return bytes.HasPrefix(data, []byte(grokGeneratedMarker)) ||
		bytes.HasPrefix(data, []byte(grokLegacyGeneratedLine))
}

func buildGrokConfig(project *config.ProjectConfig) (string, error) {
	var builder strings.Builder
	builder.WriteString(grokHeader)

	// Grok project passthrough is validated as plugin-only before sync.
	agentSpecific := project.Config.Agents.Grok.AgentSpecific
	if len(agentSpecific) > 0 {
		var buf bytes.Buffer
		encoder := toml.NewEncoder(&buf)
		if err := encoder.Encode(agentSpecific); err != nil {
			return "", fmt.Errorf("failed to encode agents.grok.agent_specific: %w", err)
		}
		encoded := strings.TrimSpace(buf.String())
		if encoded != "" {
			builder.WriteString(encoded)
			builder.WriteString("\n\n")
		}
	}

	writeGrokPermission(&builder, project)

	resolved, err := projection.EffectiveMCPServers(
		project.Config,
		project.Env,
		projection.ClientGrok,
		projection.ClientPlaceholderResolver("${%s}"),
	)
	if err != nil {
		return "", err
	}

	for i, server := range resolved {
		if i > 0 {
			builder.WriteString("\n")
		}
		fmt.Fprintf(&builder, "[%s.%q]\n", grokMCPServersKey, server.ID)
		switch server.Transport {
		case config.TransportHTTP:
			writeGrokHTTPServer(&builder, server)
		case config.TransportStdio:
			writeGrokStdioServer(&builder, server)
		default:
			return "", fmt.Errorf(messages.MCPServerUnsupportedTransportFmt, server.ID, server.Transport)
		}
	}

	return builder.String(), nil
}

func writeGrokHTTPServer(builder *strings.Builder, server projection.ResolvedMCPServer) {
	fmt.Fprintf(builder, "url = %q\n", server.URL)
	if server.HTTPTransport == "sse" {
		builder.WriteString("type = \"sse\"\n")
	}
	if len(server.Headers) > 0 {
		fmt.Fprintf(builder, "headers = %s\n", tomlInlineTable(server.Headers))
	}
}

func writeGrokStdioServer(builder *strings.Builder, server projection.ResolvedMCPServer) {
	fmt.Fprintf(builder, "command = %q\n", server.Command)
	if len(server.Args) > 0 {
		fmt.Fprintf(builder, "args = %s\n", tomlStringArray(server.Args))
	}
	if len(server.Env) > 0 {
		fmt.Fprintf(builder, "env = %s\n", tomlInlineTable(server.Env))
	}
	if server.ToolTimeoutSeconds > 0 {
		fmt.Fprintf(builder, "tool_timeout_sec = %d\n", server.ToolTimeoutSeconds)
	}
}

func writeGrokPermission(builder *strings.Builder, project *config.ProjectConfig) {
	rules := projection.ClaudeAllowRules(
		project.Config,
		project.CommandsAllow,
		projection.EffectiveServerIDs(project.Config, projection.ClientGrok),
	)
	if len(rules) == 0 {
		return
	}
	builder.WriteString("[permission]\n")
	fmt.Fprintf(builder, "allow = %s\n\n", tomlStringArray(rules))
}

// cleanGrokOutputs removes Grok artifacts generated by previous syncs.
// Call this when agents.grok is disabled so stale config does not persist.
func cleanGrokOutputs(sys System, root string) error {
	grokDir := filepath.Join(root, ".grok")
	if info, err := sys.Lstat(grokDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("grok config directory must be a real directory: %s", grokDir)
		}
	} else if os.IsNotExist(err) {
		return cleanGrokChimeHook(sys, root)
	} else {
		return fmt.Errorf(messages.InstallFailedStatFmt, grokDir, err)
	}

	path := filepath.Join(grokDir, "config.toml")
	info, err := sys.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("grok config target must be a regular file, not a symlink or special file: %s", path)
		}
		data, readErr := sys.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf(messages.SyncReadFailedFmt, path, readErr)
		}
		if grokConfigIsManaged(data) {
			if err := sys.Remove(path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf(messages.SyncRemoveFailedFmt, path, err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf(messages.InstallFailedStatFmt, path, err)
	}
	return cleanGrokChimeHook(sys, root)
}
