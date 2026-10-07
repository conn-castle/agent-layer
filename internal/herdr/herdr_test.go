package herdr

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHandleStoresExactResumeForEachProvider(t *testing.T) {
	cases := []struct {
		provider, payload, source string
		env, want                 []string
	}{
		{"claude", `{"hook_event_name":"SessionStart","session_id":"claude-id"}`, "al:claude", nil, []string{"al", "claude", "--resume", "claude-id"}},
		{"codex", `{"hook_event_name":"SessionStart","session_id":"01a1138f-7df0-7fa0-94ae-820334783a29"}`, "al:codex", nil, []string{"al", "codex", "resume", "01a1138f-7df0-7fa0-94ae-820334783a29"}},
		// The actual Antigravity PreInvocation payload supplies conversationId;
		// it has no synthetic hook-event discriminator.
		{"agy", `{"conversationId":"agy-id"}`, "al:agy", nil, []string{"al", "agy", "--conversation", "agy-id"}},
		{"muse", `{"hook_event_name":"SessionStart","session_id":"muse-id"}`, sourceMuseHerdR, nil, []string{"al", "muse", "resume", "muse-id"}},
		{"grok", `{"hook_event_name":"SessionStart"}`, "al:grok", []string{"GROK_SESSION_ID=grok-id"}, []string{"al", "grok", "--resume", "grok-id"}},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			socket, reports, done := fakeHerdR(t, fakeHerdROptions{})
			env := append([]string{EnvEnabled + "=1", EnvSocketPath + "=" + socket, EnvPaneID + "=w1:p1"}, tc.env...)
			if err := Handle(tc.provider, bytes.NewBufferString(tc.payload), &bytes.Buffer{}, &bytes.Buffer{}, env); err != nil {
				t.Fatal(err)
			}
			waitFakeHerdR(t, done)
			params := (<-reports)["params"].(map[string]any)
			if params["source"] != tc.source || !equalStrings(interfaceStrings(params["resume_argv"]), tc.want) {
				t.Fatalf("report = %#v, want source %q argv %#v", params, tc.source, tc.want)
			}
		})
	}
}

func TestCodexFirstPromptAcceptsPayloadThreadIDWithoutInheritedThread(t *testing.T) {
	spec := providers[providerCodex]
	id, ok, err := sessionID(spec, map[string]any{"hook_event_name": eventUserPromptSubmit, "session_id": "01a1138f-7df0-7fa0-94ae-820334783a29"}, map[string]string{"CODEX_THREAD_ID": "stale-thread"})
	if err != nil || !ok || id != "01a1138f-7df0-7fa0-94ae-820334783a29" {
		t.Fatalf("Codex first-prompt identity = %q, %t, %v", id, ok, err)
	}
}

func TestCodexRejectsMalformedPayloadThreadID(t *testing.T) {
	_, ok, err := sessionID(providers[providerCodex], map[string]any{"hook_event_name": eventSessionStart, "session_id": "copied-short-id"}, nil)
	if err == nil || ok {
		t.Fatalf("malformed Codex session ID was accepted: ok=%t err=%v", ok, err)
	}
}

func TestCodexRecoveryDedupUsesResolvedSocketPath(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	firstCWD := filepath.Join(cwd, "one")
	secondCWD := filepath.Join(cwd, "two")
	for _, directory := range []string{firstCWD, secondCWD} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	first := launchContext{SocketPath: "session/herdr.sock", LaunchCWD: firstCWD, PaneID: "w1:p1"}
	second := launchContext{SocketPath: "session/herdr.sock", LaunchCWD: secondCWD, PaneID: "w1:p1"}
	_, firstKey, err := codexRecoveryDedupKey(first)
	if err != nil {
		t.Fatal(err)
	}
	_, secondKey, err := codexRecoveryDedupKey(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstKey == secondKey {
		t.Fatalf("relative sockets in different launch directories collapsed to %q", firstKey)
	}
	_, again, err := codexRecoveryDedupKey(first)
	if err != nil {
		t.Fatal(err)
	}
	if again != firstKey {
		t.Fatalf("identical resolved launches were not stable: %q vs %q", firstKey, again)
	}
}

func TestCodexForegroundProcessUsesLaunchIdentityNotThreadArgv(t *testing.T) {
	if !codexForegroundProcess(map[string]any{"name": "codex", "argv": []any{"codex", "resume"}}) {
		t.Fatal("native Codex process was rejected when its argv no longer names the selected thread")
	}
	if !codexForegroundProcess(map[string]any{"name": "node", "argv": []any{"node", "/opt/homebrew/bin/codex", "resume"}}) {
		t.Fatal("Codex Node wrapper was rejected")
	}
	if codexForegroundProcess(map[string]any{"name": "node", "argv": []any{"node", "worker.js"}}) {
		t.Fatal("unrelated Node process was accepted as Codex")
	}
}

func TestSessionPathResolvesRelativeSocketAliasToPhysicalHerdRState(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(t.TempDir(), "herdr.sock")
	if err := os.WriteFile(physical, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	aliasDir := filepath.Join(root, ".agent-layer", "tmp", "hr")
	if err := os.MkdirAll(aliasDir, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(aliasDir, "s")
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	got, err := sessionPath(filepath.Join(".agent-layer", "tmp", "hr", "s"))
	if err != nil {
		t.Fatal(err)
	}
	canonicalPhysical, err := filepath.EvalSymlinks(physical)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(filepath.Dir(canonicalPhysical), "session.json")
	if got != want {
		t.Fatalf("session path = %q, want %q", got, want)
	}
}

func TestHandleUsesCanonicalPaneAndPersistedSessionMapping(t *testing.T) {
	// The socket is reached via the project's s symlink. pane.get resolves an
	// alias to w2:p3, which session.json maps to internal pane key 7.
	socket, reports, done := fakeHerdR(t, fakeHerdROptions{canonicalPane: "w2:p3", workspace: "w2", publicNumber: 3, internalPane: "7"})
	env := []string{EnvEnabled + "=1", EnvSocketPath + "=" + socket, EnvPaneID + "=moved-pane-alias"}
	if err := Handle("claude", bytes.NewBufferString(`{"hook_event_name":"SessionStart","session_id":"id"}`), &bytes.Buffer{}, &bytes.Buffer{}, env); err != nil {
		t.Fatal(err)
	}
	waitFakeHerdR(t, done)
	if len(reports) != 1 {
		t.Fatalf("reports = %d, want 1", len(reports))
	}
}

func TestHandleRejectsOkWithoutCurrentPaneCommand(t *testing.T) {
	// A matching command stored at public pane 2 must not acknowledge pane 1.
	socket, reports, done := fakeHerdR(t, fakeHerdROptions{storedPublicNumber: 2, reportsExpected: reportAttempts, paneGetsExpected: 1})
	err := Handle("claude", bytes.NewBufferString(`{"hook_event_name":"SessionStart","session_id":"id"}`), &bytes.Buffer{}, &bytes.Buffer{}, []string{EnvEnabled + "=1", EnvSocketPath + "=" + socket, EnvPaneID + "=w1:p1"})
	if err == nil {
		t.Fatal("Handle succeeded with a matching command in another pane")
	}
	waitFakeHerdR(t, done)
	if len(reports) != reportAttempts {
		t.Fatalf("reports = %d, want %d", len(reports), reportAttempts)
	}
}

func TestHandleRetriesOkUntilPersistedCommandAppears(t *testing.T) {
	socket, reports, done := fakeHerdR(t, fakeHerdROptions{staleFirst: true, reportsExpected: 2, paneGetsExpected: 1})
	if err := Handle("claude", bytes.NewBufferString(`{"hook_event_name":"SessionStart","session_id":"id"}`), &bytes.Buffer{}, &bytes.Buffer{}, []string{EnvEnabled + "=1", EnvSocketPath + "=" + socket, EnvPaneID + "=w1:p1"}); err != nil {
		t.Fatal(err)
	}
	waitFakeHerdR(t, done)
	if len(reports) != 2 {
		t.Fatalf("reports = %d, want 2", len(reports))
	}
}

func TestHandleAwaitsFiveSecondDebouncedPersistenceWithoutNewSequence(t *testing.T) {
	socket, reports, done := fakeHerdR(t, fakeHerdROptions{persistDelay: 5 * time.Second, paneGetsExpected: 1})
	if err := Handle("claude", bytes.NewBufferString(`{"hook_event_name":"SessionStart","session_id":"id"}`), &bytes.Buffer{}, &bytes.Buffer{}, []string{EnvEnabled + "=1", EnvSocketPath + "=" + socket, EnvPaneID + "=w1:p1"}); err != nil {
		t.Fatal(err)
	}
	waitFakeHerdR(t, done)
	if len(reports) != 1 {
		t.Fatalf("reports = %d, want one accepted report while persistence was pending", len(reports))
	}
}

func TestMuseRetriesAfterStaleSharedSourceReportWithNewSequence(t *testing.T) {
	// This models a report accepted at the RPC boundary but not applied to the
	// persisted resume state (including a stale shared-source sequence). It
	// intentionally does not claim anything about the ordering of Muse's
	// separate native lifecycle events.
	socket, reports, done := fakeHerdR(t, fakeHerdROptions{staleFirst: true, reportsExpected: 2, paneGetsExpected: 1})
	err := Handle("muse", bytes.NewBufferString(`{"hook_event_name":"SessionStart","session_id":"id"}`), &bytes.Buffer{}, &bytes.Buffer{}, []string{EnvEnabled + "=1", EnvSocketPath + "=" + socket, EnvPaneID + "=w1:p1"})
	if err != nil {
		t.Fatal(err)
	}
	waitFakeHerdR(t, done)
	first := (<-reports)["params"].(map[string]any)["seq"].(float64)
	second := (<-reports)["params"].(map[string]any)["seq"].(float64)
	if second <= first {
		t.Fatalf("Muse retry sequence = %.0f after %.0f, want a new ordered sequence", second, first)
	}
}

func TestMuseFirstPromptRootHookStoresRetainedSessionWithDevelopmentCommand(t *testing.T) {
	// A retained native Muse conversation opened in a new pane emits
	// UserPromptSubmit, not SessionStart. Exercise the root-bound generated
	// hook path with a stale shared-source acknowledgement before persistence.
	socket, reports, done := fakeHerdR(t, fakeHerdROptions{staleFirst: true, reportsExpected: 2, paneGetsExpected: 1})
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "muse-first-prompt")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	developmentExecutable := filepath.Join(root, "candidate", "al")
	if err := os.MkdirAll(filepath.Dir(developmentExecutable), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(developmentExecutable, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{
		EnvEnabled + "=1",
		EnvSocketPath + "=" + socket,
		EnvPaneID + "=w1:p1",
		EnvDevBypass + "=1",
		EnvDevExecutable + "=" + developmentExecutable,
	}
	if err := CaptureLaunch(root, runDir, env, providerMuse); err != nil {
		t.Fatal(err)
	}
	_, start, err := processLineage(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"hook_event_name":"UserPromptSubmit","session_id":"retained-muse-id","cwd":"` + root + `","model":"fixture-model","permission_mode":"default"}`
	if err := HandleForRoot(providerMuse, root, strings.NewReader(payload), io.Discard, io.Discard, env); err != nil {
		t.Fatal(err)
	}
	waitFakeHerdR(t, done)
	first := (<-reports)["params"].(map[string]any)
	second := (<-reports)["params"].(map[string]any)
	if first["agent_session_id"] != "retained-muse-id" || second["agent_session_id"] != "retained-muse-id" {
		t.Fatalf("retained Muse IDs = %#v then %#v", first["agent_session_id"], second["agent_session_id"])
	}
	if second["seq"].(float64) <= first["seq"].(float64) {
		t.Fatalf("Muse retry sequence = %#v after %#v, want newer sequence", second["seq"], first["seq"])
	}
	argv := interfaceStrings(second["resume_argv"])
	canonicalDev, err := filepath.EvalSymlinks(developmentExecutable)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(argv, []string{commandEnv, envRecoveryGeneration + "=" + launchGeneration(os.Getpid(), start), EnvDevBypass + "=1", EnvDevExecutable + "=" + canonicalDev, canonicalDev, providerMuse, resumeVerb, "retained-muse-id"}) {
		t.Fatalf("retained Muse development argv = %#v", argv)
	}
}

func TestMuseFirstPromptSkipsReportWhenExactCommandIsAlreadyDurable(t *testing.T) {
	socket, reports, done := fakeHerdR(t, fakeHerdROptions{requestsExpected: 1, paneGetsExpected: 1})
	path, err := sessionPath(socket)
	if err != nil {
		t.Fatal(err)
	}
	argv := []any{"al", providerMuse, resumeVerb, "retained-muse-id"}
	writePersistedSession(t, filepath.Dir(path), fakeHerdROptions{workspace: "w1", publicNumber: 1, internalPane: "1", storedPublicNumber: 1}, map[string]any{
		"source":      sourceMuseHerdR,
		"agent":       providerMuse,
		"resume_argv": argv,
	})
	env := []string{EnvEnabled + "=1", EnvSocketPath + "=" + socket, EnvPaneID + "=w1:p1"}
	payload := `{"hook_event_name":"UserPromptSubmit","session_id":"retained-muse-id","cwd":"/project","model":"fixture-model","permission_mode":"default"}`
	if err := Handle(providerMuse, strings.NewReader(payload), io.Discard, io.Discard, env); err != nil {
		t.Fatal(err)
	}
	waitFakeHerdR(t, done)
	if len(reports) != 0 {
		t.Fatalf("durable Muse prompt sent a duplicate report: %#v", <-reports)
	}
}

func TestHandleRetriesExplicitlyUnappliedReport(t *testing.T) {
	socket, reports, done := fakeHerdR(t, fakeHerdROptions{unappliedFirst: true, reportsExpected: 2, paneGetsExpected: 1})
	env := []string{EnvEnabled + "=1", EnvSocketPath + "=" + socket, EnvPaneID + "=w1:p1"}
	if err := Handle(providerMuse, strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"id"}`), io.Discard, io.Discard, env); err != nil {
		t.Fatal(err)
	}
	waitFakeHerdR(t, done)
	first := (<-reports)["params"].(map[string]any)["seq"].(float64)
	second := (<-reports)["params"].(map[string]any)["seq"].(float64)
	if second <= first {
		t.Fatal("unapplied report was not retried with a newer sequence")
	}
}

func TestMuseCurrentLaunchDoesNotTrustIdenticalResumeFromEarlierLaunch(t *testing.T) {
	// An automatic cold restore can retain the native ID and old command. Its
	// new captured launch identity must still produce a different exact argv,
	// then later prompts from that same launch can use the ordinary fast path.
	socket, reports, done := fakeHerdR(t, fakeHerdROptions{staleFirst: true, reportsExpected: 2, paneGetsExpected: 2, requestsExpected: 4})
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "reopened")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CaptureLaunch(root, runDir, []string{EnvEnabled + "=1", EnvSocketPath + "=" + socket, EnvPaneID + "=w1:p1"}, providerMuse); err != nil {
		t.Fatal(err)
	}
	_, start, err := processLineage(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	path, err := sessionPath(socket)
	if err != nil {
		t.Fatal(err)
	}
	previousGeneration := launchGeneration(os.Getpid(), "previous-native-exec")
	params := map[string]any{"source": sourceMuseHerdR, "agent": providerMuse, "resume_argv": []any{commandEnv, envRecoveryGeneration + "=" + previousGeneration, "al", providerMuse, resumeVerb, "retained-muse-id"}}
	writePersistedSession(t, filepath.Dir(path), fakeHerdROptions{workspace: "w1", publicNumber: 1, internalPane: "1"}, params)
	payload := `{"hook_event_name":"UserPromptSubmit","session_id":"retained-muse-id"}`
	if err := HandleForRoot(providerMuse, root, strings.NewReader(payload), io.Discard, io.Discard, []string{envRecoveryGeneration + "=spoofed-inbound-value"}); err != nil {
		t.Fatal(err)
	}
	if err := HandleForRoot(providerMuse, root, strings.NewReader(payload), io.Discard, io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	waitFakeHerdR(t, done)
	if len(reports) != 2 {
		t.Fatalf("reports = %d, want stale rejection then fresh current-launch registration", len(reports))
	}
	want := []string{commandEnv, envRecoveryGeneration + "=" + launchGeneration(os.Getpid(), start), "al", providerMuse, resumeVerb, "retained-muse-id"}
	var previousSequence float64
	for index := range 2 {
		params := (<-reports)["params"].(map[string]any)
		sequence := params["seq"].(float64)
		if index > 0 && sequence <= previousSequence {
			t.Fatalf("stale current-generation retry sequence = %.0f after %.0f", sequence, previousSequence)
		}
		previousSequence = sequence
		argv := interfaceStrings(params[requestResumeArgvKey])
		if !equalStrings(argv, want) {
			t.Fatalf("current launch resume argv = %#v, want %#v", argv, want)
		}
	}
}

func TestMuseCurrentLaunchRejectsMalformedPersistenceBeforeReporting(t *testing.T) {
	socket, reports, done := fakeHerdR(t, fakeHerdROptions{requestsExpected: 1})
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "malformed")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CaptureLaunch(root, runDir, []string{EnvEnabled + "=1", EnvSocketPath + "=" + socket, EnvPaneID + "=w1:p1"}, providerMuse); err != nil {
		t.Fatal(err)
	}
	path, err := sessionPath(socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"workspaces":`), 0o600); err != nil {
		t.Fatal(err)
	}
	err = HandleForRoot(providerMuse, root, strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"retained-muse-id"}`), io.Discard, io.Discard, nil)
	if err == nil || !strings.Contains(err.Error(), "decode HerdR persisted session state") {
		t.Fatalf("malformed current-launch persistence error = %v", err)
	}
	waitFakeHerdR(t, done)
	if len(reports) != 0 {
		t.Fatal("malformed persistence sent a session report")
	}
}

func TestMuseFirstPromptRejectsMalformedPersistedSessionWithoutReporting(t *testing.T) {
	socket, reports, done := fakeHerdR(t, fakeHerdROptions{requestsExpected: 1, paneGetsExpected: 1})
	path, err := sessionPath(socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"workspaces":`), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{EnvEnabled + "=1", EnvSocketPath + "=" + socket, EnvPaneID + "=w1:p1"}
	err = Handle(providerMuse, strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"retained-muse-id"}`), io.Discard, io.Discard, env)
	if err == nil || !strings.Contains(err.Error(), "stored resume command") {
		t.Fatalf("malformed persisted state error = %v", err)
	}
	waitFakeHerdR(t, done)
	if len(reports) != 0 {
		t.Fatal("malformed persistence sent a session report")
	}
}

func TestHandleDoesNotReportOutsideGenuineMainHerdRSession(t *testing.T) {
	for _, tc := range []struct{ name, env, payload string }{
		{"not herdr", "", `{"hook_event_name":"SessionStart","session_id":"id"}`},
		{"dispatched", EnvEnabled + "=1\n" + EnvSocketPath + "=/missing\n" + EnvPaneID + "=w1:p1\n" + EnvDispatch + "=1", `{"hook_event_name":"SessionStart","session_id":"id"}`},
		{"subagent", EnvEnabled + "=1\n" + EnvSocketPath + "=/missing\n" + EnvPaneID + "=w1:p1", `{"hook_event_name":"SessionStart","session_id":"id","agent_id":"child"}`},
		{"wrong event", EnvEnabled + "=1\n" + EnvSocketPath + "=/missing\n" + EnvPaneID + "=w1:p1", `{"hook_event_name":"Stop","session_id":"id"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var env []string
			for _, entry := range bytes.Split([]byte(tc.env), []byte{'\n'}) {
				if len(entry) > 0 {
					env = append(env, string(entry))
				}
			}
			if err := Handle("claude", bytes.NewBufferString(tc.payload), &bytes.Buffer{}, &bytes.Buffer{}, env); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHandleDiagnosesMissingSessionPayload(t *testing.T) {
	for _, tc := range []struct{ provider, payload string }{{"claude", `{"hook_event_name":"SessionStart"}`}, {"claude-empty", `{}`}, {"agy", `{}`}, {"grok", `{"hook_event_name":"SessionStart"}`}} {
		t.Run(tc.provider, func(t *testing.T) {
			provider := tc.provider
			if provider == "claude-empty" {
				provider = "claude"
			}
			err := Handle(provider, bytes.NewBufferString(tc.payload), &bytes.Buffer{}, &bytes.Buffer{}, []string{EnvEnabled + "=1", EnvSocketPath + "=/missing", EnvPaneID + "=w1:p1"})
			if err == nil {
				t.Fatal("missing session payload was silently accepted")
			}
		})
	}
}

func TestResumeArgvCarriesOnlyDevelopmentExecutable(t *testing.T) {
	root := t.TempDir()
	env := map[string]string{EnvDevExecutable: filepath.Join(root, "candidate", "al"), EnvDevBypass: "1", "HOME": filepath.Join(root, "home"), "PATH": filepath.Join(root, "bin"), "SECRET": "must-not-appear"}
	argv, err := resumeArgv(providers["muse"], "id", env)
	if err != nil {
		t.Fatal(err)
	}
	if argv[0] != "env" || contains(argv, "HOME="+filepath.Join(root, "home")) || argv[len(argv)-2] != "resume" || argv[len(argv)-1] != "id" {
		t.Fatalf("argv = %#v", argv)
	}
	if contains(argv, "SECRET=must-not-appear") {
		t.Fatalf("secret persisted: %#v", argv)
	}
}

type fakeHerdROptions struct {
	canonicalPane, workspace, internalPane                              string
	publicNumber, storedPublicNumber, reportsExpected, paneGetsExpected int
	requestsExpected                                                    int
	staleFirst, distinctPanes, unappliedFirst                           bool
	persistDelay                                                        time.Duration
	switchCodexTitleAfterReport, switchCodexTitleAfterPaneList          string
	codexTitle                                                          string
	codexProcessPID                                                     int
	reportError                                                         string
	codexPanes                                                          []fakeCodexPane
	codexProcessError                                                   string
	workingDir                                                          string
	socketPath                                                          string
}

// fakeCodexPane is intentionally limited to the fields read through the
// public HerdR protocol by the native-Codex hook boundary tests.
type fakeCodexPane struct {
	paneID, title, authority                   string
	liveSource, liveAgent, liveKind, liveValue string
	processName                                string
	processPID                                 int
	processArgv                                []any
}

func fakeHerdR(t *testing.T, options fakeHerdROptions) (string, chan map[string]any, chan struct{}) {
	t.Helper()
	if options.canonicalPane == "" {
		options.canonicalPane = "w1:p1"
	}
	if options.workspace == "" {
		options.workspace = "w1"
	}
	if options.publicNumber == 0 {
		options.publicNumber = 1
	}
	if options.internalPane == "" {
		options.internalPane = "1"
	}
	if options.storedPublicNumber == 0 {
		options.storedPublicNumber = options.publicNumber
	}
	if options.reportsExpected == 0 {
		options.reportsExpected = 1
	}
	if options.paneGetsExpected == 0 {
		options.paneGetsExpected = options.reportsExpected
	}
	// Bind from a test-owned temporary working directory so the relative socket
	// stays under Unix-domain limits without retaining test artifacts.
	root := options.workingDir
	if root == "" {
		root = t.TempDir()
	}
	t.Chdir(root)
	sessionDir := filepath.Join(root, "session")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	projectDir := "project"
	if err := os.Mkdir(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sessionDir, filepath.Join(projectDir, "s")); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join("session", "herdr.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	reports := make(chan map[string]any, options.reportsExpected)
	done := make(chan struct{})
	go func() {
		defer close(done)
		reportCount, paneGets, requests := 0, 0, 0
		lastSequence := map[string]float64{}
		requestsExpected := options.requestsExpected
		if requestsExpected == 0 {
			requestsExpected = options.reportsExpected + options.paneGetsExpected
		}
		for requests < requestsExpected {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			var request map[string]any
			decodeErr := json.NewDecoder(bufio.NewReader(conn)).Decode(&request)
			requests++
			switch request["method"] {
			case "pane.report_agent_session":
				params := request["params"].(map[string]any)
				invalid := false
				for _, arg := range interfaceStrings(params["resume_argv"]) {
					if strings.Contains(arg, "'") {
						invalid = true
					}
				}
				if invalid {
					_, _ = conn.Write([]byte("{\"error\":{\"code\":\"invalid_resume_argv\",\"message\":\"resume_argv must not contain apostrophes\"}}\n"))
					_ = conn.Close()
					continue
				}
				if options.reportError != "" {
					_, _ = conn.Write([]byte(`{"error":{"code":"` + options.reportError + `"}}` + "\n"))
					_ = conn.Close()
					continue
				}
				reportCount++
				reports <- request
				for index := range options.codexPanes {
					if options.codexPanes[index].paneID == stringValue(params[requestPaneIDKey]) {
						options.codexPanes[index].liveSource = stringValue(params[requestSourceKey])
						options.codexPanes[index].liveAgent = stringValue(params[requestAgentKey])
						options.codexPanes[index].liveKind = "id"
						options.codexPanes[index].liveValue = stringValue(params["agent_session_id"])
					}
				}
				if reportCount == 1 && options.switchCodexTitleAfterReport != "" && len(options.codexPanes) > 0 {
					options.codexPanes[0].title = options.switchCodexTitleAfterReport
				}
				source := params["source"].(string)
				sequence := params["seq"].(float64)
				if sequence <= lastSequence[source] {
					_, _ = conn.Write([]byte("{\"result\":{\"type\":\"ok\"}}\n"))
					_ = conn.Close()
					continue
				}
				lastSequence[source] = sequence
				if options.persistDelay > 0 {
					params := request["params"].(map[string]any)
					time.AfterFunc(options.persistDelay, func() { writePersistedSession(t, sessionDir, options, params) })
				} else if !options.staleFirst || reportCount > 1 {
					writePersistedSession(t, sessionDir, options, request["params"].(map[string]any))
				}
				if options.unappliedFirst && reportCount == 1 {
					_, _ = conn.Write([]byte("{\"result\":{\"type\":\"ok\",\"applied\":false}}\n"))
				} else {
					_, _ = conn.Write([]byte("{\"id\":\"ok\",\"result\":{\"type\":\"ok\"}}\n"))
				}
			case "pane.get":
				paneGets++
				canonicalPane := options.canonicalPane
				if options.distinctPanes {
					canonicalPane = request["params"].(map[string]any)["pane_id"].(string)
				}
				response, _ := json.Marshal(map[string]any{"id": "canonical", "result": map[string]any{"type": "pane_info", "pane": map[string]any{"pane_id": canonicalPane}}})
				_, _ = conn.Write(append(response, '\n'))
			case "pane.list":
				panes := make([]any, 0, len(options.codexPanes))
				if len(options.codexPanes) == 0 {
					panes = append(panes, map[string]any{"pane_id": options.canonicalPane, "terminal_title": options.codexTitle})
				} else {
					for _, fixture := range options.codexPanes {
						pane := map[string]any{"pane_id": fixture.paneID, "terminal_title": fixture.title}
						if fixture.authority != "" {
							pane["agent_session"] = map[string]any{"source": fixture.authority}
						} else if fixture.liveSource != "" {
							pane["agent_session"] = map[string]any{"source": fixture.liveSource, "agent": fixture.liveAgent, "kind": fixture.liveKind, "value": fixture.liveValue}
						}
						panes = append(panes, pane)
					}
				}
				response, _ := json.Marshal(map[string]any{"id": "panes", "result": map[string]any{"panes": panes}})
				_, _ = conn.Write(append(response, '\n'))
				if options.switchCodexTitleAfterPaneList != "" && len(options.codexPanes) > 0 {
					options.codexPanes[0].title = options.switchCodexTitleAfterPaneList
					options.switchCodexTitleAfterPaneList = ""
				}
			case "pane.process_info":
				if options.codexProcessError != "" {
					response, _ := json.Marshal(map[string]any{"id": "process", "error": map[string]any{"code": options.codexProcessError}})
					_, _ = conn.Write(append(response, '\n'))
					break
				}
				pid, argv, name := options.codexProcessPID, []any{"codex"}, "codex"
				if len(options.codexPanes) > 0 {
					paneID := request["params"].(map[string]any)["pane_id"].(string)
					for _, fixture := range options.codexPanes {
						if fixture.paneID == paneID {
							pid = fixture.processPID
							if fixture.processName != "" {
								name = fixture.processName
							}
							if len(fixture.processArgv) > 0 {
								argv = fixture.processArgv
							}
							break
						}
					}
				}
				response, _ := json.Marshal(map[string]any{"id": "process", "result": map[string]any{"process_info": map[string]any{"foreground_processes": []any{map[string]any{"pid": pid, "name": name, "argv": argv}}}}})
				_, _ = conn.Write(append(response, '\n'))
			default:
				// session.snapshot is intentionally absent: its live 0.9.3
				// schema does not expose persisted agent_resume commands.
				if decodeErr == nil {
					_, _ = conn.Write([]byte("{\"id\":\"error\",\"error\":{\"code\":\"unexpected_method\"}}\n"))
				}
			}
			_ = conn.Close()
		}
	}()
	if options.socketPath != "" {
		return options.socketPath, reports, done
	}
	return filepath.Join(projectDir, "s", "herdr.sock"), reports, done
}

func writePersistedSession(t *testing.T, dir string, options fakeHerdROptions, params map[string]any) {
	t.Helper()
	storedInternal := options.internalPane
	if options.storedPublicNumber != options.publicNumber {
		storedInternal = "99"
	}
	document := map[string]any{
		"version": 3,
		"workspaces": []any{map[string]any{
			"id":                  options.workspace,
			"public_pane_numbers": map[string]any{options.internalPane: options.publicNumber, storedInternal: options.storedPublicNumber},
			"tabs": []any{map[string]any{
				"panes": map[string]any{
					storedInternal: map[string]any{
						"cwd":          "/fixture",
						"agent_resume": map[string]any{"source": params["source"], "agent": params["agent"], "argv": params["resume_argv"]},
					},
				},
			}},
		}},
	}
	if options.distinctPanes {
		paneID := params["pane_id"].(string)
		internal := strings.TrimPrefix(paneID, "w1:p")
		publicNumber, err := strconv.Atoi(internal)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "session.json")
		if previous, err := os.ReadFile(path); err == nil { // #nosec G304 -- path is inside this test-owned fake HerdR session directory.
			if err := json.Unmarshal(previous, &document); err != nil {
				t.Fatal(err)
			}
		}
		workspace := document["workspaces"].([]any)[0].(map[string]any)
		workspace["public_pane_numbers"].(map[string]any)[internal] = publicNumber
		panes := workspace["tabs"].([]any)[0].(map[string]any)["panes"].(map[string]any)
		panes[internal] = map[string]any{"cwd": "/fixture", "agent_resume": map[string]any{"source": params["source"], "agent": params["agent"], "argv": params["resume_argv"]}}
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func interfaceStrings(value any) []string {
	values, _ := value.([]any)
	result := make([]string, len(values))
	for i, value := range values {
		result[i], _ = value.(string)
	}
	return result
}
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestReleaseLaunchRecordIgnoresAmbientDevelopmentHookEnvironment(t *testing.T) {
	socket, reports, done := fakeHerdR(t, fakeHerdROptions{})
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "release")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	env := []string{EnvEnabled + "=1", EnvSocketPath + "=" + socket, EnvPaneID + "=w1:p1"}
	if err := CaptureLaunch(root, runDir, env, providerMuse); err != nil {
		t.Fatal(err)
	}
	_, start, err := processLineage(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	env = append(env, EnvDevBypass+"=1", EnvDevExecutable+"=/incorrect-development-binary", envRecoveryGeneration+"=spoofed-inbound-value")
	if err := HandleForRoot("muse", root, strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"release-id"}`), io.Discard, io.Discard, env); err != nil {
		t.Fatal(err)
	}
	waitFakeHerdR(t, done)
	argv := interfaceStrings((<-reports)["params"].(map[string]any)["resume_argv"])
	want := []string{commandEnv, envRecoveryGeneration + "=" + launchGeneration(os.Getpid(), start), "al", providerMuse, resumeVerb, "release-id"}
	if !equalStrings(argv, want) {
		t.Fatalf("release record inherited ambient identity or generation: %#v", argv)
	}
}

func TestRecoveryVerificationReturnsBeforeItsDeadline(t *testing.T) {
	socket, _, done := fakeHerdR(t, fakeHerdROptions{staleFirst: true})
	start := time.Now()
	err := reportAndVerify(providers["muse"], "stale-id", []string{"al", "muse", "resume", "stale-id"}, map[string]string{EnvSocketPath: socket, EnvPaneID: "w1:p1"}, start.Add(150*time.Millisecond), "", nil)
	waitFakeHerdR(t, done)
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("stale Ok must fail visibly: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("verification exceeded its bounded diagnostic window: %s", elapsed)
	}
}

func waitFakeHerdR(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("fake HerdR did not receive expected public requests; hook may have unexpectedly become a no-op")
	}
}

func TestHandleForRootDoesNotClaimPlainNativePane(t *testing.T) {
	for _, provider := range []string{providerClaude, providerCodex, providerGrok, providerAgy} {
		t.Run(provider, func(t *testing.T) {
			root := t.TempDir()
			env := []string{EnvEnabled + "=1", EnvSocketPath + "=/nonexistent-herdr.sock", EnvPaneID + "=w1:p1"}
			if err := HandleForRoot(provider, root, strings.NewReader(`{}`), io.Discard, io.Discard, env); err != nil {
				t.Fatalf("plain native hook must not report: %v", err)
			}
		})
	}
}

func TestHandleForRootStoresNonMuseLaunchIdentity(t *testing.T) {
	cases := []struct {
		provider, payload string
		want              []string
	}{
		{providerClaude, `{"hook_event_name":"SessionStart","session_id":"native-id"}`, []string{"al", "claude", "--resume", "native-id"}},
		{providerCodex, `{"hook_event_name":"SessionStart","session_id":"01a1138f-7df0-7fa0-94ae-820334783a29"}`, []string{"al", "codex", "resume", "01a1138f-7df0-7fa0-94ae-820334783a29"}},
		{providerGrok, `{"hookEventName":"SessionStart","sessionId":"native-id"}`, []string{"al", "grok", "--resume", "native-id"}},
		{providerAgy, `{"conversationId":"native-id"}`, []string{"al", "agy", "--conversation", "native-id"}},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			options := fakeHerdROptions{paneGetsExpected: 1}
			if tc.provider == providerCodex {
				// Codex recovery requires the projected title and live foreground owner.
				options.requestsExpected = 6
				options.codexPanes = []fakeCodexPane{{
					paneID: "w1:p1", title: "✳ 01a1138f-7df0-7fa0-94ae-820334783a29 | selected",
					processPID: os.Getpid(), processArgv: []any{"codex"},
				}}
			}
			socket, reports, done := fakeHerdR(t, options)
			root, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			runDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "identity")
			if err := os.MkdirAll(runDir, 0o700); err != nil {
				t.Fatal(err)
			}
			env := []string{EnvEnabled + "=1", EnvSocketPath + "=" + socket, EnvPaneID + "=w1:p1"}
			if err := CaptureLaunch(root, runDir, env, tc.provider); err != nil {
				t.Fatal(err)
			}
			hookEnv := env
			if tc.provider == providerAgy {
				hookEnv = nil
			}
			var out bytes.Buffer
			if err := HandleForRoot(tc.provider, root, strings.NewReader(tc.payload), &out, io.Discard, hookEnv); err != nil {
				t.Fatal(err)
			}
			waitFakeHerdR(t, done)
			params := (<-reports)["params"].(map[string]any)
			got := params[requestResumeArgvKey].([]any)
			want := tc.want
			if len(got) != len(want) {
				t.Fatalf("argv = %v, want %v", got, want)
			}
			for i, argument := range want {
				if got[i] != argument {
					t.Fatalf("argv = %v, want %v", got, want)
				}
			}
			if tc.provider == providerAgy && out.String() != "{}\n" {
				t.Fatalf("Agy response = %q", out.String())
			}
		})
	}
}

func TestHandleForRootRejectsDifferentProviderAncestor(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "foreign")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	env := []string{EnvEnabled + "=1", EnvSocketPath + "=/nonexistent-herdr.sock", EnvPaneID + "=w1:p1"}
	if err := CaptureLaunch(root, runDir, env, providerClaude); err != nil {
		t.Fatal(err)
	}
	if err := HandleForRoot(providerMuse, root, strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"child-id"}`), io.Discard, io.Discard, nil); err != nil {
		t.Fatalf("different provider must not claim parent: %v", err)
	}
}
