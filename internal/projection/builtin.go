package projection

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
)

// BuiltInDispatchServerID is the reserved ID of the Agent Layer MCP server that
// exposes the Agent Dispatch tools. It is derived Agent Layer state rather than
// a `[[mcp.servers]]` entry, and config validation keeps the same ID
// unavailable to user-defined servers.
const BuiltInDispatchServerID = "agent-layer"

// Client identifiers used by MCP projection and `mcp.servers[].clients`.
const (
	ClientAntigravity = "antigravity"
	ClientClaude      = "claude"
	ClientCodex       = "codex"
	ClientCopilot     = "copilot"
	ClientGrok        = "grok"
	ClientMuse        = "muse"
	ClientVSCode      = "vscode"
)

const (
	builtInDispatchCommand = "al"
	builtInDispatchSubject = "dispatch"
	builtInDispatchAction  = "mcp-server"
	builtInDispatchShell   = "/bin/sh"
	// The release branch prefers the al entry that sync resolved, exports it as
	// AL_INSTALLED_EXECUTABLE so in-server regeneration records it again, and
	// appends its directory to PATH so GUI and background clients that never
	// inherited the terminal PATH can start the server. Nested al calls find
	// that directory only when no other al precedes it on the client PATH.
	// Clients expand ${...} in argv, so the script avoids it.
	builtInDispatchScript  = `AL_MCP_WORKING_DIR=$PWD; export AL_MCP_WORKING_DIR; cd "$1" || exit; if [ -n "$2" ]; then AL_DEV_EXECUTABLE=$2; AL_DEV_BYPASS_VERSION_DISPATCH=1; AL_DEV_SOURCE_ROOT=$1; export AL_DEV_EXECUTABLE AL_DEV_BYPASS_VERSION_DISPATCH AL_DEV_SOURCE_ROOT; exec "$2" dispatch mcp-server; fi; unset AL_DEV_EXECUTABLE AL_DEV_BYPASS_VERSION_DISPATCH AL_DEV_SOURCE_ROOT; if [ -x "$3" ]; then case ":$PATH:" in *":$4:"*) ;; "::") PATH=$4 ;; *) PATH=$PATH:$4 ;; esac; AL_INSTALLED_EXECUTABLE=$3; export PATH AL_INSTALLED_EXECUTABLE; exec "$3" dispatch mcp-server; fi; exec al dispatch mcp-server`
	builtInDispatchArgZero = "agent-layer-mcp"
)

// BuiltInDispatchServer returns the derived Agent Dispatch MCP server for one
// client, or false when that client is not an enabled dispatch caller. It is
// the single source every surface reads from: native MCP projection, permission
// allowlists, warning accounting, and doctor.
func BuiltInDispatchServer(cfg config.Config, client string) (ResolvedMCPServer, bool) {
	if !clientAgentEnabled(cfg, client) {
		return ResolvedMCPServer{}, false
	}
	return ResolvedMCPServer{
		ID:                 BuiltInDispatchServerID,
		Transport:          config.TransportStdio,
		Command:            builtInDispatchCommand,
		Args:               []string{builtInDispatchSubject, builtInDispatchAction},
		ToolTimeoutSeconds: int(config.DispatchMCPToolTimeout(cfg).Seconds()),
	}, true
}

// DispatchLauncherOptions controls per-call generation of the built-in launcher.
// The zero value preserves ordinary installed executable discovery.
type DispatchLauncherOptions struct {
	// OmitInstalledExecutable keeps host installation paths out of portable projections.
	OmitInstalledExecutable bool
}

// RootedBuiltInDispatchServer returns the built-in server with a launcher that
// enters repoRoot before invoking al. This lets version dispatch find the repo
// pin even when an MCP client chooses a different server working directory.
// The release launcher also records the al entry that sync resolved (see
// InstalledExecutable) and appends its directory to PATH, so GUI and background
// clients without the terminal PATH can start it. It falls back to al on PATH
// when nothing was recorded or the recorded entry is no longer executable.
// Options can omit the installed entry when generating portable projections.
func RootedBuiltInDispatchServer(cfg config.Config, client string, repoRoot string, options DispatchLauncherOptions) (ResolvedMCPServer, bool, error) {
	builtIn, ok := BuiltInDispatchServer(cfg, client)
	if !ok {
		return ResolvedMCPServer{}, false, nil
	}
	if repoRoot != "" {
		if err := versiondispatch.ValidateDevelopmentExecutable(repoRoot); err != nil {
			return ResolvedMCPServer{}, false, err
		}
		// Preserve the caller cwd for current binaries while keeping the actual al
		// arguments compatible with cached older binaries, which ignore the extra
		// environment variable and run safely from the canonical root.
		builtIn.Command = builtInDispatchShell
		installed, installedDir := "", ""
		if !options.OmitInstalledExecutable {
			installed = InstalledExecutable()
		}
		if installed != "" {
			installedDir = filepath.Dir(installed)
		}
		builtIn.Args = []string{"-c", builtInDispatchScript, builtInDispatchArgZero, repoRoot, versiondispatch.DevelopmentExecutable(repoRoot), installed, installedDir}
	}
	return builtIn, true, nil
}

// InstalledExecutable returns the absolute installed al entry point that the
// generated launcher records, or "" when none qualifies. Candidates, in order:
// the entry a release launcher or pinned re-exec exported (so regeneration
// stays stable even when PATH lists another al first or omits it), the first al
// on PATH, then an unpinned invocation path such as /install/bin/al. The
// development executable, go run builds and version-dispatched cache binaries
// are never recorded, and the entry is not resolved through symlinks because
// package-manager targets are versioned and go stale on upgrade.
func InstalledExecutable() string {
	if candidate := os.Getenv(versiondispatch.EnvInstalledExecutable); filepath.Base(candidate) == builtInDispatchCommand && installedEntry(candidate) {
		return candidate
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if !filepath.IsAbs(dir) {
			continue
		}
		if candidate := filepath.Join(dir, builtInDispatchCommand); installedEntry(candidate) {
			return candidate
		}
	}
	// Dev snapshots intentionally do not export a shim marker. Their argv[0]
	// is still a cache image, never an installed entry for regenerated launchers.
	if len(os.Args) == 0 || os.Getenv(versiondispatch.EnvShimActive) != "" || versiondispatch.RunningFromImageCache() {
		return ""
	}
	invoked := os.Args[0]
	if !strings.ContainsRune(invoked, filepath.Separator) || filepath.Base(invoked) != builtInDispatchCommand || goRunExecutable(invoked) {
		return ""
	}
	if abs, err := filepath.Abs(invoked); err == nil && installedEntry(abs) {
		return abs
	}
	return ""
}

// goRunExecutable reports whether path lies in a go run build directory, which
// Go removes when the command exits.
func goRunExecutable(path string) bool {
	return slices.ContainsFunc(strings.Split(filepath.ToSlash(path), "/"), func(element string) bool {
		return strings.HasPrefix(element, "go-build")
	})
}

// installedEntry reports whether path is an absolute regular file this user
// can execute, other than the development executable. Clients expand ${...} in
// generated argv, so such paths are rejected rather than escaped.
func installedEntry(path string) bool {
	if !filepath.IsAbs(path) || strings.Contains(path, "${") {
		return false
	}
	info, err := os.Stat(path) // #nosec G703 -- only inspects a candidate executable; nothing is read or written.
	if err != nil {
		return false
	}
	if err := fsutil.RequireExecutable(path, info); err != nil {
		return false
	}
	development := os.Getenv(versiondispatch.EnvDevelopmentExecutable)
	if !filepath.IsAbs(development) {
		return true
	}
	if filepath.Clean(path) == filepath.Clean(development) {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return true
	}
	resolvedDevelopment, err := filepath.EvalSymlinks(development)
	return err != nil || resolved != resolvedDevelopment
}

// clientAgentEnabled reports whether the agent behind an MCP client is enabled.
// Claude Code's terminal and Visual Studio Code surfaces share one project MCP
// configuration, so either one enables the Claude client.
func clientAgentEnabled(cfg config.Config, client string) bool {
	switch client {
	case ClientCodex:
		return config.IsAgentEnabled(cfg.Agents.Codex.Enabled)
	case ClientClaude:
		return config.IsAgentEnabled(cfg.Agents.Claude.Enabled) ||
			config.IsAgentEnabled(cfg.Agents.ClaudeVSCode.Enabled)
	case ClientAntigravity:
		return config.IsAgentEnabled(cfg.Agents.Antigravity.Enabled)
	case ClientCopilot:
		return config.IsAgentEnabled(cfg.Agents.CopilotCLI.Enabled)
	case ClientGrok:
		return config.IsAgentEnabled(cfg.Agents.Grok.Enabled)
	case ClientMuse:
		return config.IsAgentEnabled(cfg.Agents.Muse.Enabled)
	case ClientVSCode:
		return config.IsAgentEnabled(cfg.Agents.VSCode.Enabled)
	default:
		return false
	}
}

// EffectiveMCPServers returns every MCP server a client actually receives: the
// user's enabled servers plus the derived built-in Agent Dispatch server. A nil
// resolver keeps resolved env values.
func EffectiveMCPServers(cfg config.Config, env map[string]string, client string, resolver EnvVarResolver, options DispatchLauncherOptions) ([]ResolvedMCPServer, error) {
	resolved, err := resolveServers(clientServers(cfg.MCP.Servers, client), env, resolver)
	if err != nil {
		return nil, err
	}
	sortServersByID(resolved)
	builtIn, ok, err := RootedBuiltInDispatchServer(cfg, client, env[config.BuiltinRepoRootEnvVar], options)
	if err != nil {
		return nil, err
	}
	return withBuiltInServer(resolved, builtIn, ok), nil
}

// EffectiveServerIDs returns the sorted IDs of every MCP server a client
// actually receives.
func EffectiveServerIDs(cfg config.Config, client string) []string {
	var ids []string
	for _, server := range clientServers(cfg.MCP.Servers, client) {
		ids = append(ids, server.ID)
	}
	sort.Strings(ids)
	if _, ok := BuiltInDispatchServer(cfg, client); ok && !slices.Contains(ids, BuiltInDispatchServerID) {
		ids = append(ids, BuiltInDispatchServerID)
		sort.Strings(ids)
	}
	return ids
}

// ResolveEffectiveEnabledMCPServers resolves every MCP server that at least one
// enabled client actually receives, including the built-in Agent Dispatch
// server. Warning and doctor accounting use it so the tools Agent Layer itself
// adds are measured alongside user-configured servers.
func ResolveEffectiveEnabledMCPServers(cfg config.Config, env map[string]string) ([]ResolvedMCPServer, error) {
	resolved, err := resolveServers(ReceivedMCPServers(cfg), env, nil)
	if err != nil {
		return nil, err
	}
	builtIn, ok := effectiveBuiltInDispatchServer(cfg)
	return withBuiltInServer(resolved, builtIn, ok), nil
}

// EffectiveEnabledServerIDs returns every server doctor and warning discovery
// will inspect, including the derived built-in server at most once.
func EffectiveEnabledServerIDs(cfg config.Config) []string {
	received := ReceivedMCPServers(cfg)
	ids := make([]string, 0, len(received)+1)
	for _, server := range received {
		ids = append(ids, server.ID)
	}
	if _, ok := effectiveBuiltInDispatchServer(cfg); ok && !slices.Contains(ids, BuiltInDispatchServerID) {
		ids = append(ids, BuiltInDispatchServerID)
	}
	return ids
}

// ReceivedMCPServers returns the enabled user-configured MCP servers that sync
// projects to at least one enabled client, in configuration order. A server
// limited by `clients` to disabled clients reaches no generated config, so
// doctor neither requires its secrets nor probes it.
func ReceivedMCPServers(cfg config.Config) []config.MCPServer {
	var received []config.MCPServer
	for _, server := range cfg.MCP.Servers {
		if !config.IsAgentEnabled(server.Enabled) {
			continue
		}
		for _, client := range mcpClients {
			if userServersReachClient(cfg, client) && server.AppliesToClient(client) {
				received = append(received, server)
				break
			}
		}
	}
	return received
}

var mcpClients = []string{ClientAntigravity, ClientClaude, ClientCodex, ClientCopilot, ClientGrok, ClientMuse, ClientVSCode}

// userServersReachClient mirrors sync's gating of each client's generated MCP
// config. Codex config is also written for VS Code, but an
// agents.codex.agent_specific.mcp_servers table replaces the projected servers.
func userServersReachClient(cfg config.Config, client string) bool {
	if client == ClientCodex {
		written := config.IsAgentEnabled(cfg.Agents.Codex.Enabled) || config.IsAgentEnabled(cfg.Agents.VSCode.Enabled)
		return written && !config.HasProviderPassthroughKey(cfg.Agents.Codex.AgentSpecific, config.CodexMCPServersKey)
	}
	return clientAgentEnabled(cfg, client)
}

func effectiveBuiltInDispatchServer(cfg config.Config) (ResolvedMCPServer, bool) {
	for _, client := range mcpClients {
		if builtIn, ok := BuiltInDispatchServer(cfg, client); ok {
			return builtIn, true
		}
	}
	return ResolvedMCPServer{}, false
}

// withBuiltInServer adds the built-in server when ok and its ID is not already
// present, then re-sorts by ID. Otherwise resolved is returned unchanged.
func withBuiltInServer(resolved []ResolvedMCPServer, builtIn ResolvedMCPServer, ok bool) []ResolvedMCPServer {
	if !ok || slices.ContainsFunc(resolved, func(server ResolvedMCPServer) bool { return server.ID == builtIn.ID }) {
		return resolved
	}
	resolved = append(resolved, builtIn)
	sortServersByID(resolved)
	return resolved
}

func sortServersByID(servers []ResolvedMCPServer) {
	sort.Slice(servers, func(i, j int) bool { return servers[i].ID < servers[j].ID })
}

// RootMCPExclusions returns generated root .mcp.json IDs that a client which
// also discovers that file must mask. The file holds the Claude projection when
// either Claude integration is enabled and the Muse projection when Muse is
// enabled. Native personal/plugin servers are deliberately outside this set.
func RootMCPExclusions(cfg config.Config, client string) []string {
	var shared []string
	if config.IsAgentEnabled(cfg.Agents.Muse.Enabled) {
		shared = append(shared, EffectiveServerIDs(cfg, ClientMuse)...)
	}
	if config.IsAgentEnabled(cfg.Agents.Claude.Enabled) || config.IsAgentEnabled(cfg.Agents.ClaudeVSCode.Enabled) {
		shared = append(shared, EffectiveServerIDs(cfg, ClientClaude)...)
	}
	selected := EffectiveServerIDs(cfg, client)
	seen := make(map[string]bool)
	var excluded []string
	for _, id := range shared {
		if !seen[id] && !slices.Contains(selected, id) {
			excluded = append(excluded, id)
		}
		seen[id] = true
	}
	sort.Strings(excluded)
	return excluded
}

// BuiltInDispatchEnvVars lists launch-time context needed by the dispatch server.
// Clients that filter subprocess environments must forward these names without
// snapshotting their values into generated configuration.
func BuiltInDispatchEnvVars() []string {
	return []string{
		"AL_DISPATCH_ACTIVE", "AL_RUN_ID", "AL_RUN_DIR",
		versiondispatch.EnvDevelopmentBypassVersionDispatch,
		versiondispatch.EnvDevelopmentExecutable,
	}
}
