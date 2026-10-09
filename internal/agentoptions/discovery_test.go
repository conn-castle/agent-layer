package agentoptions

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/config"
)

// The test executable doubles as a harness so protocol, environment, working
// directory, and cancellation checks exercise the production process boundary.
func TestMain(m *testing.M) {
	if mode := os.Getenv("AL_TEST_MODEL_HARNESS"); mode != "" {
		runModelHarness(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runModelHarness(mode string) {
	if mode == "stdout-silent-exit" {
		return
	}
	if mode == "stdout-exit" {
		fmt.Print(strings.Repeat(" ", maxDiscoveryBytes+1))
		return
	}
	if strings.HasPrefix(mode, "claude-refresh-") {
		runClaudeRefreshHarness(mode)
		return
	}
	if strings.HasPrefix(mode, "copilot") {
		runCopilotHarness(mode)
		return
	}
	if mode == "environment" {
		cwd, _ := os.Getwd()
		if os.Getenv("GROK_HOME") != filepath.Join(cwd, ".grok-config") || os.Getenv("AL_TEST_PROJECT_VALUE") != "project-value" || os.Getenv("AL_DISPATCH_ACTIVE") != "" {
			os.Exit(3)
		}
		fmt.Println("Available models:\n  * future-model (default)")
		return
	}
	if mode == "grok" {
		fmt.Println("Default model: future-model\n\nAvailable models:\n  * future-model (default)\n  - another-model")
		return
	}
	if mode == "grok-expired-session" {
		// Real Grok prints its stale status, then persists a silent refresh
		// before listing models; only a completed run saves the refresh.
		marker := os.Getenv("AL_TEST_GROK_REFRESHED")
		if _, err := os.Stat(marker); err == nil { // #nosec G703 -- marker path is inside the test-owned temporary directory.
			fmt.Println("You are logged in with grok.com.\n\nAvailable models:\n  * refreshed-model (default)")
			return
		}
		fmt.Println("You are not authenticated.")
		time.Sleep(200 * time.Millisecond)
		if err := os.WriteFile(marker, nil, 0o600); err != nil { // #nosec G703 -- marker path is inside the test-owned temporary directory.
			os.Exit(3)
		}
		fmt.Println("\nAvailable models:\n  * refreshed-model (default)")
		return
	}
	if mode == "unauthenticated-empty" {
		fmt.Println("You are not authenticated.")
		return
	}
	if mode == "unauthenticated" {
		fmt.Println("You are not authenticated.\nAvailable models:\n  * fallback")
		return
	}
	if mode == "unauthenticated-exit-error" {
		fmt.Println("You are not authenticated.\nAvailable models:\n  unexpected")
		os.Exit(1)
	}
	if mode == "bad-output" {
		fmt.Println("unexpected output")
		return
	}
	if mode == "exit-error" {
		fmt.Println("Available models:\n  - misleading-model")
		os.Exit(2)
	}
	if mode == "hang" {
		if slices.Contains(os.Args, "--input-format") {
			_, _ = io.Copy(io.Discard, os.Stdin)
			return
		}
		time.Sleep(time.Minute)
		return
	}
	if mode == "oversized" {
		fmt.Print(strings.Repeat("x", maxDiscoveryBytes+1))
		return
	}
	if mode == "antigravity" {
		cwd, _ := os.Getwd()
		if !strings.Contains(strings.Join(os.Args[1:], " "), "--gemini_dir="+filepath.Join(cwd, ".agy")) || os.Getenv("AGY_CLI_DISABLE_AUTO_UPDATE") != "1" || os.Getenv("AL_TEST_PROJECT_VALUE") != "project-value" {
			os.Exit(3)
		}
		fmt.Println("future-id\tFuture Display Name")
		return
	}
	if mode == "antigravity-no-project" {
		if strings.Join(os.Args[1:], " ") != "models" || os.Getenv("AGY_CLI_DISABLE_AUTO_UPDATE") != "1" || !stdinIsNullDevice() {
			os.Exit(3)
		}
		fmt.Print("\nmedium-id\tGemini Flash (Medium)\nhigh-id\tGemini Flash (High)\n")
		return
	}
	if mode == "antigravity-empty" {
		fmt.Println()
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var msg map[string]any
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			os.Exit(4)
		}
		if mode == "claude" || mode == "claude-error" {
			request, _ := msg["request"].(map[string]any)
			if msg["type"] != "control_request" || request["subtype"] != "initialize" {
				os.Exit(5)
			}
			subtype := "success"
			if mode == "claude-error" {
				subtype = "error"
			}
			_ = encoder.Encode(map[string]any{"type": "control_response", "response": map[string]any{"request_id": msg["request_id"], "subtype": subtype, "response": map[string]any{"models": []map[string]string{{"value": "future-claude"}}}}})
			continue
		}
		switch msg["method"] {
		case "initialize":
			if mode == "muse-noisy" {
				_ = encoder.Encode(map[string]any{"jsonrpc": copilotJSONRPCVersion, "method": "noise", "params": map[string]any{}})
			}
			_ = encoder.Encode(map[string]any{"id": msg["id"], "result": map[string]string{"userAgent": "fixture"}})
		case "initialized":
		case "model/list":
			params := msg["params"].(map[string]any)
			if mode == "muse" || mode == "muse-noisy" || mode == "muse-empty" || mode == "muse-missing-models" {
				if mode == "muse-missing-models" {
					_ = encoder.Encode(map[string]any{"jsonrpc": copilotJSONRPCVersion, "id": msg["id"], "result": map[string]any{"catalog": []any{}}})
					continue
				}
				models := []map[string]string{}
				if mode == "muse-noisy" {
					_ = encoder.Encode(map[string]any{"jsonrpc": copilotJSONRPCVersion, "method": "noise", "params": map[string]any{}})
				}
				if mode == "muse" || mode == "muse-noisy" {
					models = append(models, map[string]string{"modelId": "future-muse"}, map[string]string{"modelId": "another-muse"})
				}
				_ = encoder.Encode(map[string]any{"jsonrpc": copilotJSONRPCVersion, "id": msg["id"], "result": map[string]any{"models": models}})
				continue
			}
			if mode == "codex-error" {
				_ = encoder.Encode(map[string]any{"id": msg["id"], "error": map[string]any{"code": -32000}})
				continue
			}
			var cursor any = "next-page"
			model := "future-codex"
			if params["cursor"] == "next-page" && mode != "codex-loop" {
				cursor = nil
				model = "another-codex"
			}
			_ = encoder.Encode(map[string]any{"method": "notification"})
			_ = encoder.Encode(map[string]any{"id": msg["id"], "result": map[string]any{"data": []map[string]string{{"model": model}}, "nextCursor": cursor}})
		default:
			os.Exit(6)
		}
	}
}

const claudeRefreshSuccess = "success"

// runClaudeRefreshHarness models a spent refresh token whose replacement is
// saved during graceful shutdown, after the catalog response. The trailing
// output also forces the parent to drain stdout rather than only call Wait.
func runClaudeRefreshHarness(mode string) {
	cwd, _ := os.Getwd()
	credential := filepath.Join(cwd, ".claude-config", ".credentials.json")
	if os.Getenv("CLAUDE_CONFIG_DIR") != filepath.Dir(credential) || os.Getenv("AL_TEST_PROJECT_VALUE") != "project-value" || os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") != "fake-access-token" {
		os.Exit(3)
	}
	content, err := os.ReadFile(credential) // #nosec G304,G703 -- test-owned credential fixture.
	if err != nil || string(content) != "old-refresh-token" {
		os.Exit(3)
	}
	decoder := json.NewDecoder(os.Stdin)
	var request struct {
		ID string `json:"request_id"`
	}
	if decoder.Decode(&request) != nil {
		os.Exit(4)
	}
	if mode == "claude-refresh-early-exit" {
		os.Exit(1)
	}
	if err := os.WriteFile(credential+".spent", nil, 0o600); err != nil { // #nosec G703 -- test-owned marker.
		os.Exit(3)
	}
	switch mode {
	case "claude-refresh-timeout", "claude-refresh-cancel", "claude-refresh-held-timeout", "claude-refresh-held-timeout-exit-error":
		// A deadline/cancellation must close stdin without killing the child.
	case "claude-refresh-malformed":
		fmt.Println("invalid-json")
	case "claude-refresh-oversized":
		fmt.Print(strings.Repeat(" ", maxDiscoveryBytes+1))
	default:
		subtype := claudeRefreshSuccess
		if mode == "claude-refresh-rejected" {
			subtype = "error"
		}
		_, _ = fmt.Fprintf(os.Stdout, `{"type":"control_response","response":{"request_id":%q,"subtype":%q,"response":{"models":[{"value":"account-model"}]}}}`+"\n", request.ID, subtype)
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	if strings.Contains(mode, "held") {
		if err := os.WriteFile(credential+".draining", nil, 0o600); err != nil { // #nosec G703 -- test-owned marker.
			os.Exit(3)
		}
		for {
			if _, err := os.Stat(credential + ".release"); err == nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
	} else {
		time.Sleep(200 * time.Millisecond)
	}
	fmt.Print(strings.Repeat(" ", maxDiscoveryBytes+1))
	if err := os.WriteFile(credential, []byte("new-refresh-token"), 0o600); err != nil { // #nosec G304,G703 -- test-owned credential fixture.
		os.Exit(3)
	}
	if strings.HasSuffix(mode, "exit-error") {
		os.Exit(2)
	}
}

// stdinIsNullDevice distinguishes the null device from a pipe, which would also
// reach EOF once closed, then confirms that reading stdin ends immediately.
func stdinIsNullDevice() bool {
	stdin, err := os.Stdin.Stat()
	if err != nil || stdin.Mode()&os.ModeNamedPipe != 0 || stdin.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	if err != nil || !os.SameFile(stdin, null) {
		return false
	}
	input, err := io.ReadAll(os.Stdin)
	return err == nil && len(input) == 0
}

func harnessRequest(t *testing.T, mode string) DiscoveryRequest {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return DiscoveryRequest{Env: []string{"AL_TEST_MODEL_HARNESS=" + mode}, LookPath: func(string) (string, error) { return path, nil }, Timeout: 3 * time.Second}
}

func claudeRefreshRequest(t *testing.T, mode string) (DiscoveryRequest, string) {
	t.Helper()
	req := harnessRequest(t, "claude-refresh-"+mode)
	root := t.TempDir()
	credential := filepath.Join(root, ".claude-config", ".credentials.json")
	if err := os.MkdirAll(filepath.Dir(credential), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credential, []byte("old-refresh-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	localConfigDir := true
	req.Project = &config.ProjectConfig{Root: root, Env: map[string]string{"AL_TEST_PROJECT_VALUE": "project-value", "CLAUDE_CODE_OAUTH_TOKEN": "fake-access-token"}, Config: config.Config{Agents: config.AgentsConfig{Claude: config.ClaudeConfig{LocalConfigDir: &localConfigDir}}}}
	return req, credential
}

func TestDiscoverModelsThroughHarnessProtocols(t *testing.T) {
	for _, tc := range []struct {
		agent, mode string
		want        []string
	}{
		{"claude", "claude", []string{"future-claude"}},
		{"codex", "codex", []string{"future-codex", "another-codex"}},
		{"grok", "grok", []string{"future-model", "another-model"}},
		{"copilot_cli", "copilot", []string{"future-copilot", "another-copilot"}},
		{"muse", "muse", []string{"future-muse", "another-muse"}},
	} {
		t.Run(tc.agent, func(t *testing.T) {
			got, err := DiscoverModels(tc.agent, harnessRequest(t, tc.mode))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("models=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestGrokDiscoveryReportsSilentlyRefreshedSession(t *testing.T) {
	req := harnessRequest(t, "grok-expired-session")
	req.Env = append(req.Env, "AL_TEST_GROK_REFRESHED="+filepath.Join(t.TempDir(), "refreshed"))
	got, err := DiscoverModels("grok", req)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"refreshed-model"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("models=%v want=%v", got, want)
	}
}

func TestClaudeDiscoveryCompletesRefreshBeforeReturning(t *testing.T) {
	for _, mode := range []string{claudeRefreshSuccess, "rejected", "malformed", "oversized", "timeout", "shutdown-timeout", "cancel", "exit-error"} {
		t.Run(mode, func(t *testing.T) {
			req, credential := claudeRefreshRequest(t, mode)
			if mode == "timeout" || mode == "shutdown-timeout" {
				req.Timeout = 100 * time.Millisecond
			}
			if mode == "cancel" {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				req.Context = ctx
				go func() {
					ticker := time.NewTicker(time.Millisecond)
					defer ticker.Stop()
					for {
						if _, err := os.Stat(credential + ".spent"); err == nil {
							cancel()
							return
						}
						select {
						case <-ctx.Done():
							return
						case <-ticker.C:
						}
					}
				}()
			}
			models, err := DiscoverModels(agentClaude, req)
			if mode == claudeRefreshSuccess || mode == "shutdown-timeout" {
				if err != nil || !reflect.DeepEqual(models, []string{"account-model"}) {
					t.Fatalf("models=%v error=%v", models, err)
				}
			} else if err == nil || len(models) != 0 {
				t.Fatalf("expected discovery failure, models=%v error=%v", models, err)
			}
			if mode == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected deadline error, got %v", err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("expected cancellation error, got %v", err)
			}
			content, readErr := os.ReadFile(credential) // #nosec G304 -- test-owned credential fixture.
			if readErr != nil || string(content) != "new-refresh-token" {
				t.Fatalf("refresh was interrupted: credential=%q error=%v", content, readErr)
			}
		})
	}
}

func TestClaudeFailedDiscoveryDoesNotRepeatErrorDuringCleanup(t *testing.T) {
	for _, mode := range []string{"early-exit", "rejected", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			req, _ := claudeRefreshRequest(t, mode)
			var cleanup DiscoveryCleanup
			req.Cleanup = &cleanup
			defer func() { _ = cleanup.Close() }()
			if _, err := DiscoverModels(agentClaude, req); err == nil {
				t.Fatal("failed initialization reported success")
			}
			if err := cleanup.Close(); err != nil {
				t.Fatalf("lookup failure reported again as shutdown failure: %v", err)
			}
		})
	}
}

func TestCancelledCleanupWaitRetainsShutdownError(t *testing.T) {
	var cleanup DiscoveryCleanup
	finish, err := cleanup.begin()
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("native shutdown failed")
	finish(failure)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cleanup.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter consumed shutdown error: %v", err)
	}
	if err := cleanup.Close(); !errors.Is(err, failure) {
		t.Fatalf("owner lost shutdown error: %v", err)
	}
}

func TestClaudeCleanupStopsDrainingAfterNativeExit(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close(); _ = writer.Close() }()
	// Keep the parent's write end open, simulating a descendant that inherited
	// stdout. The child also fills the pipe, requiring a concurrent drain.
	cmd := exec.Command(path) // #nosec G204 -- the hermetic test executable.
	cmd.Env = []string{"AL_TEST_MODEL_HARNESS=stdout-exit"}
	cmd.Stdout = writer
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		drainErr, waitErr := waitForClaudeExit(cmd.Wait, reader)
		done <- errors.Join(drainErr, waitErr)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		_ = writer.Close()
		<-done
		t.Fatal("cleanup waited for inherited stdout after native exit")
	}
}

func TestClaudeCancellationReleasesInheritedStdoutBeforeReply(t *testing.T) {
	req := harnessRequest(t, "stdout-silent-exit")
	path, err := req.LookPath(agentClaude)
	if err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close(); _ = writer.Close() }()
	cmd := exec.Command(path) // #nosec G204 -- the hermetic test executable.
	cmd.Env = req.Env
	cmd.Stdout = writer
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	wait := claudeProcessWait(ctx, cmd, reader)
	defer func() { _ = wait() }()
	done := make(chan error, 1)
	go func() {
		var reply any
		done <- json.NewDecoder(reader).Decode(&reply)
	}()
	select {
	case err := <-done:
		if !errors.Is(err, os.ErrClosed) {
			t.Fatalf("parser was not released by cancellation after native exit: %v", err)
		}
	case <-time.After(3 * time.Second):
		_ = writer.Close()
		<-done
		t.Fatal("parser waited for inherited stdout beyond cancellation")
	}
}

func TestClaudeDiscoveryReturnsCatalogWhileOwnedCleanupFinishes(t *testing.T) {
	for _, mode := range []string{"held", "held-exit-error", "held-timeout", "held-timeout-exit-error"} {
		t.Run(mode, func(t *testing.T) {
			req, credential := claudeRefreshRequest(t, mode)
			var cleanup DiscoveryCleanup
			req.Cleanup = &cleanup
			defer func() {
				_ = os.WriteFile(credential+".release", nil, 0o600)
				_ = cleanup.Close()
			}()
			if strings.Contains(mode, "timeout") {
				req.Timeout = 100 * time.Millisecond
			}
			models, err := DiscoverModels(agentClaude, req)
			if strings.Contains(mode, "timeout") {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("expected deadline error, got %v", err)
				}
			} else if err != nil || !slices.Equal(models, []string{"account-model"}) {
				t.Fatalf("catalog was not available during cleanup: models=%v error=%v", models, err)
			}
			content, err := os.ReadFile(credential) // #nosec G304 -- test-owned credential fixture.
			if err != nil || string(content) != "old-refresh-token" {
				t.Fatalf("catalog waited for refresh: credential=%q error=%v", content, err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			if err := cleanup.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("launch barrier did not wait for cleanup: %v", err)
			}
			if err := os.WriteFile(credential+".release", nil, 0o600); err != nil {
				t.Fatal(err)
			}
			err = cleanup.Close()
			if strings.HasSuffix(mode, "exit-error") {
				if err == nil || !strings.Contains(err.Error(), "exit status 2") {
					t.Fatalf("lost background shutdown error: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			content, err = os.ReadFile(credential) // #nosec G304 -- test-owned credential fixture.
			if err != nil || string(content) != "new-refresh-token" {
				t.Fatalf("cleanup interrupted refresh: credential=%q error=%v", content, err)
			}
			if _, err := DiscoverModels(agentClaude, req); err == nil {
				t.Fatal("discovery started after owner shutdown")
			}
		})
	}
}

func TestDiscoveryUsesProjectLaunchContextWithoutSync(t *testing.T) {
	for _, agent := range []string{"grok", "antigravity"} {
		t.Run(agent, func(t *testing.T) {
			mode := agent
			if agent == "grok" {
				mode = "environment"
			}
			req := harnessRequest(t, mode)
			req.Env = append(req.Env, "AL_DISPATCH_ACTIVE=1")
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			req.Project = &config.ProjectConfig{Root: root, Env: map[string]string{"AL_TEST_PROJECT_VALUE": "project-value"}}
			// No config.toml or sync inputs exist. Discovery must still work from
			// the supplied snapshot using normal provider launch preparation.
			if _, err := DiscoverModels(agent, req); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(req.Project.Root, ".agent-layer")); !os.IsNotExist(err) {
				t.Fatalf("discovery created sync/run state: %v", err)
			}
		})
	}
}

func TestDiscoveryFailuresRemainExplicit(t *testing.T) {
	for _, tc := range []struct {
		agent, mode, wantError string
	}{
		{"copilot_cli", "copilot-error", "-32603: Failed to list models"},
		{"copilot_cli", "copilot-empty", ""}, {"copilot_cli", "copilot-malformed", ""}, {"copilot_cli", "copilot-oversized", ""},
		{"claude", "claude-error", ""}, {"codex", "codex-error", ""}, {"codex", "codex-loop", ""},
		{"grok", "unauthenticated", "sign in using al grok"}, {"grok", "unauthenticated-exit-error", "sign in using al grok"}, {"grok", "bad-output", ""}, {"grok", "exit-error", ""},
		{"antigravity", "bad-output", "invalid model row"}, {"antigravity", "exit-error", "exit status 2"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			req := harnessRequest(t, tc.mode)
			req.Live = true
			option := Resolve(config.Config{}, tc.agent, KindModel, req)
			if option.DiscoveryError == "" || option.Source != "unavailable" || len(option.Suggestions) != 0 {
				t.Fatalf("false discovery success: %+v", option)
			}
			if tc.wantError != "" && !strings.Contains(option.DiscoveryError, tc.wantError) {
				t.Fatalf("discovery error %q missing %q", option.DiscoveryError, tc.wantError)
			}
		})
	}
}

func TestDiscoveryDeadlineAndOffline(t *testing.T) {
	req := harnessRequest(t, "hang")
	req.Timeout = 50 * time.Millisecond
	for _, agent := range []string{"claude", "codex", "grok", "antigravity", "copilot_cli"} {
		start := time.Now()
		if _, err := DiscoverModels(agent, req); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s timeout error=%v", agent, err)
		}
		if time.Since(start) > 2*time.Second {
			t.Fatalf("%s timeout did not stop the child", agent)
		}
	}
	req.Env = append(req.Env, "AL_NO_NETWORK=1")
	req.LookPath = func(string) (string, error) { t.Fatal("offline discovery launched a harness"); return "", nil }
	if _, err := DiscoverModels("codex", req); err == nil {
		t.Fatal("offline discovery claimed success")
	}
	req.Env = []string{}
	req.Project = &config.ProjectConfig{Env: map[string]string{"AL_NO_NETWORK": "1"}}
	if _, err := DiscoverModels("codex", req); err == nil {
		t.Fatal("project-level offline discovery claimed success")
	}
	req = harnessRequest(t, "hang")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req.Context = ctx
	for _, agent := range []string{"grok", "antigravity"} {
		if _, err := DiscoverModels(agent, req); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s cancelled discovery error=%v", agent, err)
		}
	}
}

func TestAntigravityDiscoveryWithoutProject(t *testing.T) {
	got, err := DiscoverModels("antigravity", harnessRequest(t, "antigravity-no-project"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"medium-id", "high-id"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("models=%v want=%v", got, want)
	}
}

func TestAntigravityMissingBinaryCreatesNoHome(t *testing.T) {
	root := t.TempDir()
	req := DiscoveryRequest{Project: &config.ProjectConfig{Root: root}, LookPath: func(string) (string, error) { return "", os.ErrNotExist }}
	if _, err := DiscoverModels("antigravity", req); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing binary error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agy")); !os.IsNotExist(err) {
		t.Fatalf("missing binary created Antigravity home: %v", err)
	}
}

func TestParseAntigravityModelRows(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		want         []string
	}{
		{"current", "slug\tDisplay Name\nsecond\tOther Model\n", []string{"slug", "second"}},
		{"unstructured output", "\nAuthentication failed\n", nil},
		{"missing label", "slug\t\n", nil},
		{"missing slug", "\tDisplay Name\n", nil},
		{"extra column", "slug\tDisplay\textra\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			models, err := ParseModelCommandOutput("antigravity", []byte(tc.output))
			if tc.want == nil {
				if err == nil {
					t.Fatalf("invalid output accepted: %v", models)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(models, tc.want) {
				t.Fatalf("models=%v err=%v", models, err)
			}
		})
	}
}

func TestAntigravityLiveDiagnostics(t *testing.T) {
	for _, tc := range []struct{ mode, want string }{
		{"oversized", "antigravity model discovery: agy models output exceeded size limit"},
		{"antigravity-empty", "antigravity model discovery: agy models returned no model options"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			if _, err := DiscoverModels("antigravity", harnessRequest(t, tc.mode)); err == nil || err.Error() != tc.want {
				t.Fatalf("error=%v want %q", err, tc.want)
			}
		})
	}
}

// BenchmarkLiveModelDiscovery exercises exactly the project-aware no-sync Go
// discovery used by Wizard, Doctor, and Dispatch. Explicit opt-in keeps normal
// tests hermetic. Each iteration starts a fresh harness; upstream caches remain.
func BenchmarkLiveModelDiscovery(b *testing.B) {
	root := os.Getenv("AL_MODEL_DISCOVERY_BENCHMARK_ROOT")
	if root == "" {
		b.Skip("set AL_MODEL_DISCOVERY_BENCHMARK_ROOT to an initialized project")
	}
	project, err := config.LoadProjectConfig(root)
	if err != nil {
		b.Fatal(err)
	}
	for _, agent := range []string{"claude", "codex", "grok", "antigravity", "copilot_cli"} {
		b.Run(agent, func(b *testing.B) {
			req := DefaultDiscoveryRequest()
			req.Project = project
			b.ResetTimer()
			for range b.N {
				started := time.Now()
				models, err := DiscoverModels(agent, req)
				if err != nil {
					b.Fatalf("discovery failed after %s: %v", time.Since(started), err)
				}
				b.ReportMetric(float64(len(models)), "models")
			}
		})
	}
}

// Decode requests independently with MIME headers to check the actual wire
// contract, including startup flags and the absence of session/inference calls.
func runCopilotHarness(mode string) {
	if strings.Join(os.Args[1:], " ") != "--headless --stdio --no-auto-update" {
		os.Exit(7)
	}
	reader := bufio.NewReader(os.Stdin)
	headers := textproto.NewReader(reader)
	for _, method := range []string{"connect", "models.list"} {
		header, err := headers.ReadMIMEHeader()
		if err != nil {
			os.Exit(8)
		}
		length, err := strconv.Atoi(header.Get("Content-Length"))
		if err != nil || length <= 0 || length > 4096 {
			os.Exit(9)
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(reader, body); err != nil {
			os.Exit(10)
		}
		var request struct {
			ID      int    `json:"id"`
			Method  string `json:"method"`
			JSONRPC string `json:"jsonrpc"`
		}
		if json.Unmarshal(body, &request) != nil || request.Method != method || request.JSONRPC != copilotJSONRPCVersion {
			os.Exit(11)
		}
		result := any(map[string]any{"protocolVersion": 3})
		if method == "models.list" {
			switch mode {
			case "copilot-malformed":
				fmt.Print("Content-Length: nope\r\n\r\n")
				return
			case "copilot-oversized":
				fmt.Printf("Content-Length: %d\r\n\r\n", maxDiscoveryBytes+1)
				return
			case "copilot-empty":
				result = map[string]any{"models": []any{}}
			default:
				result = map[string]any{"models": []map[string]string{{"id": "future-copilot", "name": "Future Copilot"}, {"id": "another-copilot"}, {"id": "future-copilot"}}}
			}
		}
		response := map[string]any{"jsonrpc": copilotJSONRPCVersion, "id": request.ID, "result": result}
		if method == "models.list" && mode == "copilot-error" {
			delete(response, "result")
			response["error"] = map[string]any{"code": -32603, "message": "Failed to list models"}
		}
		body, _ = json.Marshal(response)
		fmt.Printf("Content-Length: %d\r\n\r\n%s", len(body), body)
	}
	// The discovery caller must cancel/reap an otherwise long-lived server.
	time.Sleep(time.Minute)
}

func TestMuseDiscoveryExplainsEmptyNativeCatalog(t *testing.T) {
	_, err := DiscoverModels(agentMuse, harnessRequest(t, "muse-empty"))
	if err == nil || !strings.Contains(err.Error(), "empty catalog") {
		t.Fatalf("error = %v, want empty catalog explanation", err)
	}
}

func TestMuseDiscoveryRejectsMissingModelsMember(t *testing.T) {
	_, err := DiscoverModels(agentMuse, harnessRequest(t, "muse-missing-models"))
	if err == nil || !strings.Contains(err.Error(), "omitted result.models") {
		t.Fatalf("error = %v, want malformed response failure", err)
	}
}

func TestMuseDiscoverySkipsUnrelatedMessages(t *testing.T) {
	got, err := DiscoverModels(agentMuse, harnessRequest(t, "muse-noisy"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"future-muse", "another-muse"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("models=%v want=%v", got, want)
	}
}

func TestMuseDiscoveryPreservesNativeEnvironment(t *testing.T) {
	root := t.TempDir()
	env := []string{"HOME=/native/home", "XDG_CONFIG_HOME=/native/config", "XDG_DATA_HOME=/native/data"}
	cmd, err := discoveryCommand(agentMuse, DiscoveryRequest{Context: context.Background(), Project: &config.ProjectConfig{Root: root}, Env: env, LookPath: func(string) (string, error) { return "/fixture/muse", nil }})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range env {
		if !slices.Contains(cmd.Env, entry) {
			t.Fatalf("lost environment %s: %v", entry, cmd.Env)
		}
	}
	for _, name := range []string{".muse-config", ".muse-data"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("created isolated root: %s", name)
		}
	}
}
