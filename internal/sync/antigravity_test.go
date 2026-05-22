package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestBuildAntigravitySettingsPermissions(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll},
			MCP: config.MCPConfig{Servers: []config.MCPServer{
				{ID: "zeta", Enabled: &enabled, Transport: config.TransportHTTP, URL: "https://z.example", Clients: []string{antigravityClientID}},
				{ID: "alpha", Enabled: &enabled, Transport: config.TransportHTTP, URL: "https://a.example", Clients: []string{antigravityClientID}},
			}},
		},
		CommandsAllow: []string{"git status"},
	}

	settings := buildAntigravitySettings(project)
	allow := settings.Permissions["allow"].([]string)
	want := []string{"command(git status)", "mcp(alpha/)", "mcp(zeta/)"}
	if strings.Join(allow, "\n") != strings.Join(want, "\n") {
		t.Fatalf("allow = %#v, want %#v", allow, want)
	}
}

func TestBuildAntigravityMCPConfig(t *testing.T) {
	t.Parallel()
	enabled := true
	disabled := false
	project := &config.ProjectConfig{
		Env: map[string]string{
			config.BuiltinRepoRootEnvVar: "/repo",
			"AL_TOKEN":                   "token-value",
		},
		Config: config.Config{
			MCP: config.MCPConfig{Servers: []config.MCPServer{
				{
					ID:        "remote",
					Enabled:   &enabled,
					Clients:   []string{antigravityClientID},
					Transport: config.TransportHTTP,
					URL:       "https://mcp.example.com/${AL_TOKEN}",
					Headers:   map[string]string{"Authorization": "Bearer ${AL_TOKEN}"},
				},
				{
					ID:        "stdio",
					Enabled:   &enabled,
					Clients:   []string{antigravityClientID},
					Transport: config.TransportStdio,
					Command:   "./bin/server",
					Args:      []string{"--token", "${AL_TOKEN}"},
					Env:       map[string]string{"TOKEN": "${AL_TOKEN}"}, //nolint:gosec // test data with placeholder syntax
				},
				{
					ID:        "disabled",
					Enabled:   &disabled,
					Clients:   []string{antigravityClientID},
					Transport: config.TransportHTTP,
					URL:       "https://disabled.example.com",
				},
			}},
		},
	}

	cfg, err := buildAntigravityMCPConfig(project)
	if err != nil {
		t.Fatalf("buildAntigravityMCPConfig error: %v", err)
	}
	if _, ok := cfg.Servers["disabled"]; ok {
		t.Fatal("disabled server should not be projected")
	}
	if got := cfg.Servers["remote"].ServerURL; got != "https://mcp.example.com/${AL_TOKEN}" {
		t.Fatalf("remote serverUrl = %q", got)
	}
	if got := cfg.Servers["remote"].Headers["Authorization"]; got != "Bearer ${AL_TOKEN}" {
		t.Fatalf("remote auth header = %q", got)
	}
	if got := cfg.Servers["stdio"].Command; got != "./bin/server" {
		t.Fatalf("stdio command = %q", got)
	}
	if got := cfg.Servers["stdio"].Args[1]; got != "${AL_TOKEN}" {
		t.Fatalf("stdio arg env placeholder = %q", got)
	}
	if got := cfg.Servers["stdio"].Env["TOKEN"]; got != "${AL_TOKEN}" {
		t.Fatalf("stdio env placeholder = %q", got)
	}
}

func TestWriteAntigravityOutputs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll},
			MCP: config.MCPConfig{Servers: []config.MCPServer{
				{ID: "example", Enabled: &enabled, Transport: config.TransportHTTP, URL: "https://example.com", Clients: []string{antigravityClientID}},
			}},
		},
		CommandsAllow: []string{"git status"},
	}

	if err := WriteAntigravitySettings(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteAntigravitySettings error: %v", err)
	}
	if err := WriteAntigravityMCPConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteAntigravityMCPConfig error: %v", err)
	}

	// Existence checks alone (the old test) would still pass if the writers
	// produced empty `{}` files. Assert at least one content-level invariant
	// per file so a regression that breaks the projection but keeps the
	// writers intact still fails the test.
	settings := readFileForTest(t, filepath.Join(root, ".agy", "antigravity-cli", "settings.json"))
	if !strings.Contains(settings, `"command(git status)"`) {
		t.Fatalf("expected allow-list entry in settings.json, got:\n%s", settings)
	}
	if !strings.Contains(settings, `"mcp(example/)"`) {
		t.Fatalf("expected mcp entry in settings.json, got:\n%s", settings)
	}

	mcp := readFileForTest(t, filepath.Join(root, ".agy", "antigravity-cli", "mcp_config.json"))
	if !strings.Contains(mcp, `"mcpServers"`) {
		t.Fatalf("expected mcpServers key in mcp_config.json, got:\n%s", mcp)
	}
	if !strings.Contains(mcp, `"example"`) {
		t.Fatalf("expected example server id keyed in mcp_config.json, got:\n%s", mcp)
	}
}

func TestWriteAntigravityMCPConfigUpdatesMigratedSymlinkTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	legacyPath := filepath.Join(root, ".agy", "antigravity-cli", "mcp_config.json")
	migratedPath := filepath.Join(root, ".agy", "config", "mcp_config.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(migratedPath), 0o700); err != nil {
		t.Fatalf("mkdir migrated dir: %v", err)
	}
	if err := os.WriteFile(migratedPath, []byte(`{"mcpServers":{"old":{}}}`), 0o600); err != nil {
		t.Fatalf("seed migrated config: %v", err)
	}
	if err := os.Symlink(filepath.Join("..", "config", "mcp_config.json"), legacyPath); err != nil {
		t.Fatalf("seed migrated symlink: %v", err)
	}

	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{Servers: []config.MCPServer{
				{ID: "updated", Enabled: &enabled, Transport: config.TransportHTTP, URL: "https://updated.example", Clients: []string{antigravityClientID}},
			}},
		},
	}

	if err := WriteAntigravityMCPConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteAntigravityMCPConfig error: %v", err)
	}

	linkInfo, err := os.Lstat(legacyPath)
	if err != nil {
		t.Fatalf("lstat legacy symlink: %v", err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatal("legacy mcp_config.json should remain a symlink")
	}
	migrated := readFileForTest(t, migratedPath)
	if !strings.Contains(migrated, `"updated"`) {
		t.Fatalf("expected migrated target to be updated, got:\n%s", migrated)
	}
	if strings.Contains(migrated, `"old"`) {
		t.Fatalf("expected old migrated target contents to be replaced, got:\n%s", migrated)
	}
}

func TestWriteAntigravityMCPConfigUsesSystemForMigratedSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	legacyPath := filepath.Join(root, ".agy", "antigravity-cli", "mcp_config.json")
	migratedPath := filepath.Join(root, ".agy", "config", "mcp_config.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(migratedPath), 0o700); err != nil {
		t.Fatalf("mkdir migrated dir: %v", err)
	}
	if err := os.WriteFile(migratedPath, []byte(`{"mcpServers":{"old":{}}}`), 0o600); err != nil {
		t.Fatalf("seed migrated config: %v", err)
	}
	if err := os.Symlink(filepath.Join("..", "config", "mcp_config.json"), legacyPath); err != nil {
		t.Fatalf("seed migrated symlink: %v", err)
	}
	var usedLstat bool
	var usedReadlink bool
	sys := &MockSystem{
		Fallback: RealSystem{},
		LstatFunc: func(name string) (os.FileInfo, error) {
			if name == legacyPath {
				usedLstat = true
			}
			return RealSystem{}.Lstat(name)
		},
		ReadlinkFunc: func(name string) (string, error) {
			if name == legacyPath {
				usedReadlink = true
			}
			return RealSystem{}.Readlink(name)
		},
	}
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{Servers: []config.MCPServer{
				{ID: "updated", Enabled: &enabled, Transport: config.TransportHTTP, URL: "https://updated.example", Clients: []string{antigravityClientID}},
			}},
		},
	}

	if err := WriteAntigravityMCPConfig(sys, root, project); err != nil {
		t.Fatalf("WriteAntigravityMCPConfig error: %v", err)
	}
	if !usedLstat || !usedReadlink {
		t.Fatalf("expected System Lstat and Readlink for migrated symlink, got Lstat=%v Readlink=%v", usedLstat, usedReadlink)
	}
}

func TestWriteAntigravityMCPConfigRejectsEscapedMigratedSymlinkParent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	legacyPath := filepath.Join(root, ".agy", "antigravity-cli", "mcp_config.json")
	configPath := filepath.Join(root, ".agy", "config")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.Symlink(outside, configPath); err != nil {
		t.Fatalf("seed escaped config symlink: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "mcp_config.json"), []byte(`{"mcpServers":{"old":{}}}`), 0o600); err != nil {
		t.Fatalf("seed outside config: %v", err)
	}
	if err := os.Symlink(filepath.Join("..", "config", "mcp_config.json"), legacyPath); err != nil {
		t.Fatalf("seed legacy symlink: %v", err)
	}

	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{Servers: []config.MCPServer{
				{ID: "updated", Enabled: &enabled, Transport: config.TransportHTTP, URL: "https://updated.example", Clients: []string{antigravityClientID}},
			}},
		},
	}

	err := WriteAntigravityMCPConfig(RealSystem{}, root, project)
	if err == nil || !strings.Contains(err.Error(), "resolves outside .agy") {
		t.Fatalf("expected escaped symlink parent error, got %v", err)
	}
	outsideContent := readFileForTest(t, filepath.Join(outside, "mcp_config.json"))
	if strings.Contains(outsideContent, `"updated"`) {
		t.Fatalf("outside config must not be updated, got:\n%s", outsideContent)
	}
}

func TestWriteAntigravityMCPConfigRejectsEscapedDirectMigratedParent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	configPath := filepath.Join(root, ".agy", "config")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("mkdir .agy: %v", err)
	}
	if err := os.Symlink(outside, configPath); err != nil {
		t.Fatalf("seed escaped config symlink: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "mcp_config.json"), []byte(`{"mcpServers":{"old":{}}}`), 0o600); err != nil {
		t.Fatalf("seed outside config: %v", err)
	}

	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{Servers: []config.MCPServer{
				{ID: "updated", Enabled: &enabled, Transport: config.TransportHTTP, URL: "https://updated.example", Clients: []string{antigravityClientID}},
			}},
		},
	}

	err := WriteAntigravityMCPConfig(RealSystem{}, root, project)
	if err == nil || !strings.Contains(err.Error(), "resolves outside .agy") {
		t.Fatalf("expected escaped migrated parent error, got %v", err)
	}
	outsideContent := readFileForTest(t, filepath.Join(outside, "mcp_config.json"))
	if strings.Contains(outsideContent, `"updated"`) {
		t.Fatalf("outside config must not be updated, got:\n%s", outsideContent)
	}
}

func TestWriteAntigravitySettingsRejectsEscapedAgyRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".agy")); err != nil {
		t.Fatalf("seed escaped .agy symlink: %v", err)
	}
	project := &config.ProjectConfig{Config: config.Config{}}

	err := WriteAntigravitySettings(RealSystem{}, root, project)
	if err == nil || !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("expected escaped .agy error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "antigravity-cli", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("outside settings must not be written, stat err = %v", err)
	}
}

func TestCleanAntigravityOutputsRemovesMigratedMCPConfig(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := []string{
		filepath.Join(root, ".agy", "antigravity-cli", "settings.json"),
		filepath.Join(root, ".agy", "config", "mcp_config.json"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
	}
	legacyPath := filepath.Join(root, ".agy", "antigravity-cli", "mcp_config.json")
	if err := os.Symlink(filepath.Join("..", "config", "mcp_config.json"), legacyPath); err != nil {
		t.Fatalf("seed legacy symlink: %v", err)
	}

	if err := CleanAntigravityOutputs(RealSystem{}, root); err != nil {
		t.Fatalf("CleanAntigravityOutputs: %v", err)
	}
	for _, path := range append(paths, legacyPath) {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s removed, lstat err = %v", path, err)
		}
	}
}

func TestCleanAntigravityOutputsRejectsEscapedMigratedParent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	configPath := filepath.Join(root, ".agy", "config")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("mkdir .agy: %v", err)
	}
	if err := os.Symlink(outside, configPath); err != nil {
		t.Fatalf("seed escaped config symlink: %v", err)
	}
	outsideConfig := filepath.Join(outside, "mcp_config.json")
	if err := os.WriteFile(outsideConfig, []byte("{}"), 0o600); err != nil {
		t.Fatalf("seed outside config: %v", err)
	}

	err := CleanAntigravityOutputs(RealSystem{}, root)
	if err == nil || !strings.Contains(err.Error(), "resolves outside .agy") {
		t.Fatalf("expected escaped cleanup error, got %v", err)
	}
	if _, err := os.Stat(outsideConfig); err != nil {
		t.Fatalf("outside config must survive cleanup: %v", err)
	}
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
