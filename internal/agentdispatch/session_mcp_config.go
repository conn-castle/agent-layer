package agentdispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/projection"
)

const (
	maxSessionMCPConfigBytes   = 1 << 20 // 1 MiB
	maxSessionMCPMergedBytes   = 2 * maxSessionMCPConfigBytes
	maxSessionMCPServerNameLen = 128
)

var (
	sessionMCPNamePattern   = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	sessionMCPEnvKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

type rawSessionMCPConfigDoc struct {
	MCPServers map[string]rawSessionMCPServer `json:"mcpServers"`
}

type rawSessionMCPServer struct {
	Type    *string           `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// resolveSessionMCPServers loads and merges --mcp-config files with any
// programmatically supplied SessionMCPServers, validating syntax and rejecting
// duplicate names before launch.
func resolveSessionMCPServers(configPaths []string, direct []SessionMCPServer) ([]SessionMCPServer, error) {
	if len(configPaths) == 0 && len(direct) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{})
	var merged []SessionMCPServer
	cumulativeBytes := 2 // JSON array brackets "[]"
	appendBounded := func(server SessionMCPServer) error {
		encoded, err := json.Marshal(server)
		if err != nil {
			return wrapExitError(ExitUsage, "encode session MCP server", err)
		}
		entrySize := len(encoded)
		if len(merged) > 0 {
			entrySize++ // comma separator
		}
		if cumulativeBytes+entrySize > maxSessionMCPMergedBytes {
			return exitError(ExitUsage, fmt.Sprintf("merged session MCP configuration exceeds maximum size of %d bytes", maxSessionMCPMergedBytes))
		}
		cumulativeBytes += entrySize
		merged = append(merged, server)
		return nil
	}
	for _, rawPath := range configPaths {
		if strings.TrimSpace(rawPath) == "" {
			return nil, exitError(ExitUsage, "--mcp-config path must not be empty")
		}
		parsed, err := loadSessionMCPConfigFile(fsutil.ResolvePaddedPath(rawPath))
		if err != nil {
			return nil, err
		}
		for _, server := range parsed {
			if _, exists := seen[server.Name]; exists {
				return nil, exitError(ExitUsage, fmt.Sprintf("duplicate session MCP server %q across --mcp-config files", server.Name))
			}
			seen[server.Name] = struct{}{}
			if err := appendBounded(server); err != nil {
				return nil, err
			}
		}
	}
	for _, server := range direct {
		if err := validateSessionMCPServer(server); err != nil {
			return nil, err
		}
		if _, exists := seen[server.Name]; exists {
			return nil, exitError(ExitUsage, fmt.Sprintf("duplicate session MCP server %q", server.Name))
		}
		seen[server.Name] = struct{}{}
		if err := appendBounded(server); err != nil {
			return nil, err
		}
	}
	normalized := normalizeSessionMCPServers(merged)
	if encoded, err := json.Marshal(normalized); err != nil {
		return nil, wrapExitError(ExitUsage, "encode merged session MCP configuration", err)
	} else if len(encoded) > maxSessionMCPMergedBytes {
		return nil, exitError(ExitUsage, fmt.Sprintf("merged session MCP configuration exceeds maximum size of %d bytes", maxSessionMCPMergedBytes))
	}
	return normalized, nil
}

func loadSessionMCPConfigFile(path string) ([]SessionMCPServer, error) {
	file, err := fsutil.OpenRegularFile(path)
	if err != nil {
		return nil, wrapExitError(ExitUsage, fmt.Sprintf("read --mcp-config %q", path), err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxSessionMCPConfigBytes+1))
	if err != nil {
		return nil, wrapExitError(ExitUsage, fmt.Sprintf("read --mcp-config %q", path), err)
	}
	if len(data) > maxSessionMCPConfigBytes {
		return nil, exitError(ExitUsage, fmt.Sprintf("--mcp-config %q exceeds maximum size of %d bytes", path, maxSessionMCPConfigBytes))
	}
	servers, err := parseSessionMCPConfigJSON(data)
	if err != nil {
		return nil, wrapExitError(ExitUsage, fmt.Sprintf("invalid --mcp-config %q", path), err)
	}
	return servers, nil
}

func parseSessionMCPConfigJSON(data []byte) ([]SessionMCPServer, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("--mcp-config must be valid UTF-8")
	}
	if hasUnpairedJSONSurrogate(data) {
		return nil, errors.New("--mcp-config must not contain unpaired UTF-16 surrogate escapes")
	}
	if err := validateStrictJSONObject(data); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var doc rawSessionMCPConfigDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	if len(doc.MCPServers) == 0 {
		return nil, errors.New(`"mcpServers" must be a non-empty object of stdio MCP server definitions`)
	}
	servers := make([]SessionMCPServer, 0, len(doc.MCPServers))
	for name, raw := range doc.MCPServers {
		if raw.Type != nil && *raw.Type != config.TransportStdio {
			return nil, fmt.Errorf("server %q: unsupported transport %q (session-scoped MCP servers support only %q)", name, *raw.Type, config.TransportStdio)
		}
		server := SessionMCPServer{
			Name:    name,
			Command: raw.Command,
			Args:    raw.Args,
			Env:     raw.Env,
		}
		if err := validateSessionMCPServer(server); err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return normalizeSessionMCPServers(servers), nil
}

// validateStrictJSONObject ensures data contains a single JSON object with no
// duplicate keys at any nesting level, exact case-sensitive schema keys, no
// null values, and no trailing data.
func validateStrictJSONObject(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("expected JSON object: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return errors.New("top-level JSON value must be an object")
	}
	if err := validateStrictJSONValue(dec, tok, 0); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("must contain a single JSON object with no trailing data")
	}
	return nil
}

func validateStrictJSONValue(dec *json.Decoder, first json.Token, depth int) error {
	if first == nil {
		return errors.New("null values are not allowed in --mcp-config")
	}
	delim, ok := first.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyTok.(string)
			if !ok {
				return errors.New("JSON object key must be a string")
			}
			switch depth {
			case 0:
				if key != jsonMCPServersKey {
					return fmt.Errorf("unknown top-level key %q (expected %q)", key, jsonMCPServersKey)
				}
			case 2:
				switch key {
				case jsonTypeKey, jsonCommandKey, "args", "env":
				default:
					return fmt.Errorf("unknown server key %q (expected type, command, args, or env)", key)
				}
			}
			if _, dup := seen[key]; dup {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = struct{}{}
			valTok, err := dec.Token()
			if err != nil {
				return err
			}
			if err := validateStrictJSONValue(dec, valTok, depth+1); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := end.(json.Delim); !ok || d != '}' {
			return errors.New("malformed JSON object")
		}
	case '[':
		for dec.More() {
			elemTok, err := dec.Token()
			if err != nil {
				return err
			}
			if err := validateStrictJSONValue(dec, elemTok, depth+1); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := end.(json.Delim); !ok || d != ']' {
			return errors.New("malformed JSON array")
		}
	}
	return nil
}

func validateSessionMCPServerName(name string) error {
	if name == "" {
		return exitError(ExitUsage, "session MCP server name must not be empty")
	}
	if len(name) > maxSessionMCPServerNameLen {
		return exitError(ExitUsage, fmt.Sprintf("session MCP server name %q exceeds maximum length of %d characters", name, maxSessionMCPServerNameLen))
	}
	normalized := strings.ReplaceAll(name, "-", "_")
	if !sessionMCPNamePattern.MatchString(name) || strings.HasPrefix(normalized, "_") || strings.HasSuffix(normalized, "_") || strings.Contains(normalized, "__") {
		return exitError(ExitUsage, fmt.Sprintf("session MCP server name %q must contain only ASCII letters, digits, hyphens, or underscores without leading, trailing, or adjacent hyphens/underscores", name))
	}
	return nil
}

func hasUnpairedJSONSurrogate(data []byte) bool {
	inString := false
	for i := 0; i < len(data); i++ {
		b := data[i]
		if !inString {
			if b == '"' {
				inString = true
			}
			continue
		}
		if b == '"' {
			inString = false
			continue
		}
		if b != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return false
		}
		if data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return false
		}
		code, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			continue
		}
		i += 4
		switch {
		case code >= 0xD800 && code <= 0xDBFF:
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return true
			}
			low, lowErr := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
			if lowErr != nil || low < 0xDC00 || low > 0xDFFF {
				return true
			}
			i += 6
		case code >= 0xDC00 && code <= 0xDFFF:
			return true
		}
	}
	return false
}

func validateSessionMCPServer(server SessionMCPServer) error {
	if err := validateSessionMCPServerName(server.Name); err != nil {
		return err
	}
	if !utf8.ValidString(server.Command) {
		return exitError(ExitUsage, fmt.Sprintf("session MCP server %q command must be valid UTF-8", server.Name))
	}
	if strings.TrimSpace(server.Command) == "" {
		return exitError(ExitUsage, fmt.Sprintf("session MCP server %q requires a non-empty command", server.Name))
	}
	if strings.ContainsRune(server.Command, 0) || strings.IndexFunc(server.Command, unicode.IsControl) >= 0 {
		return exitError(ExitUsage, fmt.Sprintf("session MCP server %q command must not contain control or NUL characters", server.Name))
	}
	for i, arg := range server.Args {
		if !utf8.ValidString(arg) {
			return exitError(ExitUsage, fmt.Sprintf("session MCP server %q arg[%d] must be valid UTF-8", server.Name, i))
		}
		if strings.ContainsRune(arg, 0) {
			return exitError(ExitUsage, fmt.Sprintf("session MCP server %q arg[%d] must not contain NUL bytes", server.Name, i))
		}
	}
	for key, value := range server.Env {
		if !sessionMCPEnvKeyPattern.MatchString(key) {
			return exitError(ExitUsage, fmt.Sprintf("session MCP server %q has invalid environment variable name %q", server.Name, key))
		}
		if !utf8.ValidString(value) {
			return exitError(ExitUsage, fmt.Sprintf("session MCP server %q env[%q] must be valid UTF-8", server.Name, key))
		}
		if strings.ContainsRune(value, 0) {
			return exitError(ExitUsage, fmt.Sprintf("session MCP server %q env[%q] must not contain NUL bytes", server.Name, key))
		}
	}
	return nil
}

func normalizeSessionMCPServers(servers []SessionMCPServer) []SessionMCPServer {
	if len(servers) == 0 {
		return nil
	}
	out := make([]SessionMCPServer, len(servers))
	for i, server := range servers {
		cloned := SessionMCPServer{
			Name:    server.Name,
			Command: server.Command,
		}
		if len(server.Args) > 0 {
			cloned.Args = slices.Clone(server.Args)
		}
		if len(server.Env) > 0 {
			cloned.Env = maps.Clone(server.Env)
		}
		out[i] = cloned
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func sessionMCPServerNames(servers []SessionMCPServer) []string {
	if len(servers) == 0 {
		return nil
	}
	names := make([]string, len(servers))
	for i, server := range servers {
		names[i] = server.Name
	}
	sort.Strings(names)
	return names
}

// validateSessionMCPServersForProject enforces capability discovery, server
// schema invariants, reserved/project collisions, and provider approval policy
// compatibility before launching a provider or claiming a reservation.
func validateSessionMCPServersForProject(project *config.ProjectConfig, target targetMeta, servers []SessionMCPServer) error {
	if len(servers) == 0 {
		return nil
	}
	if !target.SessionMCPServersSupported {
		reason := target.SessionMCPServersReason
		if reason == "" {
			reason = "provider does not support isolated session-scoped MCP servers"
		}
		return exitError(ExitUsage, fmt.Sprintf("%s does not support session-scoped MCP servers: %s", target.Name, reason))
	}
	seenExact := make(map[string]struct{}, len(servers))
	seenNormalized := make(map[string]string, len(servers))
	for _, server := range servers {
		if err := validateSessionMCPServer(server); err != nil {
			return err
		}
		if _, dup := seenExact[server.Name]; dup {
			return exitError(ExitUsage, fmt.Sprintf("duplicate session MCP server %q", server.Name))
		}
		seenExact[server.Name] = struct{}{}
		norm := strings.ReplaceAll(server.Name, "-", "_")
		if other, dup := seenNormalized[norm]; dup {
			return exitError(ExitUsage, fmt.Sprintf("session MCP servers %q and %q share normalized namespace %q; rename one server", other, server.Name, norm))
		}
		seenNormalized[norm] = server.Name
	}

	builtInNormalized := strings.ReplaceAll(projection.BuiltInDispatchServerID, "-", "_")
	projectIDs := make(map[string]struct{}, len(project.Config.MCP.Servers))
	for _, srv := range project.Config.MCP.Servers {
		projectIDs[srv.ID] = struct{}{}
	}
	codexPassthroughIDs := make(map[string]struct{})
	if target.Name == AgentCodex {
		if raw, ok := project.Config.Agents.Codex.AgentSpecific[config.CodexMCPServersKey].(map[string]any); ok {
			for id := range raw {
				codexPassthroughIDs[id] = struct{}{}
			}
		}
	}
	claudeProjectRules := make(map[string]string)
	if target.Name == AgentClaude {
		for _, id := range projection.EffectiveServerIDs(project.Config, projection.ClientClaude) {
			claudeProjectRules[projection.ClaudeMCPRule(id)] = id
		}
	}
	museProjectNormalized := make(map[string]string)
	if target.Name == AgentMuse {
		for _, id := range projection.EffectiveServerIDs(project.Config, projection.ClientMuse) {
			museProjectNormalized[strings.ReplaceAll(id, "-", "_")] = id
		}
	}

	for _, server := range servers {
		if server.Name == projection.BuiltInDispatchServerID {
			return exitError(ExitUsage, fmt.Sprintf("session MCP server name %q is reserved for Agent Dispatch", server.Name))
		}
		if strings.ReplaceAll(server.Name, "-", "_") == builtInNormalized {
			return exitError(ExitUsage, fmt.Sprintf("session MCP server name %q collides with reserved server %q", server.Name, projection.BuiltInDispatchServerID))
		}
		if _, exists := projectIDs[server.Name]; exists {
			return exitError(ExitUsage, fmt.Sprintf("session MCP server %q collides with existing project-configured MCP server %q", server.Name, server.Name))
		}
		if _, exists := codexPassthroughIDs[server.Name]; exists {
			return exitError(ExitUsage, fmt.Sprintf("session MCP server %q collides with agents.codex.agent_specific.mcp_servers.%s", server.Name, server.Name))
		}
		switch target.Name {
		case AgentClaude:
			rule := projection.ClaudeMCPRule(server.Name)
			if existing, exists := claudeProjectRules[rule]; exists {
				return exitError(ExitUsage, fmt.Sprintf("session MCP server %q collides with Claude tool namespace of project MCP server %q (%s)", server.Name, existing, rule))
			}
			if denyRule, blocked := claudePermissionListBlocksSessionMCP(project.Config.Agents.Claude.AgentSpecific, "deny", server.Name); blocked {
				return exitError(ExitConfig, fmt.Sprintf("cannot attach session MCP server %q on claude: agents.claude.agent_specific.permissions.deny contains %q which blocks its tools", server.Name, denyRule))
			}
			if project.Config.Approvals.Mode != config.ApprovalModeYOLO {
				if askRule, blocked := claudePermissionListBlocksSessionMCP(project.Config.Agents.Claude.AgentSpecific, "ask", server.Name); blocked {
					return exitError(ExitConfig, fmt.Sprintf("cannot attach session MCP server %q on claude: agents.claude.agent_specific.permissions.ask contains %q which blocks non-interactive tool approval", server.Name, askRule))
				}
			}
		case AgentMuse:
			norm := strings.ReplaceAll(server.Name, "-", "_")
			if existing, exists := museProjectNormalized[norm]; exists {
				return exitError(ExitUsage, fmt.Sprintf("session MCP server %q collides with Muse tool namespace of project MCP server %q (mcp__%s__*)", server.Name, existing, norm))
			}
		case AgentGrok:
			// Grok prefixes MCP tools as "<server>__<tool>". Reject a session
			// server whose rendered prefix overlaps a project server prefix.
			for existing := range projectIDs {
				if strings.HasPrefix(existing+"__", server.Name+"__") || strings.HasPrefix(server.Name+"__", existing+"__") {
					return exitError(ExitUsage, fmt.Sprintf("session MCP server %q has an ambiguous Grok MCP tool prefix with project MCP server %q; rename the session server", server.Name, existing))
				}
			}
		case AgentAntigravity:
			// Antigravity eager MCP tools use a single-underscore separator
			// ("mcp_<server>_<tool>"), so a server name that is an underscore
			// prefix of another server name creates an ambiguous approval namespace.
			agyExisting := make(map[string]struct{}, len(projectIDs)+1)
			for existing := range projectIDs {
				agyExisting[existing] = struct{}{}
			}
			for _, id := range projection.EffectiveServerIDs(project.Config, projection.ClientAntigravity) {
				agyExisting[id] = struct{}{}
			}
			for existing := range agyExisting {
				if antigravityNamespaceAmbiguous(server.Name, existing) {
					return exitError(ExitUsage, fmt.Sprintf("session MCP server %q has an ambiguous Antigravity MCP tool prefix with project MCP server %q; rename the session server", server.Name, existing))
				}
			}
			if antigravityNamespaceAmbiguous(server.Name, projection.BuiltInDispatchServerID) {
				return exitError(ExitUsage, fmt.Sprintf("session MCP server %q has an ambiguous Antigravity MCP tool prefix with reserved server %q; rename the session server", server.Name, projection.BuiltInDispatchServerID))
			}
			for _, other := range servers {
				if other.Name != server.Name && antigravityNamespaceAmbiguous(server.Name, other.Name) {
					return exitError(ExitUsage, fmt.Sprintf("session MCP servers %q and %q have ambiguous Antigravity MCP tool prefixes; rename one server", server.Name, other.Name))
				}
			}
		}
	}
	return nil
}

func antigravityNamespaceAmbiguous(a string, b string) bool {
	na := strings.ReplaceAll(a, "-", "_")
	nb := strings.ReplaceAll(b, "-", "_")
	return strings.HasPrefix(a, b+"_") || strings.HasPrefix(b, a+"_") ||
		strings.HasPrefix(na, nb+"_") || strings.HasPrefix(nb, na+"_")
}

func claudePermissionListBlocksSessionMCP(passthrough config.ProviderPassthrough, listKey string, serverName string) (string, bool) {
	permissions, ok := passthrough[claudePermissionsPassthroughKey].(map[string]any)
	if !ok {
		return "", false
	}
	rawList, ok := permissions[listKey]
	if !ok {
		return "", false
	}
	var rules []string
	switch list := rawList.(type) {
	case []string:
		rules = list
	case []any:
		for _, item := range list {
			if value, ok := item.(string); ok {
				rules = append(rules, value)
			}
		}
	}
	rule := projection.ClaudeMCPRule(serverName) // mcp__<normalized>__*
	prefix := strings.TrimSuffix(rule, "*")      // mcp__<normalized>__
	serverExact := strings.TrimSuffix(prefix, "__")
	for _, entry := range rules {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "mcp__*" || trimmed == "mcp__**__" || trimmed == rule || trimmed == serverExact || strings.HasPrefix(trimmed, prefix) {
			return trimmed, true
		}
	}
	return "", false
}
