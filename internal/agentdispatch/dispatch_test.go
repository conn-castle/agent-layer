package agentdispatch

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestBuildOptionsJSONShapeAndRandomExclusion(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{AntigravityModel: "Gemini 3.1 Pro (High)"})
	options, err := BuildOptions(OptionsRequest{
		Root: root,
		Env:  []string{clients.EnvDispatchCallerAgent + "=" + AgentClaude},
		LookPath: func(string) (string, error) {
			return "/bin/mock", nil
		},
	})
	if err != nil {
		t.Fatalf("BuildOptions error: %v", err)
	}
	if !options.Caller.Known || options.Caller.Agent != AgentClaude {
		t.Fatalf("unexpected caller: %#v", options.Caller)
	}
	if strings.Join(options.Random.Pool, ",") != "codex,antigravity" {
		t.Fatalf("unexpected random pool: %#v", options.Random.Pool)
	}
	var claude TargetOption
	for _, target := range options.Targets {
		if target.Agent == AgentClaude {
			claude = target
		}
	}
	if claude.RandomExclusionReason == nil || *claude.RandomExclusionReason != "caller" {
		t.Fatalf("expected claude caller exclusion, got %#v", claude.RandomExclusionReason)
	}
	if !claude.Model.OverrideSupported || len(claude.Model.Suggestions) == 0 || !claude.Model.AllowCustom {
		t.Fatalf("unexpected claude model metadata: %#v", claude.Model)
	}
	var agy TargetOption
	for _, target := range options.Targets {
		if target.Agent == AgentAntigravity {
			agy = target
		}
	}
	if !agy.Model.OverrideSupported || agy.Model.Configured != "Gemini 3.1 Pro (High)" || !agy.Model.AllowCustom {
		t.Fatalf("unexpected antigravity model metadata: %#v", agy.Model)
	}
	if !slices.Contains(agy.Model.Suggestions, "Gemini 3.1 Pro (High)") {
		t.Fatalf("unexpected antigravity model suggestions: %#v", agy.Model.Suggestions)
	}
	if agy.ReasoningEffort.OverrideSupported {
		t.Fatalf("antigravity reasoning_effort should remain unsupported: %#v", agy.ReasoningEffort)
	}
}

func TestBuildOptionsUsesTargetModelSuggestionProvider(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "agy.log")
	writeDispatchStub(t, binDir, "agy", `if [ "$1" = "models" ]; then
  printf 'Live Antigravity Model\nBackup Antigravity Model\n'
fi`)

	options, err := BuildOptions(OptionsRequest{
		Root: root,
		Env: []string{
			"PATH=" + testPath(binDir),
			"AL_TEST_LOG=" + logPath,
		},
		LookPath: mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("BuildOptions error: %v", err)
	}
	var agy TargetOption
	for _, target := range options.Targets {
		if target.Agent == AgentAntigravity {
			agy = target
		}
	}
	got := strings.Join(agy.Model.Suggestions, ",")
	if got != "Live Antigravity Model,Backup Antigravity Model" {
		t.Fatalf("antigravity suggestions = %q", got)
	}
	assertFileContains(t, logPath, "ARG_0=models")
	assertFileContains(t, logPath, "AGY_CLI_DISABLE_AUTO_UPDATE=1")
}

func TestRunBlocksNestedDispatchAtDefaultDepth(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	err := Run(RunOptions{
		Root: root,
		Env:  []string{clients.EnvDispatchActive + "=1"},
	})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitNested {
		t.Fatalf("expected nested exit, got %T: %v", err, err)
	}
}

func TestRunAllowsNestedDispatchWithinConfiguredDepth(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 2})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "codex.log")
	writeDispatchStub(t, binDir, "codex", `printf '{"type":"agent_message","message":"codex ok"}\n'`)
	var stdout bytes.Buffer
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentCodex,
		PromptArgs: []string{"Review"},
		Env: []string{
			"PATH=" + testPath(binDir),
			clients.EnvDispatchActive + "=1",
			"AL_TEST_LOG=" + logPath,
		},
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		LookPath: mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if stdout.String() != "codex ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	assertFileContains(t, logPath, clients.EnvDispatchActive+"=2")
}

func TestRunBlocksNestedDispatchAtConfiguredDepth(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 2})
	err := Run(RunOptions{
		Root: root,
		Env:  []string{clients.EnvDispatchActive + "=2"},
	})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitNested {
		t.Fatalf("expected nested exit, got %T: %v", err, err)
	}
}

func TestRunRejectsInvalidDispatchDepthEnv(t *testing.T) {
	// A present-but-non-parseable AL_DISPATCH_ACTIVE fails loud rather than
	// silently defaulting to depth 0. Empty/whitespace counts as malformed: the
	// variable is only ever set by dispatch itself to a positive integer.
	for _, value := range []string{"bogus", "", "-1"} {
		t.Run(fmt.Sprintf("value=%q", value), func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{DispatchMaxDepth: 2})
			err := Run(RunOptions{
				Root: root,
				Env:  []string{clients.EnvDispatchActive + "=" + value},
			})
			var exitErr *ExitError
			if !errors.As(err, &exitErr) || exitErr.Code != ExitNested {
				t.Fatalf("expected nested exit, got %T: %v", err, err)
			}
			if !strings.Contains(exitErr.Error(), clients.EnvDispatchActive) {
				t.Fatalf("expected %s in error, got %q", clients.EnvDispatchActive, exitErr.Error())
			}
		})
	}
}

func TestRunUnknownCallerRequiresAgent(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	err := Run(RunOptions{
		Root: root,
		Env:  []string{"PATH=/bin"},
		LookPath: func(string) (string, error) {
			return "/bin/mock", nil
		},
		PromptArgs: []string{"Review"},
	})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitUsage {
		t.Fatalf("expected usage exit, got %T: %v", err, err)
	}
}

func TestRunRandomExcludesCallerAndExecutesCodex(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "codex.log")
	promptPath := filepath.Join(t.TempDir(), "codex.prompt")
	writeDispatchStub(t, binDir, "codex", `printf '{"type":"thread.started"}\n{"type":"agent_message","message":"codex ok"}\n'`)
	writeDispatchStub(t, binDir, "agy", `printf 'agy unused'`)
	env := []string{
		"PATH=" + testPath(binDir),
		clients.EnvDispatchCallerAgent + "=" + AgentClaude,
		"AL_TEST_LOG=" + logPath,
		"AL_TEST_PROMPT=" + promptPath,
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentRandom,
		PromptArgs: []string{"Review", "this"},
		Env:        env,
		Stdout:     &stdout,
		Stderr:     &stderr,
		LookPath:   mockLookPath(binDir),
		ChooseRandom: func(pool []string) (string, error) {
			if strings.Join(pool, ",") != "codex,antigravity" {
				return "", fmt.Errorf("pool = %v", pool)
			}
			return AgentCodex, nil
		},
	})
	if err != nil {
		t.Fatalf("Run error: %v\nstderr:\n%s", err, stderr.String())
	}
	if stdout.String() != "codex ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Dispatch target: codex (random selection)") {
		t.Fatalf("expected random target notice, got %q", stderr.String())
	}
	assertFileContains(t, logPath, clients.EnvDispatchActive+"=1")
	assertFileContains(t, logPath, clients.EnvDispatchCallerAgent+"=codex")
	assertFileContains(t, logPath, "CODEX_HOME="+filepath.Join(root, ".codex"))
	// Positive env-passthrough contract: dispatch creates a per-run directory
	// via run.Create and exports AL_RUN_DIR / AL_RUN_ID via BuildEnv.
	assertFileContains(t, logPath, "AL_RUN_DIR=")
	assertFileContains(t, logPath, "AL_RUN_ID=")
	// Negative env-passthrough contract: BuildEnv strips AL_SHIM_ACTIVE so the
	// shim marker does not leak into the dispatched child env.
	assertFileDoesNotContain(t, logPath, "AL_SHIM_ACTIVE=")
	assertFileContains(t, promptPath, "Review this")
}

func TestRunAntigravityRejectsReasoningEffortBeforeLaunch(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	err := Run(RunOptions{
		Root:            root,
		Agent:           AgentAntigravity,
		ReasoningEffort: "high",
		PromptArgs:      []string{"Review"},
		Env:             []string{"PATH=/bin"},
		LookPath: func(string) (string, error) {
			return "/bin/mock", nil
		},
	})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitUsage {
		t.Fatalf("expected usage exit, got %T: %v", err, err)
	}
}

func TestRunAntigravityUsesConfiguredModel(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{AntigravityModel: "Gemini 3.1 Pro (High)"})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "agy.log")
	writeDispatchStub(t, binDir, "agy", `printf 'agy ok'`)
	env := []string{
		"PATH=" + testPath(binDir),
		"AL_TEST_LOG=" + logPath,
	}
	var stdout bytes.Buffer
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentAntigravity,
		PromptArgs: []string{"Review"},
		Env:        env,
		Stdout:     &stdout,
		Stderr:     &bytes.Buffer{},
		LookPath:   mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if stdout.String() != "agy ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	assertFileContains(t, logPath, "ARG_1=--model")
	assertFileContains(t, logPath, "ARG_2=Gemini 3.1 Pro (High)")
	assertFileContains(t, logPath, "ARG_3=--print-timeout")
}

func TestRunAntigravityUsesModelOverride(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{AntigravityModel: "Gemini 3.1 Pro (High)"})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "agy.log")
	writeDispatchStub(t, binDir, "agy", `printf 'agy ok'`)
	env := []string{
		"PATH=" + testPath(binDir),
		"AL_TEST_LOG=" + logPath,
	}
	var stdout bytes.Buffer
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentAntigravity,
		Model:      "Gemini 3.5 Flash (High)",
		PromptArgs: []string{"Review"},
		Env:        env,
		Stdout:     &stdout,
		Stderr:     &bytes.Buffer{},
		LookPath:   mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if stdout.String() != "agy ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	assertFileContains(t, logPath, "ARG_1=--model")
	assertFileContains(t, logPath, "ARG_2=Gemini 3.5 Flash (High)")
	assertFileDoesNotContain(t, logPath, "Gemini 3.1 Pro (High)")
}

func TestRunClaudeSkillPromptAndCommandConstruction(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{ClaudeModel: "opus", ClaudeReasoningEffort: "high", ClaudeLocalConfigDir: true})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "claude.log")
	promptPath := filepath.Join(t.TempDir(), "claude.prompt")
	writeDispatchStub(t, binDir, "claude", `printf '{"type":"stream_event","event":{"delta":{"type":"text_delta","text":"claude ok"}}}\n'`)
	env := []string{
		"PATH=" + testPath(binDir),
		"AL_TEST_LOG=" + logPath,
		"AL_TEST_PROMPT=" + promptPath,
	}
	var stdout bytes.Buffer
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentClaude,
		Skill:      "review-plan",
		PromptArgs: []string{"Review"},
		Env:        env,
		Stdout:     &stdout,
		Stderr:     &bytes.Buffer{},
		LookPath:   mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if stdout.String() != "claude ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	assertFileContains(t, logPath, "ARG_0=--print")
	assertFileContains(t, logPath, "ARG_5=--model")
	assertFileContains(t, logPath, "ARG_6=opus")
	assertFileContains(t, logPath, "ARG_7=--effort")
	assertFileContains(t, logPath, "ARG_8=high")
	assertFileContains(t, logPath, "CLAUDE_CONFIG_DIR="+filepath.Join(root, ".claude-config"))
	assertFileContains(t, promptPath, "/review-plan\nReview")
}

func TestRunAntigravityCommandConstruction(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "agy.log")
	promptPath := filepath.Join(t.TempDir(), "agy.prompt")
	writeDispatchStub(t, binDir, "agy", `printf 'agy ok'`)
	env := []string{
		"PATH=" + testPath(binDir),
		"AL_TEST_LOG=" + logPath,
		"AL_TEST_PROMPT=" + promptPath,
	}
	var stdout bytes.Buffer
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentAntigravity,
		PromptArgs: []string{"Review"},
		Env:        env,
		Stdout:     &stdout,
		Stderr:     &bytes.Buffer{},
		LookPath:   mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if stdout.String() != "agy ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	assertFileContains(t, logPath, "ARG_0=--gemini_dir="+filepath.Join(root, ".agy"))
	assertFileContains(t, logPath, "ARG_1=--print-timeout")
	assertFileContains(t, logPath, "ARG_2="+AntigravityPrintTimeout)
	assertFileContains(t, logPath, "ARG_3=--print")
	assertFileContains(t, logPath, "AGY_CLI_DISABLE_AUTO_UPDATE=1")
}

// TestRunCodexDownstreamRejectsCustomOverridePreservesStderr exercises
// F11: when the downstream CLI rejects a custom override value, dispatch
// must exit 70 (ExitTargetFailure) AND the target's stderr must be
// preserved on the dispatch caller's stderr (per spec § Exit Status).
func TestRunCodexDownstreamRejectsCustomOverridePreservesStderr(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "codex.log")
	// The stub looks for --model bogus-rejected-model on argv and exits
	// non-zero with a recognizable stderr message; any other invocation
	// succeeds. This mimics the contract documented in spec § CLI:
	// "If the downstream CLI rejects a custom override value, dispatch
	// exits 70 and preserves the target error text on stderr."
	stub := `
if printf '%s\n' "$@" | grep -qx "bogus-rejected-model"; then
  printf 'codex: model bogus-rejected-model is not recognized\n' >&2
  exit 2
fi
printf '{"type":"agent_message","message":"ok"}\n'
`
	writeDispatchStub(t, binDir, "codex", stub)
	env := []string{
		"PATH=" + testPath(binDir),
		clients.EnvDispatchCallerAgent + "=" + AgentClaude,
		"AL_TEST_LOG=" + logPath,
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentCodex,
		Model:      "bogus-rejected-model",
		PromptArgs: []string{"Review"},
		Env:        env,
		Stdout:     &stdout,
		Stderr:     &stderr,
		LookPath:   mockLookPath(binDir),
	})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitTargetFailure {
		t.Fatalf("expected ExitTargetFailure (70), got %T: %v", err, err)
	}
	// The stub's stderr text must appear in the dispatch caller's stderr
	// buffer (forwarded verbatim by runStructuredCommand's stderr copier).
	if !strings.Contains(stderr.String(), "bogus-rejected-model is not recognized") {
		t.Fatalf("expected stub stderr to be preserved on caller stderr; got %q", stderr.String())
	}
}

type dispatchRepoConfig struct {
	AntigravityModel      string
	ClaudeModel           string
	ClaudeReasoningEffort string
	ClaudeLocalConfigDir  bool
	DispatchMaxDepth      int
}

func writeDispatchRepo(t *testing.T, repoConfig dispatchRepoConfig) string {
	t.Helper()
	root := t.TempDir()
	paths := config.DefaultPaths(root)
	for _, dir := range []string{paths.InstructionsDir, paths.SkillsDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	localConfigLine := ""
	if repoConfig.ClaudeLocalConfigDir {
		localConfigLine = "local_config_dir = true\n"
	}
	antigravityModelLine := ""
	if repoConfig.AntigravityModel != "" {
		antigravityModelLine = fmt.Sprintf("model = %q\n", repoConfig.AntigravityModel)
	}
	dispatchBlock := ""
	if repoConfig.DispatchMaxDepth != 0 {
		dispatchBlock = fmt.Sprintf("max_depth = %d\n", repoConfig.DispatchMaxDepth)
	}
	configToml := fmt.Sprintf(`
[dispatch]
%s

[approvals]
mode = "all"

[agents.antigravity]
enabled = true
%s

[agents.claude]
enabled = true
model = %q
reasoning_effort = %q
%s
[agents.claude_vscode]
enabled = false

[agents.codex]
enabled = true

[agents.vscode]
enabled = false

[agents.copilot_cli]
enabled = false

[warnings]
instruction_token_threshold = 50000
mcp_server_threshold = 50
`, dispatchBlock, antigravityModelLine, repoConfig.ClaudeModel, repoConfig.ClaudeReasoningEffort, localConfigLine)
	if err := os.WriteFile(paths.ConfigPath, []byte(configToml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstructionsDir, "00_rules.md"), []byte("base"), 0o600); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	skillDir := filepath.Join(paths.SkillsDir, "review-plan")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	skill := "---\nname: review-plan\ndescription: Review a plan.\n---\n\nReview it.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skill), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.WriteFile(paths.CommandsAllow, []byte(""), 0o600); err != nil {
		t.Fatalf("write commands.allow: %v", err)
	}
	block, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read gitignore block: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "gitignore.block"), block, 0o600); err != nil {
		t.Fatalf("write gitignore block: %v", err)
	}
	return root
}

func writeDispatchStub(t *testing.T, binDir string, name string, outputScript string) {
	t.Helper()
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	path := filepath.Join(binDir, name)
	content := fmt.Sprintf(`#!/bin/sh
{
  i=0
  for arg in "$@"; do
    echo "ARG_${i}=${arg}"
    i=$((i + 1))
  done
  env | grep -E '^(AL_|CODEX_HOME|CLAUDE_CONFIG_DIR|AGY_CLI)' | sort || true
} >> "$AL_TEST_LOG"
if [ -n "${AL_TEST_PROMPT:-}" ]; then
  cat > "$AL_TEST_PROMPT"
else
  cat >/dev/null
fi
%s
`, outputScript)
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil { // #nosec G306 -- test writes an executable shell stub in a test-owned bin directory.
		t.Fatalf("write stub: %v", err)
	}
}

func mockLookPath(binDir string) func(string) (string, error) {
	return func(name string) (string, error) {
		path := filepath.Join(binDir, name)
		if _, err := os.Stat(path); err != nil {
			return "", err
		}
		return path, nil
	}
}

func testPath(binDir string) string {
	return binDir + string(os.PathListSeparator) + os.Getenv("PATH")
}

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("expected %q in %s:\n%s", want, path, string(data))
	}
}

func assertFileDoesNotContain(t *testing.T, path string, unwanted string) {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if strings.Contains(string(data), unwanted) {
		t.Fatalf("did not expect %q in %s:\n%s", unwanted, path, string(data))
	}
}
