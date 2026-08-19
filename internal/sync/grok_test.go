package sync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestBuildGrokConfigStdio(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "ripgrep",
						Enabled:   &enabled,
						Clients:   []string{"grok"},
						Transport: "stdio",
						Command:   "npx",
						Args:      []string{"-y", "mcp-ripgrep@0.4.0"},
						Env:       map[string]string{"DEBUG": "1"},
					},
				},
			},
		},
		Env: map[string]string{},
	}

	content, err := buildGrokConfig(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(content, `[mcp_servers."ripgrep"]`) {
		t.Fatalf("expected quoted ripgrep MCP table, got:\n%s", content)
	}
	if !strings.Contains(content, `command = "npx"`) {
		t.Fatalf("expected command = \"npx\", got:\n%s", content)
	}
	if !strings.Contains(content, `args = ["-y", "mcp-ripgrep@0.4.0"]`) {
		t.Fatalf("expected args, got:\n%s", content)
	}
	if !strings.Contains(content, `env = { "DEBUG" = "1" }`) {
		t.Fatalf("expected env, got:\n%s", content)
	}
}

func TestBuildGrokConfigHTTP(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "example",
						Enabled:   &enabled,
						Clients:   []string{"grok"},
						Transport: "http",
						URL:       "https://example.com/mcp",
						Headers:   map[string]string{"Authorization": "Bearer token"},
					},
				},
			},
		},
		Env: map[string]string{},
	}

	content, err := buildGrokConfig(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(content, `[mcp_servers."example"]`) {
		t.Fatalf("expected quoted example MCP table, got:\n%s", content)
	}
	if !strings.Contains(content, `url = "https://example.com/mcp"`) {
		t.Fatalf("expected URL, got:\n%s", content)
	}
	if !strings.Contains(content, `headers = { "Authorization" = "Bearer token" }`) {
		t.Fatalf("expected headers, got:\n%s", content)
	}
	if !strings.Contains(content, `type = "sse"`) || strings.Contains(content, `transport =`) {
		t.Fatalf("expected Grok-native SSE type and no transport key, got:\n%s", content)
	}
}

func TestBuildGrokConfigStreamableHTTPOmitsSSEType(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{MCP: config.MCPConfig{Servers: []config.MCPServer{{
			ID: "streamable", Enabled: &enabled, Clients: []string{"grok"},
			Transport: "http", HTTPTransport: "streamable", URL: "https://example.com/mcp",
		}}}},
		Env: map[string]string{},
	}
	content, err := buildGrokConfig(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(content, `type = "sse"`) || strings.Contains(content, `transport =`) {
		t.Fatalf("streamable HTTP should use Grok's URL-only form, got:\n%s", content)
	}
}

func TestBuildGrokConfigQuotesMCPServerIDs(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{MCP: config.MCPConfig{Servers: []config.MCPServer{{
			ID: "server.with.dot", Enabled: &enabled, Clients: []string{"grok"},
			Transport: "stdio", Command: "server",
		}}}},
		Env: map[string]string{},
	}
	content, err := buildGrokConfig(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(content, `[mcp_servers."server.with.dot"]`) {
		t.Fatalf("expected server ID to remain one quoted TOML key, got:\n%s", content)
	}
}

func TestBuildGrokConfigPluginSettings(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Grok: config.GrokConfig{
					AgentSpecific: map[string]any{
						"plugins": map[string]any{"enabled": []string{"example"}},
					},
				},
			},
		},
		Env: map[string]string{},
	}

	content, err := buildGrokConfig(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(content, `[plugins]`) || !strings.Contains(content, `enabled = ['example']`) {
		t.Fatalf("expected plugin setting in output, got:\n%s", content)
	}
}

func TestBuildGrokConfigExcludesOtherClients(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "claude-only",
						Enabled:   &enabled,
						Clients:   []string{"claude"},
						Transport: "stdio",
						Command:   "tool",
					},
				},
			},
		},
		Env: map[string]string{},
	}

	content, err := buildGrokConfig(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(content, "claude-only") {
		t.Fatalf("expected no claude-only server in grok config, got:\n%s", content)
	}
}

func TestWriteGrokConfig(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "test",
						Enabled:   &enabled,
						Transport: "stdio",
						Command:   "test-tool",
					},
				},
			},
		},
		Env: map[string]string{},
	}

	sys := &RealSystem{}
	if err := writeGrokConfig(sys, root, project); err != nil {
		t.Fatalf("writeGrokConfig error: %v", err)
	}

	path := filepath.Join(root, ".grok", "config.toml")
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("failed to read generated file: %v", err)
	}

	if !strings.Contains(string(data), `[mcp_servers."test"]`) {
		t.Fatalf("expected quoted test MCP table in config.toml, got:\n%s", string(data))
	}
}

func TestWriteGrokConfigMkdirError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Place a file where the .grok directory would be created.
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	project := &config.ProjectConfig{Env: map[string]string{}}
	if err := writeGrokConfig(&RealSystem{}, file, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteGrokConfigWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	grokDir := filepath.Join(root, ".grok")
	if err := os.MkdirAll(grokDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Create a directory where the file would be written.
	if err := os.Mkdir(filepath.Join(grokDir, "config.toml"), 0o700); err != nil {
		t.Fatalf("mkdir config.toml: %v", err)
	}
	project := &config.ProjectConfig{Env: map[string]string{}}
	if err := writeGrokConfig(&RealSystem{}, root, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteGrokConfigSurfacesBuildAndFilesystemFailures(t *testing.T) {
	injected := errors.New("injected")
	root := t.TempDir()

	badPassthrough := &config.ProjectConfig{
		Config: config.Config{Agents: config.AgentsConfig{Grok: config.GrokConfig{
			AgentSpecific: map[string]any{"unsupported": make(chan int)},
		}}},
		Env: map[string]string{},
	}
	if err := writeGrokConfig(RealSystem{}, root, badPassthrough); err == nil || !strings.Contains(err.Error(), "encode agents.grok.agent_specific") {
		t.Fatalf("passthrough encoding error = %v", err)
	}

	project := &config.ProjectConfig{Env: map[string]string{}}
	mkdirSystem := &MockSystem{Fallback: RealSystem{}, MkdirAllFunc: func(string, os.FileMode) error { return injected }}
	if err := writeGrokConfig(mkdirSystem, root, project); !errors.Is(err, injected) {
		t.Fatalf("mkdir error = %v", err)
	}
	writeSystem := &MockSystem{Fallback: RealSystem{}, WriteFileAtomicFunc: func(string, []byte, os.FileMode) error { return injected }}
	if err := writeGrokConfig(writeSystem, root, project); !errors.Is(err, injected) {
		t.Fatalf("write error = %v", err)
	}
}

func TestCleanGrokOutputsSurfacesManagedFileFailures(t *testing.T) {
	injected := errors.New("injected")
	root := t.TempDir()
	path := filepath.Join(root, ".grok", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(grokHeader), 0o600); err != nil {
		t.Fatal(err)
	}

	readSystem := &MockSystem{Fallback: RealSystem{}, ReadFileFunc: func(string) ([]byte, error) { return nil, injected }}
	if err := cleanGrokOutputs(readSystem, root); !errors.Is(err, injected) {
		t.Fatalf("read error = %v", err)
	}
	removeSystem := &MockSystem{Fallback: RealSystem{}, RemoveFunc: func(string) error { return injected }}
	if err := cleanGrokOutputs(removeSystem, root); !errors.Is(err, injected) {
		t.Fatalf("remove error = %v", err)
	}
	lstatSystem := &MockSystem{Fallback: RealSystem{}, LstatFunc: func(string) (os.FileInfo, error) { return nil, injected }}
	if err := cleanGrokOutputs(lstatSystem, root); !errors.Is(err, injected) {
		t.Fatalf("lstat error = %v", err)
	}
}

func TestWriteGrokConfigRejectsUserOwnedAndSymlinkTargets(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{Env: map[string]string{}}

	t.Run("user-owned config", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, ".grok"), 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, ".grok", "config.toml")
		const existing = "theme = \"custom\"\n"
		if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
			t.Fatal(err)
		}
		err := writeGrokConfig(RealSystem{}, root, project)
		if err == nil || !strings.Contains(err.Error(), "refusing to overwrite user-owned Grok config") {
			t.Fatalf("expected ownership conflict, got %v", err)
		}
		data, readErr := os.ReadFile(path) // #nosec G304 -- test-controlled path.
		if readErr != nil || string(data) != existing {
			t.Fatalf("user config changed: %q, %v", data, readErr)
		}
	})

	t.Run("symlinked directory", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, ".grok")); err != nil {
			t.Fatal(err)
		}
		if err := writeGrokConfig(RealSystem{}, root, project); err == nil || !strings.Contains(err.Error(), "real directory") {
			t.Fatalf("expected symlink conflict, got %v", err)
		}
	})
}

func TestWriteGrokConfigRecognizesStableAndLegacyGeneratedMarkers(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{Env: map[string]string{}}

	for _, existing := range []string{
		grokGeneratedMarker + "# Header wording changed in a later release\ncustom = true\n",
		grokLegacyGeneratedLine + "# This file is gitignored. Do not commit or share it.\n",
	} {
		root := t.TempDir()
		grokDir := filepath.Join(root, ".grok")
		if err := os.MkdirAll(grokDir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(grokDir, "config.toml")
		if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := writeGrokConfig(RealSystem{}, root, project); err != nil {
			t.Fatalf("writeGrokConfig rejected generated config: %v", err)
		}
		data, err := os.ReadFile(path) // #nosec G304 -- test-controlled path.
		if err != nil {
			t.Fatal(err)
		}
		if string(data) == existing || !strings.HasPrefix(string(data), grokHeader) {
			t.Fatalf("generated config was not refreshed: %q", data)
		}
	}

	if grokConfigIsManaged([]byte("# Personal config\n# GENERATED FILE\n")) {
		t.Fatal("generated marker away from the first line must not claim a user-owned config")
	}
}

func TestCleanGrokOutputsRemovesConfig(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := RealSystem{}

	// Set up artifacts as if Grok were previously enabled.
	grokDir := filepath.Join(root, ".grok")
	if err := os.MkdirAll(grokDir, 0o700); err != nil {
		t.Fatalf("mkdir .grok: %v", err)
	}
	if err := os.WriteFile(filepath.Join(grokDir, "config.toml"), []byte(grokHeader+`[mcp_servers]`), 0o600); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	if err := cleanGrokOutputs(sys, root); err != nil {
		t.Fatalf("cleanGrokOutputs error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(grokDir, "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("expected config.toml to be removed")
	}
}

func TestCleanGrokOutputsRejectsSymlinkedDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideConfig := filepath.Join(outside, "config.toml")
	const content = grokHeader + "[mcp_servers]\n"
	if err := os.WriteFile(outsideConfig, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".grok")); err != nil {
		t.Fatal(err)
	}

	err := cleanGrokOutputs(RealSystem{}, root)
	if err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("expected symlink conflict, got %v", err)
	}
	data, readErr := os.ReadFile(outsideConfig) // #nosec G304 -- test-controlled path.
	if readErr != nil || string(data) != content {
		t.Fatalf("outside config changed: %q, %v", data, readErr)
	}
}

func TestCleanGrokOutputsPreservesUserOwnedConfig(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	grokDir := filepath.Join(root, ".grok")
	if err := os.MkdirAll(grokDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(grokDir, "config.toml")
	const existing = "theme = \"custom\"\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cleanGrokOutputs(RealSystem{}, root); err != nil {
		t.Fatalf("cleanGrokOutputs: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled path.
	if err != nil || string(data) != existing {
		t.Fatalf("user config changed: %q, %v", data, err)
	}
}

func TestCleanGrokOutputsNoArtifacts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// No .grok/ exists — should not error.
	if err := cleanGrokOutputs(RealSystem{}, root); err != nil {
		t.Fatalf("cleanGrokOutputs error on clean dir: %v", err)
	}
}

func TestBuildGrokConfigWritesPermissionAllow(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll},
			Agents: config.AgentsConfig{
				Grok: config.GrokConfig{Enabled: &enabled},
			},
		},
		CommandsAllow: []string{"git status", "go test"},
		Env:           map[string]string{},
	}

	content, err := buildGrokConfig(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(content, "[permission]") {
		t.Fatalf("expected [permission], got:\n%s", content)
	}
	if !strings.Contains(content, `Bash(git status:*)`) || !strings.Contains(content, `Bash(go test:*)`) {
		t.Fatalf("expected command allow rules, got:\n%s", content)
	}
	if !strings.Contains(content, `mcp__agent-layer__*`) {
		t.Fatalf("expected built-in dispatch MCP allow rule, got:\n%s", content)
	}
	if !strings.Contains(content, `tool_timeout_sec = 2400`) {
		t.Fatalf("expected built-in dispatch MCP timeout, got:\n%s", content)
	}
}
