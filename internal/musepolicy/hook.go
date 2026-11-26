package musepolicy

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/projection"
)

const (
	policyAllow       = "allow"
	permissionRequest = "PermissionRequest"
)

var museIdentifier = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// MCPPrefixes validates unambiguous native tool namespaces before granting calls.
func MCPPrefixes(cfg config.Config) ([]string, error) {
	return MCPPrefixesForIDs(projection.EffectiveServerIDs(cfg, projection.ClientMuse))
}

// MCPPrefixesForIDs validates unambiguous native Muse tool namespaces for an
// explicit list of MCP server IDs.
func MCPPrefixesForIDs(ids []string) ([]string, error) {
	seen := make(map[string]string)
	var prefixes []string
	for _, id := range ids {
		normalized := strings.ReplaceAll(id, "-", "_")
		if !museIdentifier.MatchString(id) || (strings.Contains(normalized, "__") || strings.HasPrefix(normalized, "_") || strings.HasSuffix(normalized, "_")) {
			return nil, fmt.Errorf("muse MCP approval server ID %q must contain ASCII letters, digits, hyphens or underscores without adjacent, leading, or trailing normalized underscores", id)
		}
		if other, ok := seen[normalized]; ok {
			return nil, fmt.Errorf("muse MCP approval server IDs %q and %q share native namespace %q; rename one server", other, id, normalized)
		}
		seen[normalized] = id
		prefixes = append(prefixes, "mcp__"+normalized+"__")
	}
	return prefixes, nil
}

// HandleMCP reloads canonical policy for every request, including in sessions
// whose native hook registration predates a sync. Native denies take precedence.
func HandleMCP(root string, input io.Reader, output io.Writer) error {
	var event struct {
		Event     string `json:"hook_event_name"`
		Tool      string `json:"tool_name"`
		CWD       string `json:"cwd"`
		SessionID string `json:"session_id"`
	}
	dec := json.NewDecoder(input)
	if err := dec.Decode(&event); err != nil {
		return fmt.Errorf("decode Muse permission request: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("muse permission request must contain one JSON object")
	}
	response := map[string]any{}
	if event.Event != permissionRequest || !strings.HasPrefix(event.Tool, "mcp__") {
		return json.NewEncoder(output).Encode(response)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	cwd, err := filepath.EvalSymlinks(event.CWD)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(canonical, cwd)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(canonical) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return json.NewEncoder(output).Encode(response)
	}
	data, err := os.ReadFile(filepath.Join(canonical, ".agent-layer", "config.toml"))
	if err != nil {
		return fmt.Errorf("read current Muse approval configuration: %w", err)
	}
	cfg, err := config.ParseConfig(data, "Muse approval configuration")
	if err != nil {
		return err
	}
	if !config.IsAgentEnabled(cfg.Agents.Muse.Enabled) {
		return json.NewEncoder(output).Encode(response)
	}
	allowDecision := func() map[string]any {
		return map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": permissionRequest, "decision": map[string]any{"behavior": policyAllow}}}
	}
	if projection.BuildApprovals(*cfg, nil).AllowMCP {
		prefixes, err := MCPPrefixes(*cfg)
		if err != nil {
			return err
		}
		if matchesAllowedMCPTool(event.Tool, prefixes) {
			return json.NewEncoder(output).Encode(allowDecision())
		}
	}
	if strings.TrimSpace(event.SessionID) != "" {
		grant, ok, err := readSessionMCPGrant(canonical, strings.TrimSpace(event.SessionID))
		if err != nil {
			return err
		}
		if ok && len(grant.ServerIDs) > 0 {
			prefixes, err := MCPPrefixesForIDs(grant.ServerIDs)
			if err != nil {
				return err
			}
			if matchesAllowedMCPTool(event.Tool, prefixes) {
				return json.NewEncoder(output).Encode(allowDecision())
			}
		}
	}
	return json.NewEncoder(output).Encode(response)
}

func matchesAllowedMCPTool(toolName string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(toolName, prefix) && len(toolName) > len(prefix) {
			tool := strings.TrimPrefix(toolName, prefix)
			if strings.HasPrefix(tool, "_") || strings.Contains(tool, "__") {
				return false
			}
			return true
		}
	}
	return false
}
