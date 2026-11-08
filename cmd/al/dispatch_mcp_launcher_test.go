//go:build unix

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/agentdispatch"
	"github.com/conn-castle/agent-layer/internal/projection"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
)

const (
	launcherHelperArgv0  = "AL_TEST_MCP_LAUNCHER_ARGV0"
	launcherHelperRecord = "AL_TEST_MCP_LAUNCHER_RECORD"
	// launcherHelperVersion is the release version the wrapped CLI reports.
	launcherHelperVersion = "AL_TEST_MCP_LAUNCHER_VERSION"
	launcherGUIPath       = "/usr/bin:/bin:/usr/sbin:/sbin"
)

// TestDispatchMCPLauncherHelper runs the real CLI as an installed release al.
// The wrapper passes its own path as argv[0]; when started as the MCP server it
// first records the exact environment the generated launcher provided.
func TestDispatchMCPLauncherHelper(t *testing.T) {
	argv0 := os.Getenv(launcherHelperArgv0)
	if argv0 == "" {
		return
	}
	record := os.Getenv(launcherHelperRecord)
	Version = os.Getenv(launcherHelperVersion)
	for _, name := range []string{launcherHelperArgv0, launcherHelperRecord, launcherHelperVersion} {
		_ = os.Unsetenv(name)
	}
	args := flag.Args()
	if slices.Equal(args, []string{"dispatch", "mcp-server"}) {
		data, err := json.Marshal(os.Environ())
		if err == nil {
			err = os.WriteFile(record, data, 0o600) // #nosec G703 -- test-owned record path from the wrapper fixture.
		}
		if err != nil {
			os.Exit(90)
		}
	}
	os.Args = append([]string{argv0}, args...)
	main()
}

// TestDispatchMCPLauncherStartsWithoutTerminalPath reproduces #513: a GUI or
// background MCP client whose PATH omits the al install directory must still
// start the generated launcher, complete MCP discovery, and regenerate the same
// configuration from inside the server it launched.
func TestDispatchMCPLauncherStartsWithoutTerminalPath(t *testing.T) {
	base := t.TempDir()
	root := writeLauncherProject(t, filepath.Join(base, "repo"))
	installDir := filepath.Join(base, "install bin")
	record := filepath.Join(base, "server-env.json")
	installed := writeLauncherWrapper(t, filepath.Join(installDir, "al"), record, "0.0.292")
	home := filepath.Join(base, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(base, "cache")
	baseEnv := []string{"HOME=" + home, versiondispatch.EnvNoNetwork + "=1", versiondispatch.EnvCacheDir + "=" + cacheDir}
	withPath := func(path string) []string { return append(slices.Clone(baseEnv), "PATH="+path) }

	// A terminal sync resolves al from PATH and records it in the launcher.
	runLauncherAL(t, root, withPath(installDir+":/usr/bin:/bin"), installed, "sync")
	generated := readGeneratedLauncherConfig(t, root)
	command, args := codexDispatchLauncher(t, generated)
	if !slices.Contains(args, installed) {
		t.Fatalf("launcher args %q do not record %s", args, installed)
	}

	// The GUI client lacks installDir; the launcher still starts the server.
	requireDispatchTools(t, command, args, withPath(launcherGUIPath))
	serverEnv := readLauncherServerEnv(t, record)
	if got := envValue(serverEnv, "PATH"); got != launcherGUIPath+":"+installDir {
		t.Fatalf("server PATH = %q, want client PATH with %s appended", got, installDir)
	}
	if got := envValue(serverEnv, versiondispatch.EnvInstalledExecutable); got != installed {
		t.Fatalf("server %s = %q, want %q", versiondispatch.EnvInstalledExecutable, got, installed)
	}

	// Dispatch regenerates inside that server; its real environment must
	// reproduce the same launcher, which must still start.
	runLauncherAL(t, root, serverEnv, installed, "sync")
	if regenerated := readGeneratedLauncherConfig(t, root); regenerated != generated {
		t.Fatalf("regeneration from the server environment changed config:\n%s\nwant:\n%s", regenerated, generated)
	}
	requireDispatchTools(t, command, args, withPath(launcherGUIPath))

	// Another al earlier on the client PATH cannot replace the recorded entry
	// when the server regenerates.
	otherDir := filepath.Join(base, "other")
	if err := os.Mkdir(otherDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "al"), []byte("#!/bin/sh\nexit 97\n"), 0o700); err != nil { // #nosec G306 -- test-owned executable fixture.
		t.Fatal(err)
	}
	requireDispatchTools(t, command, args, withPath(otherDir+":"+launcherGUIPath))
	runLauncherAL(t, root, readLauncherServerEnv(t, record), installed, "sync")
	if regenerated := readGeneratedLauncherConfig(t, root); regenerated != generated {
		t.Fatalf("regeneration with another al on PATH changed config:\n%s\nwant:\n%s", regenerated, generated)
	}

	// An absolute invocation without installDir on PATH records the same entry.
	if err := os.Remove(filepath.Join(root, ".codex", "config.toml")); err != nil {
		t.Fatal(err)
	}
	runLauncherAL(t, root, withPath("/usr/bin:/bin"), installed, "sync")
	if regenerated := readGeneratedLauncherConfig(t, root); regenerated != generated {
		t.Fatalf("absolute invocation changed config:\n%s\nwant:\n%s", regenerated, generated)
	}

	// A project pin re-execs a cached release whose argv[0] is its cache path.
	// Offline, success proves the cached binary ran; it must still record the
	// installed entry, both from a restricted-PATH sync and inside the server.
	const pinned = "1.2.3"
	platform := runtime.GOOS + "-" + runtime.GOARCH
	writeLauncherWrapper(t, filepath.Join(cacheDir, "versions", pinned, platform, "al-"+platform), record, pinned)
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "al.version"), []byte(pinned+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ".codex", "config.toml")); err != nil {
		t.Fatal(err)
	}
	runLauncherAL(t, root, withPath("/usr/bin:/bin"), installed, "sync")
	if regenerated := readGeneratedLauncherConfig(t, root); regenerated != generated {
		t.Fatalf("pinned re-exec changed config:\n%s\nwant:\n%s", regenerated, generated)
	}
	requireDispatchTools(t, command, args, withPath(launcherGUIPath))
	serverEnv = readLauncherServerEnv(t, record)
	if envValue(serverEnv, versiondispatch.EnvShimActive) != "1" || envValue(serverEnv, versiondispatch.EnvInstalledExecutable) != installed {
		t.Fatalf("pinned server env lacks re-exec marker or installed entry %s: %q", installed, serverEnv)
	}
}

func writeLauncherProject(t *testing.T, root string) string {
	t.Helper()
	for _, dir := range []string{"instructions", "skills"} {
		if err := os.MkdirAll(filepath.Join(root, ".agent-layer", dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"env", "commands.allow"} {
		if err := os.WriteFile(filepath.Join(root, ".agent-layer", name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"config.toml", "gitignore.block"} {
		data, err := templates.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".agent-layer", name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// writeLauncherWrapper installs path as a non-development release executable
// that runs this test binary's CLI helper as the given version.
func writeLauncherWrapper(t *testing.T, path, record, version string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'" }
	script := "#!/bin/sh\n" + launcherHelperArgv0 + `=$0 ` + launcherHelperRecord + "=" + quote(record) + " " + launcherHelperVersion + "=" + quote(version) + " exec " + quote(self) + ` -test.run='^TestDispatchMCPLauncherHelper$' -- "$@"` + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { // #nosec G306 -- test-owned executable fixture.
		t.Fatal(err)
	}
	return path
}

func runLauncherAL(t *testing.T, root string, env []string, al string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, al, args...) // #nosec G204 -- test-owned wrapper.
	cmd.Dir = root
	cmd.Env = env
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("al %v: %v\n%s", args, err, output)
	}
}

func readGeneratedLauncherConfig(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".codex", "config.toml")) // #nosec G304 -- test-owned generated file.
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func codexDispatchLauncher(t *testing.T, config string) (string, []string) {
	t.Helper()
	var parsed struct {
		MCPServers map[string]struct {
			Command string   `toml:"command"`
			Args    []string `toml:"args"`
		} `toml:"mcp_servers"`
	}
	if err := toml.Unmarshal([]byte(config), &parsed); err != nil {
		t.Fatal(err)
	}
	server, ok := parsed.MCPServers[projection.BuiltInDispatchServerID]
	if !ok || server.Command == "" {
		t.Fatalf("generated Codex config lacks the %s server:\n%s", projection.BuiltInDispatchServerID, config)
	}
	return server.Command, server.Args
}

func readLauncherServerEnv(t *testing.T, record string) []string {
	t.Helper()
	data, err := os.ReadFile(record) // #nosec G304 -- test-owned record.
	if err != nil {
		t.Fatal(err)
	}
	var env []string
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	return env
}

func envValue(env []string, name string) string {
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, name+"="); ok {
			return value
		}
	}
	return ""
}

// requireDispatchTools starts the generated launcher the way an MCP client
// does and requires initialize plus tools/list to expose the dispatch tools.
func requireDispatchTools(t *testing.T, command string, args []string, env []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...) // #nosec G204 -- generated launcher under test.
	cmd.Dir = t.TempDir()
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	exchange := func(id int, request string) json.RawMessage {
		t.Helper()
		if _, err := stdin.Write([]byte(request + "\n")); err != nil {
			t.Fatalf("write %d: %v (stderr: %s)", id, err, stderr.String())
		}
		if id == 0 {
			return nil
		}
		if !scanner.Scan() {
			t.Fatalf("missing response %d: %v (stderr: %s)", id, scanner.Err(), stderr.String())
		}
		var msg struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil || msg.ID != id || len(msg.Error) != 0 {
			t.Fatalf("unexpected MCP response %s: %v", scanner.Text(), err)
		}
		return msg.Result
	}
	exchange(1, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"gui-client","version":"test"}}}`)
	exchange(0, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	var listed struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(exchange(2, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`), &listed); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	want := []string{agentdispatch.ToolCancel, agentdispatch.ToolContinue, agentdispatch.ToolInspect, agentdispatch.ToolOptions, agentdispatch.ToolOutput, agentdispatch.ToolStart, agentdispatch.ToolWait}
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("listed tools = %v, want %v", names, want)
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("server exit after client EOF: %v (stderr: %s)", err, stderr.String())
	}
}
