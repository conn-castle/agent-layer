//go:build live_codex

package sync

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const liveCodexDispatchWaitEnv = "AL_LIVE_CODEX_DISPATCH_WAIT"

// TestCodexDispatchWaitStaysDirect is a paid, local-only integration test for
// Codex code-mode polling. The live_codex build tag keeps it out of ordinary
// test and CI runs; the environment guard prevents accidental paid execution.
func TestCodexDispatchWaitStaysDirect(t *testing.T) {
	if os.Getenv(liveCodexDispatchWaitEnv) != "1" {
		t.Skipf("set %s=1 to run the paid local Codex test", liveCodexDispatchWaitEnv)
	}

	repoRoot := liveCodexRepoRoot(t)
	alBinary := filepath.Join(repoRoot, ".agent-layer", "tmp", "dev-bin", "al")
	if _, err := os.Stat(alBinary); err != nil {
		t.Fatalf("development al binary missing at %s; run `make test-codex-dispatch-wait-live`: %v", alBinary, err)
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Fatalf("codex CLI is required: %v", err)
	}

	workspace := t.TempDir()
	binDir := filepath.Join(workspace, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("create test bin directory: %v", err)
	}
	linkedAL := filepath.Join(binDir, "al")
	if err := os.Symlink(alBinary, linkedAL); err != nil {
		t.Fatalf("link development al binary: %v", err)
	}

	testEnv := liveCodexEnvironment(os.Environ(), map[string]string{
		"AL_DEV_BYPASS_VERSION_DISPATCH": "1",
		"PATH":                           binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	liveCodexRun(t, ctx, workspace, testEnv, "git", "init", "-q")
	liveCodexRun(t, ctx, workspace, testEnv, linkedAL, "init", "--no-wizard")
	configPath := filepath.Join(workspace, ".agent-layer", "config.toml")
	liveCodexConfigureProject(t, configPath)
	liveCodexRun(t, ctx, workspace, testEnv, linkedAL, "sync")

	codexHome := filepath.Join(workspace, ".codex")
	liveCodexCopyAuth(t, repoRoot, codexHome)
	generatedConfig, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatalf("read generated Codex config: %v", err)
	}
	if !bytes.Contains(generatedConfig, []byte(`direct_only_tool_namespaces = ['mcp__agent_layer']`)) &&
		!bytes.Contains(generatedConfig, []byte(`direct_only_tool_namespaces = ["mcp__agent_layer"]`)) {
		t.Fatalf("generated Codex config does not make mcp__agent_layer direct-only:\n%s", generatedConfig)
	}

	testEnv = liveCodexEnvironment(testEnv, map[string]string{"CODEX_HOME": codexHome})
	prompt := "Use /agent-dispatch to start exactly one Codex child using gpt-5.6-luna with low reasoning. " +
		"Tell the child to run the shell command `sleep 20` and then report completion. " +
		"Wait for that child until terminal, then report its handle and status. Do no unrelated work."
	stdout := liveCodexRun(t, ctx, workspace, testEnv,
		"codex", "exec", "--json", "--strict-config", "--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox", "--model", "gpt-5.6-luna",
		"--config", `model_reasoning_effort="low"`, prompt,
	)
	threadID := liveCodexThreadID(t, stdout)
	rolloutPath := liveCodexRolloutPath(t, codexHome, threadID)
	assertLiveCodexDirectWait(t, rolloutPath)
}

func liveCodexConfigureProject(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read initialized Agent Layer config: %v", err)
	}
	var project map[string]any
	if err := toml.Unmarshal(data, &project); err != nil {
		t.Fatalf("parse initialized Agent Layer config: %v", err)
	}
	agents, ok := project["agents"].(map[string]any)
	if !ok {
		t.Fatal("initialized Agent Layer config has no agents table")
	}
	for _, name := range []string{"antigravity", "claude", "claude_vscode", "vscode", "copilot_cli"} {
		agent, ok := agents[name].(map[string]any)
		if !ok {
			t.Fatalf("initialized Agent Layer config has no agents.%s table", name)
		}
		agent["enabled"] = false
	}
	codex, ok := agents["codex"].(map[string]any)
	if !ok {
		t.Fatal("initialized Agent Layer config has no agents.codex table")
	}
	codex["enabled"] = true
	codex["local_config_dir"] = true
	codex["model"] = "gpt-5.6-luna"
	codex["reasoning_effort"] = "low"
	approvals, ok := project["approvals"].(map[string]any)
	if !ok {
		t.Fatal("initialized Agent Layer config has no approvals table")
	}
	approvals["mode"] = "yolo"

	encoded, err := toml.Marshal(project)
	if err != nil {
		t.Fatalf("encode live-test Agent Layer config: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write live-test Agent Layer config: %v", err)
	}
}

func liveCodexRepoRoot(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolve working directory: %v", err)
	}
	root, err := filepath.Abs(filepath.Join(workingDirectory, "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return root
}

func liveCodexRun(t *testing.T, ctx context.Context, directory string, environment []string, name string, args ...string) []byte {
	t.Helper()
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = directory
	command.Env = environment
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			t.Fatalf("%s timed out: %v", name, ctx.Err())
		}
		t.Fatalf("%s failed: %v\nstdout:\n%s\nstderr:\n%s", name, err, stdout.String(), stderr.String())
	}
	return stdout.Bytes()
}

func liveCodexEnvironment(base []string, overrides map[string]string) []string {
	filtered := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrides[key]; replaced || strings.HasPrefix(key, "AL_DISPATCH_") {
			continue
		}
		filtered = append(filtered, entry)
	}
	for key, value := range overrides {
		filtered = append(filtered, key+"="+value)
	}
	return filtered
}

func liveCodexCopyAuth(t *testing.T, repoRoot string, destinationHome string) {
	t.Helper()
	var candidates []string
	if configuredHome := os.Getenv("CODEX_HOME"); configuredHome != "" {
		candidates = append(candidates, filepath.Join(configuredHome, "auth.json"))
	}
	candidates = append(candidates, filepath.Join(repoRoot, ".codex", "auth.json"))
	if userHome, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(userHome, ".codex", "auth.json"))
	}
	for _, candidate := range candidates {
		auth, err := os.ReadFile(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("read Codex authentication from %s: %v", candidate, err)
		}
		if err := os.WriteFile(filepath.Join(destinationHome, "auth.json"), auth, 0o600); err != nil {
			t.Fatalf("write isolated Codex authentication: %v", err)
		}
		return
	}
	t.Fatal("Codex auth.json not found in CODEX_HOME, the repository .codex directory, or ~/.codex")
}

func liveCodexThreadID(t *testing.T, output []byte) string {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err == nil && event.Type == "thread.started" && event.ThreadID != "" {
			return event.ThreadID
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read Codex JSON events: %v", err)
	}
	t.Fatalf("Codex output did not contain a thread.started event:\n%s", output)
	return ""
}

func liveCodexRolloutPath(t *testing.T, codexHome string, threadID string) string {
	t.Helper()
	var matches []string
	err := filepath.WalkDir(filepath.Join(codexHome, "sessions"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.Contains(entry.Name(), threadID) && strings.HasSuffix(entry.Name(), ".jsonl") {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("find Codex rollout for thread %s: %v", threadID, err)
	}
	if len(matches) != 1 {
		t.Fatalf("found %d Codex rollouts for thread %s, want 1: %v", len(matches), threadID, matches)
	}
	return matches[0]
}

func assertLiveCodexDirectWait(t *testing.T, rolloutPath string) {
	t.Helper()
	rollout, err := os.Open(rolloutPath)
	if err != nil {
		t.Fatalf("open Codex rollout: %v", err)
	}
	defer func() {
		if closeErr := rollout.Close(); closeErr != nil {
			t.Errorf("close Codex rollout: %v", closeErr)
		}
	}()

	var directWaits int
	var agentLayerExecWrappers int
	var codeModeWaits int
	var completedWaits int
	var longestWaitSeconds int64
	scanner := bufio.NewScanner(rollout)
	for scanner.Scan() {
		var event struct {
			Type    string `json:"type"`
			Payload struct {
				Type       string `json:"type"`
				Name       string `json:"name"`
				Namespace  string `json:"namespace"`
				Input      string `json:"input"`
				Invocation struct {
					Tool string `json:"tool"`
				} `json:"invocation"`
				Duration struct {
					Seconds int64 `json:"secs"`
				} `json:"duration"`
				Result json.RawMessage `json:"result"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("parse Codex rollout event: %v", err)
		}
		payload := event.Payload
		if event.Type == "response_item" && payload.Type == "function_call" {
			if payload.Namespace == codexAgentLayerToolNamespace && payload.Name == "dispatch_wait" {
				directWaits++
			}
			if payload.Name == "wait" {
				codeModeWaits++
			}
		}
		if event.Type == "response_item" && payload.Type == "custom_tool_call" && payload.Name == "exec" &&
			strings.Contains(payload.Input, "mcp__agent_layer") {
			agentLayerExecWrappers++
		}
		if event.Type == "event_msg" && payload.Type == "mcp_tool_call_end" && payload.Invocation.Tool == "dispatch_wait" {
			if payload.Duration.Seconds > longestWaitSeconds {
				longestWaitSeconds = payload.Duration.Seconds
			}
			if bytes.Contains(payload.Result, []byte("completed")) {
				completedWaits++
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read Codex rollout: %v", err)
	}

	if directWaits != 1 || completedWaits != 1 || longestWaitSeconds < 15 || agentLayerExecWrappers != 0 || codeModeWaits != 0 {
		t.Fatalf(
			"Codex dispatch wait invariant failed for %s: direct_waits=%d completed_waits=%d longest_wait_seconds=%d agent_layer_exec_wrappers=%d code_mode_waits=%d",
			rolloutPath, directWaits, completedWaits, longestWaitSeconds, agentLayerExecWrappers, codeModeWaits,
		)
	}
	t.Logf("direct dispatch_wait remained blocked for %ds with no Agent Layer exec wrapper or code-mode polling", longestWaitSeconds)
}
