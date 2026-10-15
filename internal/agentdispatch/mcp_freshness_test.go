package agentdispatch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func freshnessWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertStaleProviderTools(t *testing.T, session *mcp.ClientSession, handle string) {
	t.Helper()
	for _, call := range []struct {
		name  string
		input any
	}{
		{ToolOptions, OptionsInput{}},
		{ToolStart, StartInput{Agent: AgentClaude, Prompt: "fixture"}},
		{ToolContinue, ContinueInput{Handle: handle, Prompt: "fixture"}},
	} {
		result := callMCPTool(t, session, call.name, call.input)
		if !result.IsError || !strings.Contains(toolResultText(result), "No provider work was started") || !strings.Contains(toolResultText(result), "Reconnect") {
			t.Fatalf("%s did not refuse stale code: %s", call.name, toolResultText(result))
		}
	}
}

func assertStaleControls(t *testing.T, session *mcp.ClientSession, root string) {
	t.Helper()
	run, conversation := newRunningMCPTestRun(t, root)
	freshnessWrite(t, run.Record.EventsPath, "durable event\n")
	selector := SelectorInput{Handle: conversation.Name}
	if result := callMCPTool(t, session, ToolInspect, selector); result.IsError {
		t.Fatalf("stale inspect: %s", toolResultText(result))
	}
	if result := callMCPTool(t, session, ToolOutput, OutputInput{Handle: conversation.Name, Artifact: "events"}); result.IsError || !strings.Contains(toolResultText(result), "durable event") {
		t.Fatalf("stale output: %s", toolResultText(result))
	}
	if result := decodeToolResult(t, callMCPTool(t, session, ToolCancel, selector)); result.State != dispatchStateCancelled {
		t.Fatalf("stale cancel: %+v", result)
	}
	if result := decodeToolResult(t, callMCPTool(t, session, ToolWait, WaitInput{Handle: conversation.Name})); result.State != dispatchStateCancelled {
		t.Fatalf("stale wait: %+v", result)
	}
}

func TestMCPFreshnessReleaseSelection(t *testing.T) {
	t.Setenv("AL_DEV_BYPASS_VERSION_DISPATCH", "")
	t.Setenv("AL_VERSION", "")
	t.Setenv("AL_NO_NETWORK", "1")
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	bin, marker := t.TempDir(), filepath.Join(t.TempDir(), "native-access")
	if err := os.WriteFile(filepath.Join(bin, AgentClaude), []byte("#!/bin/sh\ntouch \"$AL_TEST_NATIVE_MARKER\"\nexit 1\n"), 0o700); err != nil { // #nosec G306 -- synthetic native credential-access canary.
		t.Fatal(err)
	}
	t.Setenv("PATH", testPath(bin))
	t.Setenv("AL_TEST_NATIVE_MARKER", marker)
	tools := newMCPTestTools(root)
	tools.version = "v1.2.3"
	session := newMCPTestSession(t, tools)
	pin := filepath.Join(root, ".agent-layer", "al.version")
	freshnessWrite(t, pin, "1.2.4\n")
	for _, override := range []string{"", "1.2.5", "invalid"} {
		t.Setenv("AL_VERSION", override)
		assertStaleProviderTools(t, session, "unknown")
		assertStaleControls(t, session, root)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("stale server accessed native provider/credentials: %v", err)
	}
	for _, recovery := range []string{"override", "restored pin"} {
		if recovery == "override" {
			t.Setenv("AL_VERSION", "v1.2.3") // Override takes precedence over the changed pin.
		} else {
			t.Setenv("AL_VERSION", "")
			freshnessWrite(t, pin, "v1.2.3\n")
		}
		if result := callMCPTool(t, session, ToolOptions, OptionsInput{}); result.IsError {
			t.Fatalf("%s failed to recover: %s", recovery, toolResultText(result))
		}
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("recovery did not actually reach native discovery: %v", err)
		}
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
	}
	// Required pin evidence that cannot be read fails closed, before config or native access.
	if err := os.Remove(pin); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(pin, 0o700); err != nil {
		t.Fatal(err)
	}
	assertStaleProviderTools(t, session, "unknown")
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("unverifiable selection accessed native provider: %v", err)
	}
}

// Build two actual CLIs: replacements exercise kernel executable identity, normal
// CLI handoff and detached workers, without any installed provider or network.
func TestMCPFreshnessRealImages(t *testing.T) {
	builds := t.TempDir()
	build := func(name, version string) string {
		t.Helper()
		path := filepath.Join(builds, name)
		cmd := exec.Command("go", "build", "-ldflags=-X main.Version="+version+" -X main.Commit=fixture -X main.BuildDate=2026-10-15", "-o", path, "../../cmd/al")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, out)
		}
		return path
	}
	a, b := build("a", "1.2.3"), build("b", "1.2.4")
	copyImage := func(t *testing.T, from, to string) {
		t.Helper()
		data, err := os.ReadFile(from) // #nosec G304 -- test-built CLI in a temporary directory.
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(to, data, 0o700); err != nil { // #nosec G306 G703 -- test-owned temporary executable paths.
			t.Fatal(err)
		} // #nosec G306 -- temporary CLI executable.
	}
	connect := func(t *testing.T, root, image, bin string, env ...string) *mcp.ClientSession {
		t.Helper()
		cmd := exec.Command(image, "dispatch", "mcp-server")
		cmd.Dir = root
		cmd.Env = append([]string{"PATH=" + testPath(bin), "HOME=" + t.TempDir(), "AL_NO_NETWORK=1", "AL_CACHE_DIR=" + t.TempDir()}, env...)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		session, err := mcp.NewClient(&mcp.Implementation{Name: "freshness-test", Version: "test"}, nil).Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close() })
		return session
	}
	for _, mode := range []string{"development", "legacy bypass", "unpinned", "unpinned symlink"} {
		t.Run(mode, func(t *testing.T) {
			root, bin := writeDispatchRepo(t, dispatchRepoConfig{}), t.TempDir()
			image := filepath.Join(t.TempDir(), "al")
			if mode == "unpinned symlink" {
				if err := os.Symlink(a, image); err != nil {
					t.Fatal(err)
				}
			} else {
				copyImage(t, a, image)
			}
			var env []string
			if mode == "development" || mode == "legacy bypass" {
				freshnessWrite(t, filepath.Join(root, ".agent-layer", "al.version"), "0.22.0\n")
				env = []string{"AL_DEV_BYPASS_VERSION_DISPATCH=1"}
				if mode == "development" {
					env = append(env, "AL_DEV_EXECUTABLE="+image, "AL_DEV_SOURCE_ROOT="+root)
				}
			}
			marker := filepath.Join(t.TempDir(), "native-access")
			if err := os.WriteFile(filepath.Join(bin, AgentClaude), []byte("#!/bin/sh\ntouch \"$AL_TEST_NATIVE_MARKER\"\nexit 1\n"), 0o700); err != nil {
				t.Fatal(err)
			} // #nosec G306 -- synthetic provider.
			env = append(env, "AL_TEST_NATIVE_MARKER="+marker)
			session := connect(t, root, image, bin, env...)
			if err := os.Rename(image, image+".old"); err != nil {
				t.Fatal(err)
			}
			copyImage(t, b, image)
			assertStaleProviderTools(t, session, "unknown")
			assertStaleControls(t, session, root)
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("stale native access: %v", err)
			}
			// Reconnect to B selects its actual image, even when dev overrides an old pin.
			fresh := connect(t, root, image, bin, env...)
			if result := callMCPTool(t, fresh, ToolOptions, OptionsInput{}); result.IsError {
				t.Fatalf("reconnect: %s", toolResultText(result))
			}
			if err := os.Rename(image+".old", image); err != nil {
				t.Fatal(err)
			}
			if result := callMCPTool(t, session, ToolOptions, OptionsInput{}); result.IsError {
				t.Fatalf("restored image: %s", toolResultText(result))
			}
		})
	}
	t.Run("newer global delegates to older project pin", func(t *testing.T) {
		root, cache := writeDispatchRepo(t, dispatchRepoConfig{}), t.TempDir()
		cached := filepath.Join(cache, "versions", "1.2.3", runtime.GOOS+"-"+runtime.GOARCH, "al-"+runtime.GOOS+"-"+runtime.GOARCH)
		if err := os.MkdirAll(filepath.Dir(cached), 0o700); err != nil {
			t.Fatal(err)
		}
		copyImage(t, a, cached)
		freshnessWrite(t, filepath.Join(root, ".agent-layer", "al.version"), "1.2.3\n")
		for _, extra := range [][]string{
			nil,
			{"AL_DEV_BYPASS_VERSION_DISPATCH=1", "AL_DEV_EXECUTABLE=" + b, "AL_DEV_SOURCE_ROOT=" + t.TempDir()},
		} {
			session := connect(t, root, b, t.TempDir(), append([]string{"AL_CACHE_DIR=" + cache}, extra...)...)
			if got := session.InitializeResult().ServerInfo.Version; got != "1.2.3 (commit fixture, built 2026-10-15)" {
				t.Fatalf("global bypassed pin: %s", got)
			}
			if result := callMCPTool(t, session, ToolOptions, OptionsInput{}); result.IsError {
				t.Fatalf("pinned options: %s", toolResultText(result))
			}
			assertStaleControls(t, session, root)
		}

	})
	t.Run("admitted worker retains image after replacement", func(t *testing.T) {
		root, bin := writeDispatchRepo(t, dispatchRepoConfig{}), t.TempDir()
		image, signalDir := filepath.Join(t.TempDir(), "al"), t.TempDir()
		copyImage(t, a, image)
		stub := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then
 touch "$AL_TEST_SIGNALS/entered"
 while [ ! -f "$AL_TEST_SIGNALS/release" ]; do sleep 0.01; done
 printf '%%s\n' %q
 exit 0
fi
cat >/dev/null
printf '%%s\n' '{"type":"thread.started","thread_id":"11111111-1111-4111-8111-111111111111"}' '{"type":"item.completed","item":{"type":"agent_message","text":"same image"}}' '{"type":"turn.completed"}'
`, supportedProviderVersions[AgentCodex])
		if err := os.WriteFile(filepath.Join(bin, AgentCodex), []byte(stub), 0o700); err != nil {
			t.Fatal(err)
		} // #nosec G306 -- synthetic provider.
		session := connect(t, root, image, bin, "AL_DEV_EXECUTABLE="+image, "AL_DEV_SOURCE_ROOT="+root, "AL_DEV_BYPASS_VERSION_DISPATCH=1", "AL_TEST_SIGNALS="+signalDir)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		type response struct {
			result *mcp.CallToolResult
			err    error
		}
		done := make(chan response, 1)
		go func() {
			r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: ToolStart, Arguments: StartInput{Agent: AgentCodex, Model: "gpt-test", Prompt: "fixture"}})
			done <- response{r, err}
		}()
		waitForTestPath(t, filepath.Join(signalDir, "entered"))
		if err := os.Rename(image, image+".old"); err != nil {
			t.Fatal(err)
		}
		copyImage(t, b, image)
		freshnessWrite(t, filepath.Join(signalDir, "release"), "")
		started := <-done
		if started.err != nil {
			t.Fatal(started.err)
		}
		if runtime.GOOS != "linux" {
			if !started.result.IsError || !strings.Contains(toolResultText(started.result), "replaced") {
				t.Fatalf("replacement published work: %s", toolResultText(started.result))
			}
			return
		}
		result := decodeToolResult(t, started.result)
		finished := decodeToolResult(t, callMCPTool(t, session, ToolWait, WaitInput{Handle: result.Handle}))
		if finished.State != dispatchStateCompleted {
			t.Fatalf("worker failed: %+v", finished)
		}
		record, err := loadRunRecord(root, result.InvocationID)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(a) // #nosec G304 -- test-built admitted CLI image.
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(data)
		if record.AgentLayerBuild.SHA256 != hex.EncodeToString(hash[:]) {
			t.Fatalf("worker used replacement: %+v", record.AgentLayerBuild)
		}
		assertStaleProviderTools(t, session, result.Handle)
	})
}
