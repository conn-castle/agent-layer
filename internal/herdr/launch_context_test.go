package herdr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMuseLaunchContextChildProcess exercises the production boundary: a
// launch process writes its record, and a separately-created hook process
// receives only HOME/PATH. The hook must find the launch process in its
// bounded ancestry before it can send a HerdR report.
func TestMuseLaunchContextChildProcess(t *testing.T) {
	mode, root := herdrContextTestMode()
	if mode == "launch" {
		sessionID := os.Getenv("AL_TEST_HERDR_CONTEXT_SESSION")
		if sessionID == "" {
			sessionID = "child-session"
		}
		runDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "child")
		if err := os.MkdirAll(runDir, 0o700); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := CaptureLaunch(root, runDir, os.Environ(), providerMuse); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		child := exec.Command(os.Args[0], "-test.run=^TestMuseLaunchContextChildProcess$", "--", "hook", root) // #nosec G702 -- test binary and arguments are test-controlled.
		child.Env = []string{"HOME=" + filepath.Join(root, "home"), "PATH=" + os.Getenv("PATH"), "AL_TEST_HERDR_CONTEXT_MODE=hook", "AL_TEST_HERDR_CONTEXT_ROOT=" + root, "AL_TEST_HERDR_CONTEXT_SESSION=" + sessionID}
		child.Stdin = strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"` + sessionID + `"}`)
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if mode == "hook" {
		if err := HandleForRoot("muse", root, os.Stdin, io.Discard, os.Stderr, os.Environ()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if mode == "dispatch-parent" || mode == "dispatch-worker" {
		canonicalRoot, err := canonicalDirectory(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		_, start, err := processLineage(os.Getpid())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		prior := filepath.Join(root, ".agent-layer", "tmp", "runs", "prior")
		boundary := filepath.Join(root, ".agent-layer", "tmp", "runs", "dispatch")
		if err := os.MkdirAll(prior, 0o700); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := os.MkdirAll(boundary, 0o700); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		childMode := "hook"
		if mode == "dispatch-parent" {
			data, _ := json.Marshal(launchContext{Version: 1, PID: os.Getpid(), ProcessStart: start, ProjectRoot: canonicalRoot, Provider: "muse", SocketPath: "/missing", PaneID: "w1:p1"})
			if err := os.WriteFile(filepath.Join(prior, launchContextName(os.Getpid(), start)), data, 0o600); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			childMode = "dispatch-worker"
		} else {
			if err := WriteBoundary(root, boundary); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		child := exec.Command(os.Args[0], "-test.run=^TestMuseLaunchContextChildProcess$", "--", childMode, root) //nolint:gosec // standard test re-exec pattern
		child.Env = []string{"PATH=" + os.Getenv("PATH"), "AL_TEST_HERDR_CONTEXT_MODE=" + childMode, "AL_TEST_HERDR_CONTEXT_ROOT=" + root}
		child.Stdin = strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"child"}`)
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	socket, reports, done := fakeHerdR(t, fakeHerdROptions{})
	root = t.TempDir()
	launchDir := filepath.Join(root, "subdirectory")
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer", "tmp", "runs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(launchDir, 0o700); err != nil {
		t.Fatal(err)
	}
	absoluteSocket, err := filepath.Abs(socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(absoluteSocket), filepath.Join(root, "s")); err != nil {
		t.Fatal(err)
	}
	// The source AL checkout may differ from the launched client repository.
	devExecutable := filepath.Join(t.TempDir(), "candidate", "al")
	if err := os.MkdirAll(filepath.Dir(devExecutable), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(devExecutable, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	launch := exec.Command(os.Args[0], "-test.run=^TestMuseLaunchContextChildProcess$", "--", "launch", root) // #nosec G702 -- test binary and arguments are test-controlled.
	launch.Dir = launchDir
	launch.Env = []string{
		"HERDR_ENV=1", "HERDR_SOCKET_PATH=../s/herdr.sock", "HERDR_PANE_ID=w1:p1",
		"AL_DEV_EXECUTABLE=" + devExecutable, "AL_DEV_BYPASS_VERSION_DISPATCH=1",
		"HOME=" + filepath.Join(root, "home"), "PATH=" + os.Getenv("PATH"), "AL_TEST_HERDR_CONTEXT_MODE=launch", "AL_TEST_HERDR_CONTEXT_ROOT=" + root,
	}
	output, err := launch.CombinedOutput()
	if err != nil {
		t.Fatalf("launch/hook child failed: %v\n%s", err, output)
	}
	waitFakeHerdR(t, done)
	params := (<-reports)["params"].(map[string]any)
	argv := interfaceStrings(params["resume_argv"])
	canonicalDev, err := filepath.EvalSymlinks(devExecutable)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"AL_DEV_BYPASS_VERSION_DISPATCH=1", "AL_DEV_EXECUTABLE=" + canonicalDev, canonicalDev, "muse", "resume", "child-session"}
	if len(argv) < 2 || argv[0] != commandEnv || !strings.HasPrefix(argv[1], envRecoveryGeneration+"=") || !equalStrings(argv[2:], want) {
		t.Fatalf("persisted argv = %#v, want generation-prefixed %#v", argv, want)
	}
	for _, argument := range argv {
		if strings.HasPrefix(argument, "HOME=") || strings.HasPrefix(argument, "PATH=") {
			t.Fatalf("unsafe hook context persisted in argv: %#v", argv)
		}
	}
}

func TestDispatchBoundaryStopsSanitisedChildFromBorrowingAncestor(t *testing.T) {
	root := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=^TestMuseLaunchContextChildProcess$", "--", "dispatch-parent", root) //nolint:gosec // standard test re-exec pattern
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "AL_TEST_HERDR_CONTEXT_MODE=dispatch-parent", "AL_TEST_HERDR_CONTEXT_ROOT=" + root}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("dispatch child crossed recovery boundary: %v\n%s", err, output)
	}
}

func TestMuseLaunchContextRejectsStaleAndWrongProjectRecords(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	runs := filepath.Join(root, ".agent-layer", "tmp", "runs", "stale")
	if err := os.MkdirAll(runs, 0o700); err != nil {
		t.Fatal(err)
	}
	_, start, err := processLineage(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	stale := launchContext{Version: 1, PID: os.Getpid(), ProcessStart: "reused-process", ProjectRoot: canonicalRoot, Provider: "muse", SocketPath: "/tmp/herdr.sock", PaneID: "w1:p1"}
	path := filepath.Join(runs, launchContextName(os.Getpid(), stale.ProcessStart))
	writeLaunchContextFixture(t, path, stale)
	canonicalRuns := filepath.Join(canonicalRoot, ".agent-layer", "tmp", "runs")
	if matches, err := filepath.Glob(filepath.Join(canonicalRuns, "*", launchContextName(os.Getpid(), stale.ProcessStart))); err != nil || len(matches) != 1 {
		t.Fatalf("stale record not discoverable: matches=%#v err=%v", matches, err)
	}
	if _, found, err := resolveLaunchContext(root, "muse"); err != nil || found {
		t.Fatalf("stale process identity was accepted: found=%t err=%v", found, err)
	}

	other := t.TempDir()
	canonicalOther, err := canonicalDirectory(other)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(other, ".agent-layer", "tmp", "runs", "other"), 0o700); err != nil {
		t.Fatal(err)
	}
	validOther := launchContext{Version: 1, PID: os.Getpid(), ProcessStart: start, ProjectRoot: canonicalOther, Provider: "muse", SocketPath: "/tmp/herdr.sock", PaneID: "w1:p1"}
	writeLaunchContextFixture(t, filepath.Join(other, ".agent-layer", "tmp", "runs", "other", launchContextName(os.Getpid(), start)), validOther)
	if _, found, err := resolveLaunchContext(other, "muse"); err != nil || !found {
		t.Fatalf("other project record was not independently resolvable: found=%t err=%v", found, err)
	}
	// The first project's stale record cannot make a hook borrow the second
	// project's live record.
	if _, found, err := resolveLaunchContext(root, "muse"); err != nil || found {
		t.Fatalf("project resolution borrowed a context across roots: found=%t err=%v", found, err)
	}
}

func TestLaunchContextLookupSupportsLiteralGlobCharactersInProjectRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project:[*]?")
	runs := filepath.Join(root, ".agent-layer", "tmp", "runs", "run")
	if err := os.MkdirAll(runs, 0o700); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	_, start, err := processLineage(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	context := launchContext{Version: 1, PID: os.Getpid(), ProcessStart: start, ProjectRoot: canonicalRoot, Provider: providerMuse, SocketPath: "/tmp/herdr.sock", PaneID: "w1:p1"}
	writeLaunchContextFixture(t, filepath.Join(runs, launchContextName(os.Getpid(), start)), context)
	if _, found, err := resolveLaunchContext(root, providerMuse); err != nil || !found {
		t.Fatalf("literal-character project root record = found %t, err %v", found, err)
	}
}

func TestLaunchContextLookupIgnoresRunRemovedDuringScan(t *testing.T) {
	root := t.TempDir()
	runsDir := filepath.Join(root, ".agent-layer", "tmp", "runs")
	vanished := filepath.Join(runsDir, "a-removed-dispatch")
	live := filepath.Join(runsDir, "b-live")
	for _, directory := range []string{vanished, live} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	_, start, err := processLineage(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	context := launchContext{Version: 1, PID: os.Getpid(), ProcessStart: start, ProjectRoot: canonicalRoot, Provider: providerMuse, SocketPath: "/tmp/herdr.sock", PaneID: "w1:p1"}
	writeLaunchContextFixture(t, filepath.Join(live, launchContextName(os.Getpid(), start)), context)
	original := readDirFunc
	t.Cleanup(func() { readDirFunc = original })
	readDirFunc = func(name string) ([]os.DirEntry, error) {
		entries, err := original(name)
		if filepath.Base(name) == "runs" {
			// Simulate an unrelated dispatch cleanup after the run snapshot.
			if removeErr := os.RemoveAll(vanished); removeErr != nil {
				t.Fatal(removeErr)
			}
		}
		return entries, err
	}
	if _, found, err := resolveLaunchContext(root, providerMuse); err != nil || !found {
		t.Fatalf("run removed during scan blocked lookup: found=%t err=%v", found, err)
	}
}

func TestMuseLaunchContextConcurrentSameProject(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer", "tmp", "runs"), 0o700); err != nil {
		t.Fatal(err)
	}
	devExecutable := filepath.Join(root, "candidate", "al")
	if err := os.MkdirAll(filepath.Dir(devExecutable), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(devExecutable, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	type launchResult struct {
		output []byte
		err    error
	}
	results := make(chan launchResult, 2)
	socket, reports, done := fakeHerdR(t, fakeHerdROptions{reportsExpected: 2, paneGetsExpected: 2, distinctPanes: true})
	absoluteSocket, err := filepath.Abs(socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(absoluteSocket), filepath.Join(root, "s")); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		sessionID := fmt.Sprintf("concurrent-%d", index)
		go func(sessionID string, pane int) {
			command := exec.Command(os.Args[0], "-test.run=^TestMuseLaunchContextChildProcess$", "--", "launch", root) // #nosec G702 -- test binary and arguments are test-controlled.
			command.Dir = root
			command.Env = []string{"HERDR_ENV=1", "HERDR_SOCKET_PATH=s/herdr.sock", fmt.Sprintf("HERDR_PANE_ID=w1:p%d", pane), "AL_DEV_EXECUTABLE=" + devExecutable, "AL_DEV_BYPASS_VERSION_DISPATCH=1", "HOME=" + filepath.Join(root, "home"), "PATH=" + os.Getenv("PATH"), "AL_TEST_HERDR_CONTEXT_MODE=launch", "AL_TEST_HERDR_CONTEXT_ROOT=" + root, "AL_TEST_HERDR_CONTEXT_SESSION=" + sessionID}
			output, err := command.CombinedOutput()
			results <- launchResult{output, err}
		}(sessionID, index+1)
	}
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent launch/hook child failed: %v\n%s", result.err, result.output)
		}
	}
	seen := map[string]bool{}
	waitFakeHerdR(t, done)
	for range 2 {
		params := (<-reports)["params"].(map[string]any)
		argv := interfaceStrings(params["resume_argv"])
		seen[argv[len(argv)-1]] = true
	}
	if !seen["concurrent-0"] || !seen["concurrent-1"] {
		t.Fatalf("concurrent contexts crossed or were lost: %#v", seen)
	}
}

func TestMuseLaunchContextHasNoEffectWithoutARecord(t *testing.T) {
	root := t.TempDir()
	var stderr bytes.Buffer
	if err := HandleForRoot("muse", root, strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"id"}`), io.Discard, &stderr, []string{"HOME=/tmp", "PATH=/bin"}); err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("ordinary non-HerdR Muse hook emitted %q", stderr.String())
	}
}

func TestMuseLaunchContextRejectsSymlinkedRecord(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "run")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	_, start, err := processLineage(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "record.json")
	writeLaunchContextFixture(t, external, launchContext{Version: 1, PID: os.Getpid(), ProcessStart: start, ProjectRoot: canonicalRoot, Provider: "muse", SocketPath: "/tmp/herdr.sock", PaneID: "w1:p1"})
	if err := os.Symlink(external, filepath.Join(runDir, launchContextName(os.Getpid(), start))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveLaunchContext(root, "muse"); err == nil {
		t.Fatal("symlinked context record was accepted")
	}
}

func herdrContextTestMode() (string, string) {
	if mode, root := os.Getenv("AL_TEST_HERDR_CONTEXT_MODE"), os.Getenv("AL_TEST_HERDR_CONTEXT_ROOT"); mode != "" && root != "" {
		return mode, root
	}
	for index, argument := range os.Args {
		if argument == "--" && index+2 < len(os.Args) {
			return os.Args[index+1], os.Args[index+2]
		}
	}
	return "", ""
}

func TestCaptureLaunchResolvesRelativeCodexHome(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "rel-home")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "codex-home"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	env := []string{EnvEnabled + "=1", EnvSocketPath + "=/tmp/herdr.sock", EnvPaneID + "=w1:p1", "CODEX_HOME=codex-home"}
	if err := CaptureLaunch(root, runDir, env, providerCodex); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	_, start, err := processLineage(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	context, err := decodeLaunchContext(filepath.Join(canonicalRoot, ".agent-layer", "tmp", "runs", "rel-home", launchContextName(os.Getpid(), start)), canonicalRoot)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(filepath.Join(canonicalRoot, "codex-home"))
	if context.CodexHome != want {
		t.Fatalf("CODEX_HOME = %q, want %q", context.CodexHome, want)
	}
}

func TestLiveCodexLaunchRecordsUsesIndexInsteadOfHistoricalRuns(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	_, start, err := processLineage(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	liveDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "live")
	historyDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "historical")
	for _, directory := range []string{liveDir, historyDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	live := launchContext{Version: 2, PID: os.Getpid(), ProcessStart: start, ProjectRoot: canonicalRoot, Provider: providerCodex, SocketPath: "/tmp/herdr.sock", PaneID: "w1:p1", CodexHome: filepath.Join(root, ".codex")}
	historical := live
	historical.PaneID = "w1:p2"
	livePath := filepath.Join(liveDir, launchContextName(os.Getpid(), start))
	writeLaunchContextFixture(t, livePath, live)
	writeLaunchContextFixture(t, filepath.Join(historyDir, launchContextName(os.Getpid(), start)), historical)
	if err := rememberCodexLiveLaunch(canonicalRoot, livePath); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := os.Mkdir(filepath.Join(root, ".agent-layer", "tmp", "runs", fmt.Sprintf("old-%02d", i)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	original := readDirFunc
	t.Cleanup(func() { readDirFunc = original })
	runsDir := filepath.Join(canonicalRoot, ".agent-layer", "tmp", "runs")
	readDirFunc = func(path string) ([]os.DirEntry, error) {
		if path == runsDir || strings.HasPrefix(path, runsDir+string(os.PathSeparator)) {
			t.Fatalf("indexed live lookup scanned historical runs: %s", path)
		}
		return original(path)
	}
	records, err := liveCodexLaunchRecords(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].PaneID != "w1:p1" {
		t.Fatalf("indexed records = %#v", records)
	}
}

func TestLiveCodexLaunchRecordsPrunesDeadIndexEntries(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "dead")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	dead := launchContext{Version: 2, PID: 999999, ProcessStart: "dead-process", ProjectRoot: canonicalRoot, Provider: providerCodex, SocketPath: "/tmp/herdr.sock", PaneID: "w1:p1", CodexHome: filepath.Join(root, ".codex")}
	path := filepath.Join(runDir, launchContextName(dead.PID, dead.ProcessStart))
	writeLaunchContextFixture(t, path, dead)
	if err := rememberCodexLiveLaunch(canonicalRoot, path); err != nil {
		t.Fatal(err)
	}
	records, err := liveCodexLaunchRecords(root)
	if err != nil || len(records) != 0 {
		t.Fatalf("dead indexed records = %#v, %v", records, err)
	}
	index := filepath.Join(canonicalRoot, ".agent-layer", "tmp", codexLiveLaunchIndexDir, launchContextName(dead.PID, dead.ProcessStart))
	if _, err := os.Lstat(index); !os.IsNotExist(err) {
		t.Fatalf("dead index entry was retained: %v", err)
	}
}

func TestRememberCodexLiveLaunchWritesIndexAtomically(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	_, start, err := processLineage(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "live")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(runDir, launchContextName(os.Getpid(), start))
	writeLaunchContextFixture(t, recordPath, launchContext{Version: 2, PID: os.Getpid(), ProcessStart: start, ProjectRoot: canonicalRoot, Provider: providerCodex, SocketPath: "/tmp/herdr.sock", PaneID: "w1:p1", CodexHome: filepath.Join(root, ".codex")})
	if err := rememberCodexLiveLaunch(canonicalRoot, recordPath); err != nil {
		t.Fatal(err)
	}
	indexDir := filepath.Join(canonicalRoot, ".agent-layer", "tmp", codexLiveLaunchIndexDir)
	entries, err := os.ReadDir(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	wantName := filepath.Base(recordPath)
	if len(entries) != 1 || entries[0].Name() != wantName {
		got := make([]string, len(entries))
		for i, entry := range entries {
			got[i] = entry.Name()
		}
		t.Fatalf("index directory after atomic write = %v, want only %s", got, wantName)
	}
	// #nosec G304 -- test file path constructed within test fixture root
	data, err := os.ReadFile(filepath.Join(indexDir, wantName))
	if err != nil {
		t.Fatal(err)
	}
	canonicalRecord, err := filepath.EvalSymlinks(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(canonicalRoot, canonicalRecord)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != relative+"\n" {
		t.Fatalf("index contents = %q, want %q", data, relative+"\n")
	}
}

func TestLiveCodexLaunchRecordsIgnoresStrayIndexFiles(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	_, start, err := processLineage(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "live")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	live := launchContext{Version: 2, PID: os.Getpid(), ProcessStart: start, ProjectRoot: canonicalRoot, Provider: providerCodex, SocketPath: "/tmp/herdr.sock", PaneID: "w1:p1", CodexHome: filepath.Join(root, ".codex")}
	recordPath := filepath.Join(runDir, launchContextName(os.Getpid(), start))
	writeLaunchContextFixture(t, recordPath, live)
	if err := rememberCodexLiveLaunch(canonicalRoot, recordPath); err != nil {
		t.Fatal(err)
	}
	indexDir := filepath.Join(canonicalRoot, ".agent-layer", "tmp", codexLiveLaunchIndexDir)
	for _, name := range []string{".DS_Store", launchContextName(os.Getpid(), start) + ".tmp-leftover", "readme.txt"} {
		if err := os.WriteFile(filepath.Join(indexDir, name), []byte("stray\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	records, err := liveCodexLaunchRecords(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].PaneID != "w1:p1" {
		t.Fatalf("indexed records with stray files = %#v", records)
	}
}

func TestLiveCodexLaunchRecordsRejectsIndexNameMismatch(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	_, start, err := processLineage(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "live")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	live := launchContext{Version: 2, PID: os.Getpid(), ProcessStart: start, ProjectRoot: canonicalRoot, Provider: providerCodex, SocketPath: "/tmp/herdr.sock", PaneID: "w1:p1", CodexHome: filepath.Join(root, ".codex")}
	recordPath := filepath.Join(runDir, launchContextName(os.Getpid(), start))
	writeLaunchContextFixture(t, recordPath, live)
	if err := rememberCodexLiveLaunch(canonicalRoot, recordPath); err != nil {
		t.Fatal(err)
	}
	indexDir := filepath.Join(canonicalRoot, ".agent-layer", "tmp", codexLiveLaunchIndexDir)
	matchedName := filepath.Base(recordPath)
	mismatchedName := launchContextName(os.Getpid(), start+"-other")
	if err := os.Rename(filepath.Join(indexDir, matchedName), filepath.Join(indexDir, mismatchedName)); err != nil {
		t.Fatal(err)
	}
	records, err := liveCodexLaunchRecords(root)
	if err == nil {
		t.Fatalf("mismatched index name succeeded: %#v", records)
	}
	if !strings.Contains(err.Error(), "name mismatch") {
		t.Fatalf("mismatch error = %v, want name mismatch", err)
	}
}

func TestLiveCodexLaunchRecordsFallsBackWhenIndexIsAbsent(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	_, start, err := processLineage(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "fallback")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	context := launchContext{Version: 2, PID: os.Getpid(), ProcessStart: start, ProjectRoot: canonicalRoot, Provider: providerCodex, SocketPath: "/tmp/herdr.sock", PaneID: "w1:p1", CodexHome: filepath.Join(root, ".codex")}
	writeLaunchContextFixture(t, filepath.Join(runDir, launchContextName(os.Getpid(), start)), context)
	records, err := liveCodexLaunchRecords(root)
	if err != nil || len(records) != 1 || records[0].PaneID != "w1:p1" {
		t.Fatalf("fallback records = %#v, %v", records, err)
	}
}

func writeLaunchContextFixture(t *testing.T, path string, context launchContext) {
	t.Helper()
	data, err := json.Marshal(context)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
