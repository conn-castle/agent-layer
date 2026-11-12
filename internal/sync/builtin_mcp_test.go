package sync

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/projection"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
)

func builtInProject(t *testing.T) *config.ProjectConfig {
	t.Helper()
	on := true
	cfg := config.Config{}
	cfg.Agents.Claude.Enabled = &on
	cfg.Agents.Codex.Enabled = &on
	cfg.Agents.Antigravity.Enabled = &on
	cfg.Agents.CopilotCLI.Enabled = &on
	cfg.Agents.VSCode.Enabled = &on
	root := t.TempDir()
	return &config.ProjectConfig{Config: cfg, Env: config.WithBuiltInEnv(map[string]string{}, root), Root: root}
}

func wantBuiltInDispatchInvocation(t *testing.T, command string, args []string, root string) {
	t.Helper()
	if command != "/bin/sh" {
		t.Fatalf("built-in dispatch command = %q, want /bin/sh", command)
	}
	// Parallel tests cannot pin PATH; the process PATH is stable meanwhile.
	installed, installedDir := projection.InstalledExecutable(), ""
	if installed != "" {
		installedDir = filepath.Dir(installed)
	}
	want := []string{"-c", `AL_MCP_WORKING_DIR=$PWD; export AL_MCP_WORKING_DIR; cd "$1" || exit; if [ -n "$2" ]; then AL_DEV_EXECUTABLE=$2; AL_DEV_BYPASS_VERSION_DISPATCH=1; AL_DEV_SOURCE_ROOT=$1; export AL_DEV_EXECUTABLE AL_DEV_BYPASS_VERSION_DISPATCH AL_DEV_SOURCE_ROOT; exec "$2" dispatch mcp-server; fi; unset AL_DEV_EXECUTABLE AL_DEV_BYPASS_VERSION_DISPATCH AL_DEV_SOURCE_ROOT; if [ -x "$3" ]; then case ":$PATH:" in *":$4:"*) ;; "::") PATH=$4 ;; *) PATH=$PATH:$4 ;; esac; AL_INSTALLED_EXECUTABLE=$3; export PATH AL_INSTALLED_EXECUTABLE; exec "$3" dispatch mcp-server; fi; exec al dispatch mcp-server`, "agent-layer-mcp", root, versiondispatch.DevelopmentExecutable(root), installed, installedDir}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("built-in dispatch args = %q, want %q", args, want)
	}
}

// TestClaudeReceivesTheBuiltInDispatchServer proves the generated Claude MCP
// configuration declares the canonical stdio server. Claude has no per-server
// timeout field, so the definition must carry nothing beyond the standard
// stdio keys — Agent Layer must not reach for the client-wide MCP_TOOL_TIMEOUT.
func TestClaudeReceivesTheBuiltInDispatchServer(t *testing.T) {
	t.Parallel()
	project := builtInProject(t)
	cfg, err := buildMCPConfig(project, projection.DispatchLauncherOptions{})
	if err != nil {
		t.Fatalf("build claude mcp config: %v", err)
	}
	server, ok := cfg.Servers[projection.BuiltInDispatchServerID]
	if !ok {
		t.Fatalf("claude mcp config is missing %q: %#v", projection.BuiltInDispatchServerID, cfg.Servers)
	}
	if server.Type != config.TransportStdio {
		t.Fatalf("built-in claude server = %#v, want an stdio server", server)
	}
	wantBuiltInDispatchInvocation(t, server.Command, server.Args, project.Root)
	if len(server.Env) != 0 || server.URL != "" {
		t.Fatalf("built-in claude server carries unexpected fields: %#v", server)
	}
}

// TestAntigravityReceivesTheBuiltInDispatchServer proves Antigravity receives
// the same canonical definition using only its documented schema keys; it has
// no tool-timeout field and none may be invented.
func TestAntigravityReceivesTheBuiltInDispatchServer(t *testing.T) {
	t.Parallel()
	project := builtInProject(t)
	cfg, err := buildAntigravityMCPConfig(project, projection.DispatchLauncherOptions{})
	if err != nil {
		t.Fatalf("build antigravity mcp config: %v", err)
	}
	server, ok := cfg.Servers[projection.BuiltInDispatchServerID]
	if !ok {
		t.Fatalf("antigravity mcp config is missing %q: %#v", projection.BuiltInDispatchServerID, cfg.Servers)
	}
	wantBuiltInDispatchInvocation(t, server.Command, server.Args, project.Root)
	if server.ServerURL != "" || len(server.Headers) != 0 {
		t.Fatalf("built-in antigravity server carries unexpected fields: %#v", server)
	}
}

// TestCopilotReceivesTheBuiltInDispatchServer proves a shared-skill consumer
// cannot be left with Agent Dispatch instructions but no matching MCP tools.
func TestCopilotReceivesTheBuiltInDispatchServer(t *testing.T) {
	t.Parallel()
	project := builtInProject(t)
	cfg, err := buildCopilotMCPConfig(project, projection.DispatchLauncherOptions{})
	if err != nil {
		t.Fatalf("build copilot mcp config: %v", err)
	}
	server, ok := cfg.Servers[projection.BuiltInDispatchServerID]
	if !ok {
		t.Fatalf("copilot mcp config is missing %q: %#v", projection.BuiltInDispatchServerID, cfg.Servers)
	}
	if server.Type != config.TransportStdio {
		t.Fatalf("built-in copilot server = %#v", server)
	}
	wantBuiltInDispatchInvocation(t, server.Command, server.Args, project.Root)
	if len(server.Tools) != 1 || server.Tools[0] != "*" {
		t.Fatalf("built-in copilot server tools = %v, want [\"*\"]", server.Tools)
	}
}

// TestVSCodeReceivesTheBuiltInDispatchServer proves the editor's shared-skill
// projection and MCP configuration expose one coherent Agent Dispatch surface.
func TestVSCodeReceivesTheBuiltInDispatchServer(t *testing.T) {
	t.Parallel()
	project := builtInProject(t)
	cfg, err := buildVSCodeMCPConfig(project, projection.DispatchLauncherOptions{})
	if err != nil {
		t.Fatalf("build vscode mcp config: %v", err)
	}
	server, ok := cfg.Servers[projection.BuiltInDispatchServerID]
	if !ok {
		t.Fatalf("vscode mcp config is missing %q: %#v", projection.BuiltInDispatchServerID, cfg.Servers)
	}
	if server.Type != config.TransportStdio {
		t.Fatalf("built-in vscode server = %#v", server)
	}
	wantBuiltInDispatchInvocation(t, server.Command, server.Args, project.Root)
}

// TestCodexReceivesTheBuiltInDispatchServerWithItsHardTimeout proves Codex's
// native per-server tool timeout is projected from the canonical configuration
// value, parsed structurally rather than matched as file text.
func TestCodexReceivesTheBuiltInDispatchServerWithItsHardTimeout(t *testing.T) {
	t.Parallel()
	project := builtInProject(t)
	minutes := 12
	project.Config.Dispatch.MCPWaitTimeoutMinutes = new(int)
	*project.Config.Dispatch.MCPWaitTimeoutMinutes = 5
	project.Config.Dispatch.MCPToolTimeoutMinutes = &minutes

	managed, err := buildCodexManagedConfigWithSystem(RealSystem{}, project.Root, project, true)
	if err != nil {
		t.Fatalf("build codex config: %v", err)
	}
	var decoded struct {
		Features struct {
			CodeMode struct {
				DirectOnlyToolNamespaces []string `toml:"direct_only_tool_namespaces"`
			} `toml:"code_mode"`
		} `toml:"features"`
		MCPServers map[string]struct {
			Command        string   `toml:"command"`
			Args           []string `toml:"args"`
			ToolTimeoutSec int      `toml:"tool_timeout_sec"`
		} `toml:"mcp_servers"`
	}
	if err := toml.Unmarshal([]byte(managed.Content), &decoded); err != nil {
		t.Fatalf("parse generated codex config: %v\n%s", err, managed.Content)
	}
	server, ok := decoded.MCPServers[projection.BuiltInDispatchServerID]
	if !ok {
		t.Fatalf("codex config is missing %q: %#v", projection.BuiltInDispatchServerID, decoded.MCPServers)
	}
	wantBuiltInDispatchInvocation(t, server.Command, server.Args, project.Root)
	if server.ToolTimeoutSec != minutes*60 {
		t.Fatalf("codex tool_timeout_sec = %d, want %d", server.ToolTimeoutSec, minutes*60)
	}
	namespaces := decoded.Features.CodeMode.DirectOnlyToolNamespaces
	if len(namespaces) != 1 || namespaces[0] != codexAgentLayerToolNamespace {
		t.Fatalf("Codex direct-only tool namespaces = %v, want [%q]", namespaces, codexAgentLayerToolNamespace)
	}
}

// TestCodexPreservesUserDirectToolNamespaces proves Agent Layer adds its
// namespace without replacing or duplicating a user's Codex-native settings.
func TestCodexPreservesUserDirectToolNamespaces(t *testing.T) {
	t.Parallel()
	project := builtInProject(t)
	project.Config.Agents.Codex.AgentSpecific = map[string]any{
		"features": map[string]any{
			"code_mode": map[string]any{
				"direct_only_tool_namespaces": []any{"mcp__history", codexAgentLayerToolNamespace},
			},
		},
	}

	managed, err := buildCodexManagedConfigWithSystem(RealSystem{}, project.Root, project, true)
	if err != nil {
		t.Fatalf("build codex config: %v", err)
	}
	var decoded struct {
		Features struct {
			CodeMode struct {
				DirectOnlyToolNamespaces []string `toml:"direct_only_tool_namespaces"`
			} `toml:"code_mode"`
		} `toml:"features"`
	}
	if err := toml.Unmarshal([]byte(managed.Content), &decoded); err != nil {
		t.Fatalf("parse generated codex config: %v\n%s", err, managed.Content)
	}
	namespaces := decoded.Features.CodeMode.DirectOnlyToolNamespaces
	if len(namespaces) != 2 || namespaces[0] != "mcp__history" || namespaces[1] != codexAgentLayerToolNamespace {
		t.Fatalf("Codex direct-only tool namespaces = %v, want preserved user namespace plus Agent Layer", namespaces)
	}
}

// TestCodexRemovesAgentLayerDirectNamespaceWhenDispatchIsNotProjected proves
// switching to a VS Code-only projection cannot leave a stale Codex routing
// override in the shared project config.
func TestCodexRemovesAgentLayerDirectNamespaceWhenDispatchIsNotProjected(t *testing.T) {
	t.Parallel()
	project := builtInProject(t)
	withDispatch, err := buildCodexManagedConfigWithSystem(RealSystem{}, project.Root, project, true)
	if err != nil {
		t.Fatalf("build Codex config with dispatch: %v", err)
	}

	off := false
	project.Config.Agents.Codex.Enabled = &off
	withoutDispatch, err := buildCodexManagedConfigWithSystem(RealSystem{}, project.Root, project, false)
	if err != nil {
		t.Fatalf("build VS Code-only Codex config: %v", err)
	}
	merged, err := mergeCodexConfig(filepath.Join(project.Root, ".codex", "config.toml"), withDispatch.Content, withoutDispatch)
	if err != nil {
		t.Fatalf("merge VS Code-only Codex config: %v", err)
	}

	var decoded map[string]any
	if err := toml.Unmarshal([]byte(merged), &decoded); err != nil {
		t.Fatalf("parse merged Codex config: %v\n%s", err, merged)
	}
	if _, ok := valueAtPath(decoded, []string{codexFeaturesKey, codexCodeModeKey, codexDirectOnlyToolNamespacesKey}); ok {
		t.Fatalf("stale Agent Layer direct-only namespace remains after dispatch removal:\n%s", merged)
	}
}

// TestUserServersNeverReceiveTheDispatchToolTimeout proves the per-server
// timeout stays scoped to the built-in server: a user's stdio server keeps the
// client default.
func TestUserServersNeverReceiveTheDispatchToolTimeout(t *testing.T) {
	t.Parallel()
	project := builtInProject(t)
	on := true
	project.Config.MCP.Servers = []config.MCPServer{
		{ID: "user", Enabled: &on, Transport: config.TransportStdio, Command: "user-server"},
	}
	managed, err := buildCodexManagedConfigWithSystem(RealSystem{}, project.Root, project, true)
	if err != nil {
		t.Fatalf("build codex config: %v", err)
	}
	var decoded struct {
		MCPServers map[string]struct {
			ToolTimeoutSec int `toml:"tool_timeout_sec"`
		} `toml:"mcp_servers"`
	}
	if err := toml.Unmarshal([]byte(managed.Content), &decoded); err != nil {
		t.Fatalf("parse generated codex config: %v", err)
	}
	if decoded.MCPServers["user"].ToolTimeoutSec != 0 {
		t.Fatalf("user server received tool_timeout_sec = %d, want none",
			decoded.MCPServers["user"].ToolTimeoutSec)
	}
}

type portableLauncherSystem struct {
	RealSystem
	omit bool
}

func (sys portableLauncherSystem) OmitInstalledExecutable() bool { return sys.omit }

// TestSyncDispatchLauncherInstalledEntry exercises the actual sync boundary and
// discovers every generated launcher file across the enabled clients.
func TestSyncDispatchLauncherInstalledEntry(t *testing.T) {
	t.Setenv(versiondispatch.EnvInstalledExecutable, "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	installedDir := t.TempDir()
	installed := filepath.Join(installedDir, "al")
	require.NoError(t, os.WriteFile(installed, []byte("#!/bin/sh\n"), 0o700)) // #nosec G306 -- test-owned executable fixture.
	t.Setenv("PATH", installedDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	development := filepath.Join(t.TempDir(), "al-dev")
	require.NoError(t, os.WriteFile(development, []byte("#!/bin/sh\n"), 0o700)) // #nosec G306 -- test-owned executable fixture.

	for _, layout := range []string{"all-clients", "claude-without-muse", "native-codex-migrated-antigravity"} {
		t.Run(layout, func(t *testing.T) {
			for _, provenance := range []string{"release", "development", "foreign-development-root"} {
				t.Run(provenance, func(t *testing.T) {
					root := t.TempDir()
					require.NoError(t, copyFixtureRepo(filepath.Join("testdata", "fixture-repo"), root))
					configPath := filepath.Join(root, ".agent-layer", "config.toml")
					data, err := os.ReadFile(configPath) // #nosec G304 -- private fixture source.
					require.NoError(t, err)
					if layout != "claude-without-muse" {
						data = append(data, []byte("\n[agents.muse]\nenabled = true\n")...)
					}
					agyPath := ".agy/antigravity-cli/mcp_config.json"
					if layout == "native-codex-migrated-antigravity" {
						data = append(data, []byte("\n[agents.codex.agent_specific.mcp_servers.user]\ncommand = \"user-server\"\n")...)
						agyPath = ".agy/config/mcp_config.json"
						require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, agyPath)), 0o700))
						require.NoError(t, os.WriteFile(filepath.Join(root, agyPath), []byte("{}\n"), 0o600))
					}
					require.NoError(t, os.WriteFile(configPath, data, 0o600)) // #nosec G703 -- configPath is inside the private fixture tree.
					require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", ".env"), []byte("AL_EXAMPLE_TOKEN=fixture\n"), 0o600))
					writeTemplateToFixtureSource(t, root, "claude-statusline.sh", ".agent-layer/claude-statusline.sh", 0o755)
					writeTemplateToFixtureSource(t, root, "codex-statusline.toml", ".agent-layer/codex-statusline.toml", 0o644)

					wantDevelopment := ""
					t.Setenv(versiondispatch.EnvDevelopmentBypassVersionDispatch, "")
					t.Setenv(versiondispatch.EnvDevelopmentExecutable, development)
					t.Setenv(versiondispatch.EnvDevelopmentSourceRoot, root)
					if provenance != "release" {
						t.Setenv(versiondispatch.EnvDevelopmentBypassVersionDispatch, "1")
						if provenance == "development" {
							wantDevelopment = development
						} else {
							t.Setenv(versiondispatch.EnvDevelopmentSourceRoot, t.TempDir())
						}
					}

					wantEntry, wantEntryDir := installed, installedDir
					// Cover exported-entry precedence over PATH in one existing case.
					if layout == "all-clients" && provenance == "release" {
						wantEntryDir = t.TempDir()
						wantEntry = filepath.Join(wantEntryDir, "al")
						require.NoError(t, os.WriteFile(wantEntry, []byte("#!/bin/sh\n"), 0o700)) // #nosec G306 -- test-owned executable fixture.
						t.Setenv(versiondispatch.EnvInstalledExecutable, wantEntry)
					}

					var ordinary map[string][]string
					for _, mode := range []struct {
						name string
						sys  System
						omit bool
					}{
						{"ordinary", RealSystem{}, false},
						{"omitted", portableLauncherSystem{omit: true}, true},
						{"omitted-through-wrapper", struct{ System }{portableLauncherSystem{omit: true}}, true},
						{"omitted-through-mock", &MockSystem{Fallback: portableLauncherSystem{omit: true}}, true},
						{"ordinary-after-omission", RealSystem{}, false},
						{"explicit-retain", portableLauncherSystem{}, false},
					} {
						t.Run(mode.name, func(t *testing.T) {
							_, err := RunWithSystemFS(mode.sys, os.DirFS(root), root)
							require.NoError(t, err)
							launchers := generatedDispatchLaunchers(t, root)
							for _, path := range []string{".mcp.json", ".vscode/mcp.json", ".codex/config.toml", ".copilot/mcp-config.json", ".grok/config.toml", agyPath} {
								require.Contains(t, launchers, path)
							}
							for path, args := range launchers {
								require.Len(t, args, 7, path)
								require.Equal(t, root, args[3], path)
								require.Equal(t, wantDevelopment, args[4], path)
								wantInstalled, wantDir := wantEntry, wantEntryDir
								if mode.omit {
									wantInstalled, wantDir = "", ""
								}
								require.Equal(t, []string{wantInstalled, wantDir}, args[5:], path)
								if ordinary != nil {
									require.Contains(t, ordinary, path)
									require.Equal(t, ordinary[path][:5], args[:5], path)
								}
							}
							if ordinary == nil {
								ordinary = launchers
							} else {
								require.Len(t, launchers, len(ordinary))
							}
						})
					}
				})
			}
		})
	}
}

func generatedDispatchLaunchers(t *testing.T, root string) map[string][]string {
	t.Helper()
	launchers := make(map[string][]string)
	require.NoError(t, fs.WalkDir(os.DirFS(root), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(filepath.Join(root, path)) // #nosec G304 -- walk of a private fixture tree.
		if err != nil || !strings.Contains(string(data), "agent-layer-mcp") {
			return err
		}
		var document map[string]any
		switch filepath.Ext(path) {
		case ".json":
			require.NoError(t, json.Unmarshal(data, &document), path)
		case ".toml":
			require.NoError(t, toml.Unmarshal(data, &document), path)
		default:
			t.Fatalf("unhandled dispatch launcher format: %s", path)
		}
		var collect func(map[string]any)
		collect = func(table map[string]any) {
			if raw, ok := table["args"].([]any); ok && len(raw) > 2 && raw[2] == "agent-layer-mcp" {
				require.Equal(t, "/bin/sh", table["command"], path)
				require.NotContains(t, launchers, path, "duplicate built-in launcher")
				for _, arg := range raw {
					value, ok := arg.(string)
					require.True(t, ok, path)
					launchers[path] = append(launchers[path], value)
				}
			}
			for _, value := range table {
				if nested, ok := value.(map[string]any); ok {
					collect(nested)
				}
			}
		}
		collect(document)
		return nil
	}))
	return launchers
}
