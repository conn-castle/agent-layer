package sync

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
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
	permissions, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions map, got %#v", settings["permissions"])
	}
	allow := permissions["allow"].([]string)
	want := []string{"command(git status)", "mcp(alpha/)", "mcp(zeta/)"}
	if strings.Join(allow, "\n") != strings.Join(want, "\n") {
		t.Fatalf("allow = %#v, want %#v", allow, want)
	}
}

func TestBuildAntigravitySettingsAgentSpecificMerges(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll},
			Agents: config.AgentsConfig{
				Antigravity: config.AntigravityConfig{
					Enabled: &enabled,
					AgentSpecific: map[string]any{
						"features": map[string]any{
							"example_feature": true,
						},
						"permissions": map[string]any{
							"deny": []string{"command(rm -rf /)"},
						},
					},
				},
			},
		},
		CommandsAllow: []string{"git status"},
	}

	settings := buildAntigravitySettings(project)
	features, ok := settings["features"].(map[string]any)
	if !ok {
		t.Fatalf("expected features map from agent_specific, got %#v", settings["features"])
	}
	if value, ok := features["example_feature"].(bool); !ok || !value {
		t.Fatalf("expected features.example_feature=true, got %v", features["example_feature"])
	}
	permissions, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions map, got %#v", settings["permissions"])
	}
	// Managed allow must survive when agent_specific only touches deny.
	if allow, ok := permissions["allow"].([]string); !ok || len(allow) == 0 {
		t.Fatalf("expected managed permissions.allow to survive deep merge, got %#v", permissions["allow"])
	}
	deny, ok := permissions["deny"].([]string)
	if !ok || len(deny) != 1 || deny[0] != "command(rm -rf /)" {
		t.Fatalf("expected agent_specific permissions.deny, got %#v", permissions["deny"])
	}
}

func TestBuildAntigravitySettingsModel(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Antigravity: config.AntigravityConfig{
					Model: "Gemini 3.1 Pro (High)",
				},
			},
		},
	}

	settings := buildAntigravitySettings(project)
	if settings["model"] != "Gemini 3.1 Pro (High)" {
		t.Fatalf("model = %#v, want Gemini 3.1 Pro (High)", settings["model"])
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

	if err := writeAntigravitySettings(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeAntigravitySettings error: %v", err)
	}
	if err := writeAntigravityMCPConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeAntigravityMCPConfig error: %v", err)
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

func TestWriteAntigravitySettingsPreservesNativeStateAndRefreshesManagedPaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, ".agy", "antigravity-cli", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	seed := `{"model":"old","permissions":{"allow":["old"],"nativeDeny":["keep"]},"trust":{"approved":true,"counter":900719925474099312345},"features":{"native":true}}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	project := &config.ProjectConfig{Config: config.Config{
		Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll},
		Agents: config.AgentsConfig{Antigravity: config.AntigravityConfig{
			Model: "new", AgentSpecific: map[string]any{"features": map[string]any{"managed": "first"}},
		}},
	}, CommandsAllow: []string{"git status"}}
	if err := writeAntigravitySettings(RealSystem{}, root, project); err != nil {
		t.Fatal(err)
	}
	project.Config.Agents.Antigravity.Model = "newer"
	project.Config.Agents.Antigravity.AgentSpecific = map[string]any{"features": map[string]any{"managed": "second"}}
	if err := writeAntigravitySettings(RealSystem{}, root, project); err != nil {
		t.Fatal(err)
	}
	got := readFileForTest(t, path)
	for _, want := range []string{`"model": "newer"`, `"command(git status)"`, `"approved": true`, `900719925474099312345`, `"native": true`, `"managed": "second"`, `"nativeDeny"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("settings missing %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, `"old"`) {
		t.Fatalf("stale managed model/permissions.allow value was not refreshed:\n%s", got)
	}
}

func TestWriteAntigravitySettingsPreservesNativeManagedPathsWhenConfigOmitsThem(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, ".agy", "antigravity-cli", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// settings.json is owned by Antigravity and the user. When Agent Layer
	// config produces no model or permissions, it must not delete the native
	// values the user set in Antigravity itself.
	seed := []byte(`{"model":"native-model","permissions":{"allow":["native-allow"],"deny":["native"]},"trust":true}`)
	if err := os.WriteFile(path, seed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeAntigravitySettings(RealSystem{}, root, &config.ProjectConfig{}); err != nil {
		t.Fatal(err)
	}
	got := readFileForTest(t, path)
	for _, want := range []string{`"native-model"`, `"native-allow"`, `"deny"`, `"native"`, `"trust"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("native value %s was not preserved:\n%s", want, got)
		}
	}
}

func TestWriteAntigravitySettingsRejectsInvalidStateBeforeWrite(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, seed string }{
		{"malformed", `{"trust":`},
		{"non-object", `[]`},
		{"null", `null`},
		{"trailing", `{} {}`},
		{"shape-conflict", `{"permissions":"native"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".agy", "antigravity-cli", "settings.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			before := []byte(tc.seed)
			if err := os.WriteFile(path, before, 0o600); err != nil {
				t.Fatal(err)
			}
			// Approvals mode all makes buildPermissionsBlock emit permissions.allow,
			// so the shape-conflict seed is a path Agent Layer actually writes.
			project := &config.ProjectConfig{Config: config.Config{Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll}}, CommandsAllow: []string{"git status"}}
			if err := writeAntigravitySettings(RealSystem{}, root, project); err == nil {
				t.Fatal("expected error")
			}
			after, err := os.ReadFile(path) // #nosec G304 -- test-owned path.
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("file changed on failure: %q", after)
			}
		})
	}
}

func TestWriteAntigravitySettingsRejectsSymlinkAndNonRegularTarget(t *testing.T) {
	t.Parallel()
	t.Run("symlink", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, ".agy", "antigravity-cli", "settings.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, "native.json")
		before := []byte(`{"trust":true}`)
		if err := os.WriteFile(target, before, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		err := writeAntigravitySettings(RealSystem{}, root, &config.ProjectConfig{})
		if err == nil || !strings.Contains(err.Error(), "must be a regular file") {
			t.Fatalf("expected regular-file guard error, got %v", err)
		}
		after, err := os.ReadFile(target) // #nosec G304 -- test-owned path.
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, before) {
			t.Fatal("symlink target changed")
		}
	})
	t.Run("directory", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, ".agy", "antigravity-cli", "settings.json")
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		err := writeAntigravitySettings(RealSystem{}, root, &config.ProjectConfig{})
		if err == nil || !strings.Contains(err.Error(), "must be a regular file") {
			t.Fatalf("expected regular-file guard error, got %v", err)
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			t.Fatalf("target changed: %v", err)
		}
	})
}

func TestWriteAntigravitySettingsReadErrorDoesNotWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, ".agy", "antigravity-cli", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	before := []byte(`{"trust":true}`)
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	wrote := false
	sys := &MockSystem{
		Fallback: RealSystem{},
		ReadFileFunc: func(string) ([]byte, error) {
			return nil, errors.New("permission denied")
		},
		WriteFileAtomicFunc: func(string, []byte, os.FileMode) error {
			wrote = true
			return nil
		},
	}
	if err := writeAntigravitySettings(sys, root, &config.ProjectConfig{}); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected read error, got %v", err)
	}
	if wrote {
		t.Fatal("write attempted after read failure")
	}
	after, err := os.ReadFile(path) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("file changed on read failure: %q", after)
	}
}

func TestWriteAntigravitySettingsRejectsManagedLeafShapeConflict(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, ".agy", "antigravity-cli", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// Native model is an object, but Agent Layer overlays a scalar model.
	// Overlaying a scalar onto an object is an incompatible shape and must fail
	// loud rather than silently reshape native state.
	before := []byte(`{"model":{"native":true}}`)
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	project := &config.ProjectConfig{Config: config.Config{Agents: config.AgentsConfig{Antigravity: config.AntigravityConfig{Model: "gemini"}}}}
	err := writeAntigravitySettings(RealSystem{}, root, project)
	if err == nil || !strings.Contains(err.Error(), "incompatible existing shape") {
		t.Fatalf("expected shape conflict error, got %v", err)
	}
	after, err := os.ReadFile(path) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("file changed on shape conflict: %q", after)
	}
}

func TestWriteAntigravitySettingsTreatsEmptyFileAsFresh(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, seed string }{
		{"empty", ""},
		{"whitespace", "  \n\t "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".agy", "antigravity-cli", "settings.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.seed), 0o600); err != nil {
				t.Fatal(err)
			}
			project := &config.ProjectConfig{Config: config.Config{Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll}}, CommandsAllow: []string{"git status"}}
			if err := writeAntigravitySettings(RealSystem{}, root, project); err != nil {
				t.Fatalf("empty file should be treated as fresh, got %v", err)
			}
			got := readFileForTest(t, path)
			if !strings.Contains(got, `"command(git status)"`) {
				t.Fatalf("expected managed content written over empty file, got:\n%s", got)
			}
		})
	}
}

func TestWriteAntigravitySettingsPreservesUnmanagedNonObjectValue(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, ".agy", "antigravity-cli", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// Native permissions is a non-object value and Agent Layer produces no
	// permissions here, so the merge must not inspect or reject the native
	// shape — it targets nothing at that path.
	seed := []byte(`{"permissions":null,"trust":true}`)
	if err := os.WriteFile(path, seed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeAntigravitySettings(RealSystem{}, root, &config.ProjectConfig{}); err != nil {
		t.Fatalf("unmanaged non-object native value must not error, got %v", err)
	}
	got := readFileForTest(t, path)
	for _, want := range []string{`"permissions": null`, `"trust": true`} {
		if !strings.Contains(got, want) {
			t.Fatalf("native value %s not preserved:\n%s", want, got)
		}
	}
}

func TestWriteAntigravitySettingsPreservesFileMode(t *testing.T) {
	t.Parallel()
	t.Run("preserves existing mode", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, ".agy", "antigravity-cli", "settings.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"trust":true}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := writeAntigravitySettings(RealSystem{}, root, &config.ProjectConfig{}); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("existing 0600 mode widened to %o", perm)
		}
	})
	t.Run("new file is owner-only", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, ".agy", "antigravity-cli", "settings.json")
		project := &config.ProjectConfig{Config: config.Config{Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll}}, CommandsAllow: []string{"git status"}}
		if err := writeAntigravitySettings(RealSystem{}, root, project); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("new settings.json mode = %o, want 0600", perm)
		}
	})
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

	if err := writeAntigravityMCPConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeAntigravityMCPConfig error: %v", err)
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

	if err := writeAntigravityMCPConfig(sys, root, project); err != nil {
		t.Fatalf("writeAntigravityMCPConfig error: %v", err)
	}
	if !usedLstat || !usedReadlink {
		t.Fatalf("expected System Lstat and Readlink for migrated symlink, got Lstat=%v Readlink=%v", usedLstat, usedReadlink)
	}
}

func TestWriteAntigravityMCPConfigUsesSystemForMigratedPathStat(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	migratedPath := filepath.Join(root, ".agy", "config", "mcp_config.json")
	if err := os.MkdirAll(filepath.Dir(migratedPath), 0o700); err != nil {
		t.Fatalf("mkdir migrated dir: %v", err)
	}
	if err := os.WriteFile(migratedPath, []byte(`{"mcpServers":{"old":{}}}`), 0o600); err != nil {
		t.Fatalf("seed migrated config: %v", err)
	}
	var usedMigratedStat bool
	sys := &MockSystem{
		Fallback: RealSystem{},
		StatFunc: func(name string) (os.FileInfo, error) {
			if name == migratedPath {
				usedMigratedStat = true
			}
			return RealSystem{}.Stat(name)
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

	if err := writeAntigravityMCPConfig(sys, root, project); err != nil {
		t.Fatalf("writeAntigravityMCPConfig error: %v", err)
	}
	if !usedMigratedStat {
		t.Fatal("expected System Stat for migrated Antigravity MCP config path")
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

	err := writeAntigravityMCPConfig(RealSystem{}, root, project)
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

	err := writeAntigravityMCPConfig(RealSystem{}, root, project)
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

	err := writeAntigravitySettings(RealSystem{}, root, project)
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

	if err := cleanAntigravityOutputs(RealSystem{}, root); err != nil {
		t.Fatalf("cleanAntigravityOutputs: %v", err)
	}
	if _, err := os.Lstat(paths[0]); err != nil {
		t.Fatalf("expected shared settings preserved, lstat err = %v", err)
	}
	for _, path := range []string{paths[1], legacyPath} {
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

	err := cleanAntigravityOutputs(RealSystem{}, root)
	if err == nil || !strings.Contains(err.Error(), "resolves outside .agy") {
		t.Fatalf("expected escaped cleanup error, got %v", err)
	}
	if _, err := os.Stat(outsideConfig); err != nil {
		t.Fatalf("outside config must survive cleanup: %v", err)
	}
}

func TestWriteAntigravityChimePluginWritesManagedFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}},
	}

	if err := writeAntigravityChimePlugin(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeAntigravityChimePlugin: %v", err)
	}
	dir := antigravityChimePluginDir(root)
	plugin := readFileForTest(t, filepath.Join(dir, "plugin.json"))
	hooks := readFileForTest(t, filepath.Join(dir, "hooks.json"))
	if !strings.Contains(plugin, `"name": "agent-layer-chime"`) {
		t.Fatalf("expected plugin name, got:\n%s", plugin)
	}
	for _, want := range []string{`"agent-layer-chime"`, `"Stop"`, agentLayerChimeMarker, `"timeout": 5`, `\"decision\":\"allow\"`} {
		if !strings.Contains(hooks, want) {
			t.Fatalf("expected %q in hooks.json, got:\n%s", want, hooks)
		}
	}
	var pluginConfig map[string]string
	if err := json.Unmarshal([]byte(plugin), &pluginConfig); err != nil {
		t.Fatalf("plugin.json must be valid JSON: %v\n%s", err, plugin)
	}
	if pluginConfig["name"] != antigravityChimePluginName {
		t.Fatalf("plugin name = %q, want %q", pluginConfig["name"], antigravityChimePluginName)
	}
	var hooksConfig map[string]struct {
		Enabled bool `json:"enabled"`
		Stop    []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
			Timeout int    `json:"timeout"`
		} `json:"Stop"`
	}
	if err := json.Unmarshal([]byte(hooks), &hooksConfig); err != nil {
		t.Fatalf("hooks.json must be valid JSON: %v\n%s", err, hooks)
	}
	chimeConfig := hooksConfig[antigravityChimePluginName]
	if !chimeConfig.Enabled || len(chimeConfig.Stop) != 1 {
		t.Fatalf("unexpected chime hook config: %#v", chimeConfig)
	}
	if got := chimeConfig.Stop[0]; got.Type != "command" || got.Command != agentLayerAntigravityChimeCommand || got.Timeout != agentLayerChimeTimeout {
		t.Fatalf("unexpected Stop hook: %#v", got)
	}
}

func TestCleanAntigravityChimePluginRemovesOnlyManagedPlugin(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}},
	}
	if err := writeAntigravityChimePlugin(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeAntigravityChimePlugin: %v", err)
	}
	otherPlugin := filepath.Join(root, ".agents", "plugins", "user-plugin", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(otherPlugin), 0o700); err != nil {
		t.Fatalf("mkdir user plugin: %v", err)
	}
	if err := os.WriteFile(otherPlugin, []byte(`{"name":"user-plugin"}`), 0o600); err != nil {
		t.Fatalf("write user plugin: %v", err)
	}

	if err := cleanAntigravityChimePlugin(RealSystem{}, root); err != nil {
		t.Fatalf("cleanAntigravityChimePlugin: %v", err)
	}
	if _, err := os.Lstat(antigravityChimePluginDir(root)); !os.IsNotExist(err) {
		t.Fatalf("expected chime plugin removed, lstat err=%v", err)
	}
	if _, err := os.Stat(otherPlugin); err != nil {
		t.Fatalf("expected unrelated plugin preserved: %v", err)
	}
}

func TestWriteAntigravityChimePluginMigratesLegacyArtifactSet(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := antigravityChimePluginDir(root)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir plugin: %v", err)
	}
	for _, file := range legacyAntigravityChimePluginFiles() {
		if err := os.WriteFile(filepath.Join(dir, file.name), file.data, 0o600); err != nil {
			t.Fatalf("write legacy %s: %v", file.name, err)
		}
	}
	enabled := true
	project := &config.ProjectConfig{Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}}}
	if err := writeAntigravityChimePlugin(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeAntigravityChimePlugin: %v", err)
	}
	hooks := readFileForTest(t, filepath.Join(dir, "hooks.json"))
	if strings.Contains(hooks, "/usr/bin/afplay") || !strings.Contains(hooks, "al hook chime antigravity") {
		t.Fatalf("legacy plugin was not migrated:\n%s", hooks)
	}
}

func TestCleanAntigravityChimePluginRemovesLegacyArtifactSet(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := antigravityChimePluginDir(root)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir plugin: %v", err)
	}
	for _, file := range legacyAntigravityChimePluginFiles() {
		if err := os.WriteFile(filepath.Join(dir, file.name), file.data, 0o600); err != nil {
			t.Fatalf("write legacy %s: %v", file.name, err)
		}
	}
	if err := cleanAntigravityChimePlugin(RealSystem{}, root); err != nil {
		t.Fatalf("cleanAntigravityChimePlugin: %v", err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected legacy plugin removed, lstat err=%v", err)
	}
}

func TestWriteAntigravityChimePluginDisabledCleansManagedPlugin(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}},
	}
	if err := writeAntigravityChimePlugin(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeAntigravityChimePlugin enabled: %v", err)
	}

	disabled := false
	project.Config.Notifications.Chime = &disabled
	if err := writeAntigravityChimePlugin(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeAntigravityChimePlugin disabled: %v", err)
	}
	if _, err := os.Lstat(antigravityChimePluginDir(root)); !os.IsNotExist(err) {
		t.Fatalf("expected disabled chime to remove managed plugin, lstat err=%v", err)
	}
}

func TestCleanAntigravityChimePluginNoopWhenMissing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	if err := cleanAntigravityChimePlugin(RealSystem{}, root); err != nil {
		t.Fatalf("cleanAntigravityChimePlugin missing plugin: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".agents")); !os.IsNotExist(err) {
		t.Fatalf("cleanup of missing chime plugin should not create .agents, lstat err=%v", err)
	}
}

func TestCleanAntigravityChimePluginRejectsSymlinkAgentsParentWhenPluginMissing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".agents")); err != nil {
		t.Fatalf("seed .agents symlink: %v", err)
	}

	err := cleanAntigravityChimePlugin(RealSystem{}, root)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected symlink parent cleanup conflict, got %v", err)
	}
}

func TestCleanAntigravityChimePluginRejectsUserOwnedPluginDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := antigravityChimePluginDir(root)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	pluginPath := filepath.Join(dir, "plugin.json")
	if err := os.WriteFile(pluginPath, []byte(`{"name":"mine"}`), 0o600); err != nil {
		t.Fatalf("write user plugin: %v", err)
	}

	err := cleanAntigravityChimePlugin(RealSystem{}, root)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected user-owned plugin cleanup conflict, got %v", err)
	}
	if got := readFileForTest(t, pluginPath); !strings.Contains(got, `"mine"`) {
		t.Fatalf("expected user-owned plugin preserved, got:\n%s", got)
	}
}

func TestCleanAntigravityChimePluginRejectsManagedPluginWithExtraFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}},
	}
	if err := writeAntigravityChimePlugin(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeAntigravityChimePlugin: %v", err)
	}
	extraPath := filepath.Join(antigravityChimePluginDir(root), "notes.txt")
	if err := os.WriteFile(extraPath, []byte("user data"), 0o600); err != nil {
		t.Fatalf("write extra plugin file: %v", err)
	}

	err := cleanAntigravityChimePlugin(RealSystem{}, root)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected augmented plugin cleanup conflict, got %v", err)
	}
	if got := readFileForTest(t, extraPath); got != "user data" {
		t.Fatalf("expected extra plugin file preserved, got %q", got)
	}
}

func TestCleanAntigravityChimePluginRejectsIncompletePluginDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := antigravityChimePluginDir(root)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}

	err := cleanAntigravityChimePlugin(RealSystem{}, root)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected incomplete plugin cleanup conflict, got %v", err)
	}
	if _, err := os.Lstat(dir); err != nil {
		t.Fatalf("expected incomplete plugin dir preserved: %v", err)
	}
}

func TestCleanAntigravityChimePluginReadDirErrorPreservesPlugin(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}},
	}
	if err := writeAntigravityChimePlugin(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeAntigravityChimePlugin: %v", err)
	}
	readDirErr := errors.New("readdir denied")
	sys := &MockSystem{
		Fallback: RealSystem{},
		ReadDirFunc: func(name string) ([]os.DirEntry, error) {
			if name == antigravityChimePluginDir(root) {
				return nil, readDirErr
			}
			return RealSystem{}.ReadDir(name)
		},
	}

	err := cleanAntigravityChimePlugin(sys, root)
	if err == nil || !strings.Contains(err.Error(), "readdir denied") {
		t.Fatalf("expected plugin ReadDir error, got %v", err)
	}
	if _, err := os.Lstat(antigravityChimePluginDir(root)); err != nil {
		t.Fatalf("expected plugin preserved after ReadDir error: %v", err)
	}
}

func TestCleanAntigravityChimePluginStatErrorFailsLoud(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	statErr := errors.New("stat denied")
	sys := &MockSystem{
		Fallback: RealSystem{},
		LstatFunc: func(name string) (os.FileInfo, error) {
			if name == antigravityChimePluginDir(root) {
				return nil, statErr
			}
			return RealSystem{}.Lstat(name)
		},
	}

	err := cleanAntigravityChimePlugin(sys, root)
	if err == nil || !strings.Contains(err.Error(), "stat denied") {
		t.Fatalf("expected plugin stat error, got %v", err)
	}
}

func TestCleanAntigravityChimePluginRejectsNonDirectoryPluginPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := antigravityChimePluginDir(root)
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		t.Fatalf("mkdir plugins dir: %v", err)
	}
	if err := os.WriteFile(dir, []byte("not a plugin directory"), 0o600); err != nil {
		t.Fatalf("write plugin path file: %v", err)
	}

	err := cleanAntigravityChimePlugin(RealSystem{}, root)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected non-directory plugin cleanup conflict, got %v", err)
	}
	if got := readFileForTest(t, dir); got != "not a plugin directory" {
		t.Fatalf("expected plugin path file preserved, got %q", got)
	}
}

func TestCleanAntigravityChimePluginRejectsSymlinkAgentsParent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}},
	}
	if err := writeAntigravityChimePlugin(RealSystem{}, outside, project); err != nil {
		t.Fatalf("seed outside chime plugin: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, ".agents"), filepath.Join(root, ".agents")); err != nil {
		t.Fatalf("seed .agents symlink: %v", err)
	}

	err := cleanAntigravityChimePlugin(RealSystem{}, root)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected symlink parent cleanup conflict, got %v", err)
	}
	if _, err := os.Lstat(antigravityChimePluginDir(outside)); err != nil {
		t.Fatalf("outside plugin must be preserved: %v", err)
	}
}

func TestCleanAntigravityChimePluginRemoveErrorPreservesPlugin(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}},
	}
	if err := writeAntigravityChimePlugin(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeAntigravityChimePlugin: %v", err)
	}
	removeErr := errors.New("remove denied")
	sys := &MockSystem{
		Fallback: RealSystem{},
		RemoveAllFunc: func(path string) error {
			if path == antigravityChimePluginDir(root) {
				return removeErr
			}
			return RealSystem{}.RemoveAll(path)
		},
	}

	err := cleanAntigravityChimePlugin(sys, root)
	if err == nil || !strings.Contains(err.Error(), "remove denied") {
		t.Fatalf("expected remove error, got %v", err)
	}
	if _, err := os.Lstat(antigravityChimePluginDir(root)); err != nil {
		t.Fatalf("expected plugin preserved after remove error: %v", err)
	}
}

func TestWriteAntigravityChimePluginRejectsUserOwnedPluginDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := antigravityChimePluginDir(root)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"name":"mine"}`), 0o600); err != nil {
		t.Fatalf("write plugin: %v", err)
	}
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}},
	}

	err := writeAntigravityChimePlugin(RealSystem{}, root, project)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected user-owned plugin conflict, got %v", err)
	}
}

func TestWriteAntigravityChimePluginRejectsSymlinkPluginDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	dir := antigravityChimePluginDir(root)
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		t.Fatalf("mkdir plugins dir: %v", err)
	}
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatalf("seed plugin symlink: %v", err)
	}
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}},
	}

	err := writeAntigravityChimePlugin(RealSystem{}, root, project)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected symlink plugin conflict, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "plugin.json")); !os.IsNotExist(err) {
		t.Fatalf("outside plugin must not be written, stat err=%v", err)
	}
}

func TestAntigravityChimePluginRejectsSymlinkedManagedManifests(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"write", "clean"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			dir := antigravityChimePluginDir(root)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatalf("mkdir plugin dir: %v", err)
			}
			outside := t.TempDir()
			for _, file := range antigravityChimePluginFiles() {
				target := filepath.Join(outside, file.name)
				if err := os.WriteFile(target, file.data, 0o600); err != nil {
					t.Fatalf("write outside %s: %v", file.name, err)
				}
				if err := os.Symlink(target, filepath.Join(dir, file.name)); err != nil {
					t.Fatalf("symlink %s: %v", file.name, err)
				}
			}

			var err error
			if action == "write" {
				enabled := true
				project := &config.ProjectConfig{Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}}}
				err = writeAntigravityChimePlugin(RealSystem{}, root, project)
			} else {
				err = cleanAntigravityChimePlugin(RealSystem{}, root)
			}
			if err == nil || !strings.Contains(err.Error(), "not the Agent Layer-managed chime plugin") {
				t.Fatalf("%s error = %v, want ownership conflict", action, err)
			}
			for _, file := range antigravityChimePluginFiles() {
				link := filepath.Join(dir, file.name)
				info, statErr := os.Lstat(link)
				if statErr != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("%s must preserve symlink %s, info=%v err=%v", action, file.name, info, statErr)
				}
				target := filepath.Join(outside, file.name)
				if got := readFileForTest(t, target); got != string(file.data) {
					t.Fatalf("%s changed outside %s: %q", action, file.name, got)
				}
			}
		})
	}
}

func TestWriteAntigravityChimePluginRejectsNonDirectoryPluginPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := antigravityChimePluginDir(root)
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		t.Fatalf("mkdir plugins dir: %v", err)
	}
	if err := os.WriteFile(dir, []byte("not a plugin directory"), 0o600); err != nil {
		t.Fatalf("write plugin path file: %v", err)
	}
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}},
	}

	err := writeAntigravityChimePlugin(RealSystem{}, root, project)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected non-directory plugin conflict, got %v", err)
	}
	if got := readFileForTest(t, dir); got != "not a plugin directory" {
		t.Fatalf("expected plugin path file preserved, got %q", got)
	}
}

func TestWriteAntigravityChimePluginStatErrorFailsLoud(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}},
	}
	statErr := errors.New("stat denied")
	target := antigravityChimePluginDir(root)
	targetStats := 0
	sys := &MockSystem{
		Fallback: RealSystem{},
		LstatFunc: func(name string) (os.FileInfo, error) {
			if name == target {
				targetStats++
				if targetStats == 1 {
					return nil, os.ErrNotExist
				}
				return nil, statErr
			}
			return RealSystem{}.Lstat(name)
		},
	}

	err := writeAntigravityChimePlugin(sys, root, project)
	if err == nil || !strings.Contains(err.Error(), "stat denied") {
		t.Fatalf("expected plugin stat error, got %v", err)
	}
}

func TestEnsureAntigravityChimePathContainedRejectsSiblingPrefix(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, ".agents", "plugins-other", antigravityChimePluginName)

	err := ensureAntigravityChimePathContained(RealSystem{}, root, target)
	if err == nil || !strings.Contains(err.Error(), "outside .agents/plugins") {
		t.Fatalf("expected sibling prefix containment error, got %v", err)
	}
}

func TestEnsureAntigravityChimePathContainedStatErrorFailsLoud(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	statErr := errors.New("stat denied")
	agentsDir := filepath.Join(root, ".agents")
	sys := &MockSystem{
		Fallback: RealSystem{},
		LstatFunc: func(name string) (os.FileInfo, error) {
			if name == agentsDir {
				return nil, statErr
			}
			return RealSystem{}.Lstat(name)
		},
	}

	err := ensureAntigravityChimePathContained(sys, root, antigravityChimePluginDir(root))
	if err == nil || !strings.Contains(err.Error(), "stat denied") {
		t.Fatalf("expected parent stat error, got %v", err)
	}
}

func TestWriteAntigravityChimePluginCreateDirErrorDoesNotWritePlugin(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}},
	}
	mkdirErr := errors.New("mkdir denied")
	wrote := false
	sys := &MockSystem{
		Fallback: RealSystem{},
		MkdirAllFunc: func(path string, perm os.FileMode) error {
			if path == antigravityChimePluginDir(root) {
				return mkdirErr
			}
			return RealSystem{}.MkdirAll(path, perm)
		},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			wrote = true
			return RealSystem{}.WriteFileAtomic(filename, data, perm)
		},
	}

	err := writeAntigravityChimePlugin(sys, root, project)
	if err == nil || !strings.Contains(err.Error(), "mkdir denied") {
		t.Fatalf("expected create dir error, got %v", err)
	}
	if wrote {
		t.Fatal("expected create dir failure to abort before writing plugin files")
	}
}

func TestWriteAntigravityChimePluginWriteErrorReportsTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}},
	}
	writeErr := errors.New("write denied")
	sys := &MockSystem{
		Fallback: RealSystem{},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			return writeErr
		},
	}

	err := writeAntigravityChimePlugin(sys, root, project)
	if err == nil || !strings.Contains(err.Error(), filepath.Join("agent-layer-chime", "plugin.json")) || !strings.Contains(err.Error(), "write denied") {
		t.Fatalf("expected plugin write error with target path, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(antigravityChimePluginDir(root), "plugin.json")); !os.IsNotExist(err) {
		t.Fatalf("failed plugin write should not leave plugin.json, lstat err=%v", err)
	}
}

func TestWriteAntigravityChimePluginSecondWriteFailureRollsBackFreshPlugin(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}},
	}
	writeErr := errors.New("hooks write denied")
	sys := &MockSystem{
		Fallback: RealSystem{},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			if filepath.Base(filename) == "hooks.json" {
				if err := (RealSystem{}).WriteFileAtomic(filename, data, perm); err != nil {
					return err
				}
				return writeErr
			}
			return RealSystem{}.WriteFileAtomic(filename, data, perm)
		},
	}

	err := writeAntigravityChimePlugin(sys, root, project)
	if !errors.Is(err, writeErr) {
		t.Fatalf("writeAntigravityChimePlugin error = %v, want hooks write failure", err)
	}
	if _, err := os.Lstat(antigravityChimePluginDir(root)); !os.IsNotExist(err) {
		t.Fatalf("fresh partial plugin must be rolled back, lstat err=%v", err)
	}
	if err := writeAntigravityChimePlugin(RealSystem{}, root, project); err != nil {
		t.Fatalf("retry after rolled-back write failure: %v", err)
	}
}

func TestAntigravityChimePluginValidateWhenAgyAvailable(t *testing.T) {
	t.Parallel()
	agy, err := exec.LookPath("agy")
	if err != nil {
		t.Skip("agy not available on PATH")
	}
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}},
	}
	if err := writeAntigravityChimePlugin(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeAntigravityChimePlugin: %v", err)
	}
	cmd := exec.Command(agy, "plugin", "validate", antigravityChimePluginDir(root))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("agy plugin validate failed: %v\n%s", err, string(out))
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
