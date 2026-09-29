package copilotcli

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func projectMCPConfigArgs(root string) []string {
	return []string{"--additional-mcp-config", "@" + filepath.Join(root, ".copilot", "mcp-config.json")}
}

func writeResolvableCopilot(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "copilot")
	t.Setenv("PATH", binDir)
	return filepath.Join(binDir, "copilot")
}

func TestLaunchCopilotCLIExecHandoff(t *testing.T) {
	root := t.TempDir()
	copilotPath := writeResolvableCopilot(t)
	call := testutil.CaptureExec(t, &execFunc, nil)
	env := []string{"PATH=" + filepath.Dir(copilotPath), "CUSTOM=1"}

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				CopilotCLI: config.AgentConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, []string{"--prompt", "hello"}); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	call.AssertCalled(t, copilotPath, append(append([]string{"copilot", "--model", "test-model"}, projectMCPConfigArgs(root)...), "--prompt", "hello"))
	if !reflect.DeepEqual(call.Env, env) {
		t.Fatalf("expected env to pass through unchanged, got %#v want %#v", call.Env, env)
	}
}

func TestLaunchCopilotCLIExecError(t *testing.T) {
	root := t.TempDir()
	writeResolvableCopilot(t)
	wantErr := errors.New("exec failed")
	testutil.CaptureExec(t, &execFunc, wantErr)

	cfg := &config.ProjectConfig{
		Config: config.Config{},
		Root:   root,
	}

	err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected exec error to wrap %v, got %v", wantErr, err)
	}
	if !strings.Contains(err.Error(), "copilot exec handoff failed") {
		t.Fatalf("expected exec handoff context, got %v", err)
	}
}

func TestLaunchCopilotCLIMissingBinary(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	testutil.ForbidExec(t, &execFunc)

	cfg := &config.ProjectConfig{
		Config: config.Config{},
		Root:   root,
	}

	err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{}, nil)
	if err == nil {
		t.Fatal("expected missing binary error")
	}
	if !strings.Contains(err.Error(), "copilot launcher requires `copilot` on PATH") {
		t.Fatalf("expected lookup error to name copilot, got %v", err)
	}
}

func TestLaunchCopilotCLIYOLO(t *testing.T) {
	root := t.TempDir()
	copilotPath := writeResolvableCopilot(t)
	call := testutil.CaptureExec(t, &execFunc, nil)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO},
			Agents: config.AgentsConfig{
				CopilotCLI: config.AgentConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{}, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	call.AssertCalled(t, copilotPath, append([]string{"copilot", "--model", "test-model", "--yolo"}, projectMCPConfigArgs(root)...))
}

func TestLaunchCopilotCLIAllowAllTools(t *testing.T) {
	root := t.TempDir()
	copilotPath := writeResolvableCopilot(t)
	call := testutil.CaptureExec(t, &execFunc, nil)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll},
			Agents: config.AgentsConfig{
				CopilotCLI: config.AgentConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{}, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	call.AssertCalled(t, copilotPath, append([]string{"copilot", "--model", "test-model", "--allow-all-tools"}, projectMCPConfigArgs(root)...))
}

func TestLaunchCopilotExcludesOnlyUnselectedRootServers(t *testing.T) {
	for _, tc := range []struct {
		name         string
		claude, muse bool
		excluded     []string
	}{
		{name: "neither"},
		{name: "claude", claude: true, excluded: []string{"claude-only"}},
		{name: "muse", muse: true, excluded: []string{"muse-only"}},
		{name: "claude and muse", claude: true, muse: true, excluded: []string{"claude-only", "muse-only"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			binary := writeResolvableCopilot(t)
			call := testutil.CaptureExec(t, &execFunc, nil)
			enabled := true
			claude, muse := tc.claude, tc.muse
			cfg := &config.ProjectConfig{Root: root, Config: config.Config{Agents: config.AgentsConfig{
				Muse: config.AgentConfig{Enabled: &muse}, CopilotCLI: config.AgentConfig{Enabled: &enabled}, ClaudeVSCode: config.EnableOnlyConfig{Enabled: &claude},
			}}}
			for id, selected := range map[string][]string{
				"muse-only": {"muse"}, "claude-only": {"claude"}, "shared": {"claude", "muse", "copilot"}, "copilot-only": {"copilot"}, "other": {"grok"},
			} {
				cfg.Config.MCP.Servers = append(cfg.Config.MCP.Servers, config.MCPServer{ID: id, Enabled: &enabled, Clients: selected, Transport: "stdio", Command: "fixture"})
			}
			env := []string{"CUSTOM=preserved"}
			err := Launch(cfg, &run.Info{Dir: root}, env, []string{"--disable-mcp-server", "personal-disabled"})
			if err != nil {
				t.Fatal(err)
			}
			want := append([]string{"copilot"}, projectMCPConfigArgs(root)...)
			for _, id := range tc.excluded {
				want = append(want, "--disable-mcp-server", id)
			}
			want = append(want, "--disable-mcp-server", "personal-disabled")
			call.AssertCalled(t, binary, want)
			if !reflect.DeepEqual(call.Env, env) {
				t.Fatalf("environment changed: %v", call.Env)
			}
		})
	}
}

func TestLaunchExplicitOptionsReplaceDefaults(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "copilot")
	t.Setenv("PATH", binDir)
	cfg := &config.ProjectConfig{Root: root, Config: config.Config{
		Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO},
		Agents:    config.AgentsConfig{CopilotCLI: config.AgentConfig{Model: "default-model"}},
	}}
	for _, approval := range []string{"--yolo", "--allow-all"} {
		passed := []string{"--model", "chosen", approval, "--additional-mcp-config", "custom"}
		call := testutil.CaptureExec(t, &execFunc, nil)
		if err := Launch(cfg, nil, nil, passed); err != nil {
			t.Fatal(err)
		}
		want := append([]string{"copilot"}, projectMCPConfigArgs(root)...)
		want = append(want, passed...)
		call.AssertCalled(t, filepath.Join(binDir, "copilot"), want)
	}

}

func TestLaunchDeduplicatesGeneratedMCPEntries(t *testing.T) {
	root := t.TempDir()
	binary := writeResolvableCopilot(t)
	call := testutil.CaptureExec(t, &execFunc, nil)
	enabled := true
	cfg := &config.ProjectConfig{Root: root, Config: config.Config{
		Agents: config.AgentsConfig{Claude: config.ClaudeConfig{Enabled: &enabled}, CopilotCLI: config.AgentConfig{Enabled: &enabled}},
		MCP:    config.MCPConfig{Servers: []config.MCPServer{{ID: "claude-only", Enabled: &enabled, Clients: []string{"claude"}, Transport: "stdio", Command: "fixture"}}},
	}}
	passed := []string{"--additional-mcp-config=@" + filepath.Join(root, ".copilot", "mcp-config.json"), "--disable-mcp-server=claude-only", "--disable-mcp-server", "personal"}
	if err := Launch(cfg, nil, nil, passed); err != nil {
		t.Fatal(err)
	}
	call.AssertCalled(t, binary, append([]string{"copilot"}, passed...))
}
