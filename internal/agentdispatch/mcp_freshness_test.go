package agentdispatch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	t.Setenv("AL_SHIM_ACTIVE", "")
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	bin, marker := t.TempDir(), filepath.Join(t.TempDir(), "native-access")
	if err := os.WriteFile(filepath.Join(bin, AgentClaude), []byte("#!/bin/sh\ntouch \"$AL_TEST_NATIVE_MARKER\"\nexit 1\n"), 0o700); err != nil { // #nosec G306 -- synthetic native credential-access canary.
		t.Fatal(err)
	}
	t.Setenv("PATH", testPath(bin))
	t.Setenv("AL_TEST_NATIVE_MARKER", marker)
	pin := filepath.Join(root, ".agent-layer", "al.version")
	freshnessWrite(t, pin, "v1.2.3\n")
	tools := newMCPTestTools(root)
	tools.version = "v1.2.3"
	tools.start = selectRelease(root, tools.version)
	session := newMCPTestSession(t, tools)
	assertFresh := func(label string) {
		t.Helper()
		if result := callMCPTool(t, session, ToolOptions, OptionsInput{}); result.IsError {
			t.Fatalf("%s: %s", label, toolResultText(result))
		}
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("%s did not actually reach native discovery: %v", label, err)
		}
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
	}
	assertFresh("unchanged pin")
	// Every change of selected version or provenance is stale: pin changes,
	// overrides (even of the same version), pin removal, and an empty or corrupt
	// pin falling back to the running version.
	for _, change := range []struct{ pin, override string }{
		{pin: "1.2.4\n"},
		{pin: "v1.2.3\n", override: "1.2.5"},
		{pin: "v1.2.3\n", override: "invalid"},
		{pin: "v1.2.3\n", override: "v1.2.3"},
		{},
		{pin: "\n"},
		{pin: "not-a-version\n"},
	} {
		t.Setenv("AL_VERSION", change.override)
		if change.pin == "" {
			if err := os.Remove(pin); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
		} else {
			freshnessWrite(t, pin, change.pin)
		}
		assertStaleProviderTools(t, session, "unknown")
		assertStaleControls(t, session, root)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("stale server accessed native provider/credentials: %v", err)
	}
	t.Setenv("AL_VERSION", "")
	freshnessWrite(t, pin, "1.2.3\n")
	assertFresh("restored pin")

	// An unpinned server started at the running version stays fresh until the
	// project selects something else, including a pin of the same version.
	if err := os.Remove(pin); err != nil {
		t.Fatal(err)
	}
	tools.start = selectRelease(root, tools.version)
	assertFresh("unpinned")
	freshnessWrite(t, pin, "1.2.3\n")
	assertStaleProviderTools(t, session, "unknown")
	if err := os.Remove(pin); err != nil {
		t.Fatal(err)
	}
	assertFresh("unpinned again")

	// Required pin evidence that cannot be read fails closed, before config or native access.
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
	for _, mode := range []string{"development fallback", "legacy bypass", "unpinned", "unpinned symlink"} {
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
			if mode == "development fallback" || mode == "legacy bypass" {
				freshnessWrite(t, filepath.Join(root, ".agent-layer", "al.version"), "0.22.0\n")
				env = []string{"AL_DEV_BYPASS_VERSION_DISPATCH=1"}
				if mode == "development fallback" {
					env = append(env, "AL_DEV_EXECUTABLE="+image, "AL_DEV_SOURCE_ROOT="+root)
					// A file cannot contain images/: force the supported run-in-place fallback.
					blockedCache := filepath.Join(t.TempDir(), "cache-file")
					freshnessWrite(t, blockedCache, "blocked")
					env = append(env, "AL_CACHE_DIR="+blockedCache)
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
			// Removing or corrupting the pin falls back to the running version here,
			// but a fresh launch would run the global executable instead.
			for _, change := range []string{"", "not-a-version\n", "1.2.3\n"} {
				pin := filepath.Join(root, ".agent-layer", "al.version")
				if change == "" {
					if err := os.Remove(pin); err != nil {
						t.Fatal(err)
					}
					assertStaleProviderTools(t, session, "unknown")
					continue
				}
				freshnessWrite(t, pin, change)
				if change == "1.2.3\n" {
					if result := callMCPTool(t, session, ToolOptions, OptionsInput{}); result.IsError {
						t.Fatalf("restored pin: %s", toolResultText(result))
					}
					continue
				}
				assertStaleProviderTools(t, session, "unknown")
			}
		}
	})
	t.Run("pinned server survives global replacement", func(t *testing.T) {
		root, bin, cache := writeDispatchRepo(t, dispatchRepoConfig{}), t.TempDir(), t.TempDir()
		image := filepath.Join(t.TempDir(), "al")
		copyImage(t, a, image)
		pin := filepath.Join(root, ".agent-layer", "al.version")
		freshnessWrite(t, pin, "1.2.3\n")
		stub := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then printf '%%s\n' %q; exit 0; fi
cat >/dev/null
printf '%%s\n' '{"type":"thread.started","thread_id":"11111111-1111-4111-8111-111111111111"}' '{"type":"item.completed","item":{"type":"agent_message","text":"pinned image"}}' '{"type":"turn.completed"}'
`, supportedProviderVersions[AgentCodex])
		if err := os.WriteFile(filepath.Join(bin, AgentCodex), []byte(stub), 0o700); err != nil {
			t.Fatal(err)
		} // #nosec G306 -- synthetic provider.
		session := connect(t, root, image, bin, "AL_CACHE_DIR="+cache)
		data, err := os.ReadFile(a) // #nosec G304 -- test-built CLI image.
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		copied := filepath.Join(cache, "images", hex.EncodeToString(sum[:]), "al")
		if got, err := os.ReadFile(copied); err != nil || sha256.Sum256(got) != sum { // #nosec G304 G703 -- test cache path.
			t.Fatalf("pinned server did not run from a verified image copy: %v", err)
		}
		if entries, _ := os.ReadDir(filepath.Join(cache, "versions")); len(entries) != 0 {
			t.Fatalf("image handoff wrote the versions cache: %v", entries)
		}
		// al update replaces the installed entry; the unchanged pin keeps the server fresh.
		if err := os.Rename(image, image+".old"); err != nil {
			t.Fatal(err)
		}
		copyImage(t, b, image)
		if err := os.Remove(image + ".old"); err != nil {
			t.Fatal(err)
		}
		if result := callMCPTool(t, session, ToolOptions, OptionsInput{}); result.IsError {
			t.Fatalf("pinned server went stale after global replacement: %s", toolResultText(result))
		}
		// Darwin relaunches workers from the image path, so admitted work must run A.
		started := decodeToolResult(t, callMCPTool(t, session, ToolStart, StartInput{Agent: AgentCodex, Model: "gpt-test", Prompt: "fixture"}))
		if finished := decodeToolResult(t, callMCPTool(t, session, ToolWait, WaitInput{Handle: started.Handle})); finished.State != dispatchStateCompleted {
			t.Fatalf("worker after global replacement: %+v", finished)
		}
		record, err := loadRunRecord(root, started.InvocationID)
		if err != nil {
			t.Fatal(err)
		}
		if record.AgentLayerBuild.SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatalf("worker did not run the pinned image: %+v", record.AgentLayerBuild)
		}
		for _, change := range []string{"1.2.4\n", "", "not-a-version\n"} {
			if change == "" {
				if err := os.Remove(pin); err != nil {
					t.Fatal(err)
				}
			} else {
				freshnessWrite(t, pin, change)
			}
			assertStaleProviderTools(t, session, started.Handle)
		}
		freshnessWrite(t, pin, "1.2.3\n")
		if result := callMCPTool(t, session, ToolOptions, OptionsInput{}); result.IsError {
			t.Fatalf("restored pin: %s", toolResultText(result))
		}
		// Another server of the same release reuses the verified image entry.
		other := filepath.Join(t.TempDir(), "al")
		copyImage(t, a, other)
		second := connect(t, root, other, bin, "AL_CACHE_DIR="+cache, "AL_VERSION=1.2.3")
		if result := callMCPTool(t, second, ToolOptions, OptionsInput{}); result.IsError {
			t.Fatalf("second pinned server: %s", toolResultText(result))
		}
		if entries, err := os.ReadDir(filepath.Join(cache, "images")); err != nil || len(entries) != 1 {
			t.Fatalf("image entry was not reused: %v %v", entries, err)
		}
	})
	t.Run("development snapshot with unverifiable startup identity", func(t *testing.T) {
		root, cache := writeDispatchRepo(t, dispatchRepoConfig{}), t.TempDir()
		sum, err := imageSHA256(a)
		if err != nil {
			t.Fatal(err)
		}
		for _, identity := range []string{"invalid", sum} {
			runner := filepath.Join(cache, "images", identity, "al")
			if err := os.MkdirAll(filepath.Dir(runner), 0o700); err != nil {
				t.Fatal(err)
			}
			copyImage(t, a, runner)
			// An image-cache path alone is insufficient: the selected dev build
			// must match its content at startup as well as its cache key.
			session := connect(t, root, runner, t.TempDir(), "AL_CACHE_DIR="+cache, "AL_DEV_EXECUTABLE="+b, "AL_DEV_SOURCE_ROOT="+root, "AL_DEV_BYPASS_VERSION_DISPATCH=1")
			assertStaleProviderTools(t, session, "unknown")
			if identity == sum {
				result := callMCPTool(t, session, ToolOptions, OptionsInput{})
				if !strings.Contains(toolResultText(result), "development executable changed during server startup") {
					t.Fatalf("startup replacement was not identified: %s", toolResultText(result))
				}
			}
		}
	})
	t.Run("unpinned dispatched server tracks the installed entry", func(t *testing.T) {
		root, bin := writeDispatchRepo(t, dispatchRepoConfig{}), t.TempDir()
		runner, installed := filepath.Join(t.TempDir(), "al-cached"), filepath.Join(t.TempDir(), "al")
		copyImage(t, a, runner)
		copyImage(t, a, installed)
		session := connect(t, root, runner, bin, "AL_SHIM_ACTIVE=1", "AL_INSTALLED_EXECUTABLE="+installed)
		if result := callMCPTool(t, session, ToolOptions, OptionsInput{}); result.IsError {
			t.Fatalf("unpinned dispatched server: %s", toolResultText(result))
		}
		if err := os.Rename(installed, installed+".old"); err != nil {
			t.Fatal(err)
		}
		copyImage(t, b, installed)
		assertStaleProviderTools(t, session, "unknown")

		// Without the installed entry a global replacement is undetectable, so
		// the server fails closed from the start.
		for _, entry := range []string{"", "AL_INSTALLED_EXECUTABLE="} {
			env := []string{"AL_SHIM_ACTIVE=1"}
			if entry != "" {
				env = append(env, entry)
			}
			unverifiable := connect(t, root, runner, bin, env...)
			result := callMCPTool(t, unverifiable, ToolOptions, OptionsInput{})
			if !result.IsError || !strings.Contains(toolResultText(result), "AL_INSTALLED_EXECUTABLE is unset") {
				t.Fatalf("unverifiable unpinned server stayed fresh: %s", toolResultText(result))
			}
			assertStaleControls(t, unverifiable, root)
		}
	})
	t.Run("development snapshot retains server and worker image after replacement", func(t *testing.T) {
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
		// Let initial options discovery finish, then re-arm the version barrier
		// so the later start is still admitted before replacement.
		freshnessWrite(t, filepath.Join(signalDir, "release"), "")
		session := connect(t, root, image, bin, "AL_DEV_EXECUTABLE="+image, "AL_DEV_SOURCE_ROOT="+root, "AL_DEV_BYPASS_VERSION_DISPATCH=1", "AL_TEST_SIGNALS="+signalDir)
		initial := callMCPTool(t, session, ToolOptions, OptionsInput{})
		if initial.IsError {
			t.Fatalf("initial dev options: %s", toolResultText(initial))
		}
		var before OptionsResponse
		if err := json.Unmarshal([]byte(toolResultText(initial)), &before); err != nil {
			t.Fatal(err)
		}
		if before.Server == nil || !before.Server.Snapshot || before.Server.Development == nil || before.Server.Development.Replaced || before.Server.Development.Notice != "" {
			t.Fatalf("unchanged dev snapshot options: %+v", before.Server)
		}
		for _, name := range []string{"entered", "release"} {
			if err := os.Remove(filepath.Join(signalDir, name)); err != nil {
				t.Fatal(err)
			}
		}
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
		optionsResult := callMCPTool(t, session, ToolOptions, OptionsInput{})
		if optionsResult.IsError {
			t.Fatalf("rebuilt dev server refused options: %s", toolResultText(optionsResult))
		}
		var options OptionsResponse
		if err := json.Unmarshal([]byte(toolResultText(optionsResult)), &options); err != nil {
			t.Fatal(err)
		}
		if options.Server == nil || options.Server.Version != "1.2.3" || options.Server.SHA256 != hex.EncodeToString(hash[:]) || !options.Server.Snapshot || options.Server.Development == nil || !options.Server.Development.Replaced || options.Server.Development.Notice == "" {
			t.Fatalf("missing running/replacement identity: %+v", options.Server)
		}
		// New work after the replacement must still use A, including continuation.
		for _, call := range []struct {
			name  string
			input any
		}{
			{ToolStart, StartInput{Agent: AgentCodex, Model: "gpt-test", Prompt: "fixture"}},
			{ToolContinue, ContinueInput{Handle: result.Handle, Prompt: "fixture"}},
		} {
			next := decodeToolResult(t, callMCPTool(t, session, call.name, call.input))
			if finished := decodeToolResult(t, callMCPTool(t, session, ToolWait, WaitInput{Handle: next.Handle})); finished.State != dispatchStateCompleted {
				t.Fatalf("worker after rebuild: %+v", finished)
			}
			record, err := loadRunRecord(root, next.InvocationID)
			if err != nil {
				t.Fatal(err)
			}
			if record.AgentLayerBuild.SHA256 != hex.EncodeToString(hash[:]) {
				t.Fatalf("worker used newer build: %+v", record.AgentLayerBuild)
			}
		}
		// Regeneration must continue to launch the current dev entry, without
		// recording the server's snapshot as an installed release fallback.
		generated, err := os.ReadFile(filepath.Join(root, ".codex", "config.toml")) // #nosec G304 -- test-owned generated launcher.
		if err != nil {
			t.Fatal(err)
		}
		snapshot := filepath.Join("images", hex.EncodeToString(hash[:]), "al")
		if strings.Contains(string(generated), snapshot) || !strings.Contains(string(generated), image) {
			t.Fatalf("regenerated launcher lost dev provenance: %s", generated)
		}
		// Losing the selected executable is unverifiable rather than a verified rebuild.
		if err := os.Remove(image); err != nil {
			t.Fatal(err)
		}
		assertStaleProviderTools(t, session, result.Handle)
	})
}
