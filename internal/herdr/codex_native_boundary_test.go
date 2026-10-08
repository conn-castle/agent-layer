package herdr

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const boundaryThread = "01a1138f-7df0-7fa0-94ae-820334783a29"
const boundaryOther = "01a1138f-7df1-7fa0-94ae-820334783a29"

func codexBoundaryHook(t *testing.T, root, payload string) error {
	t.Helper()
	var out, errOut bytes.Buffer
	err := HandleForRoot(providerCodex, root, strings.NewReader(payload), &out, &errOut, []string{
		EnvEnabled + "=1", EnvSocketPath + "=/stale.sock", EnvPaneID + "=stale", EnvDispatch + "=1",
		"CODEX_THREAD_ID=stale", EnvDevBypass + "=1", EnvDevExecutable + "=/stale/al",
	})
	if out.Len() != 0 {
		t.Fatalf("hook stdout = %q", out.String())
	}
	if err != nil {
		if strings.Count(errOut.String(), "agent-layer HerdR recovery (codex):") != 1 {
			t.Fatalf("failure diagnostics = %q", errOut.String())
		}
		receipts, readErr := os.ReadDir(filepath.Join(root, ".agent-layer", "tmp", "herdr-hook-errors"))
		if readErr != nil || len(receipts) != 1 {
			t.Fatalf("failure receipts = %#v / %v", receipts, readErr)
		}
	} else {
		if errOut.Len() != 0 {
			t.Fatalf("unexpected diagnostic = %q", errOut.String())
		}
		if _, err := os.Stat(filepath.Join(root, ".agent-layer", "tmp", "herdr-hook-errors")); !os.IsNotExist(err) {
			t.Fatalf("successful/no-match hook left a receipt directory: %v", err)
		}
	}
	return err
}

func codexBoundaryPayload(id string) string {
	return `{"hook_event_name":"UserPromptSubmit","session_id":"` + id + `"}`
}

func TestHandleForRootCodexTitleOwnership(t *testing.T) {
	prefix := boundaryThread[:29] + "..."
	for _, tc := range []struct {
		name, title string
		match       bool
	}{
		{"native_prefix", prefix + " | workspace", true},
		{"full", boundaryThread, true},
		{"repeated_mixed_case", prefix + " " + strings.ToUpper(prefix), true},
		{"full_then_truncated", strings.ToUpper(boundaryThread) + " " + prefix, true},
		{"truncated_then_full", strings.ToUpper(prefix) + " " + boundaryThread, true},
		{"uuid_thread_name", prefix + " | " + boundaryOther, true},
		{"same_prefix_full_uuid_name", prefix + " | " + boundaryThread[:35] + "8", true},
		{"foreign_first_then_matching", boundaryOther + " " + prefix, false},
		{"foreign_full_shares_prefix", boundaryThread[:35] + "8", false},
		{"foreign_prefix_then_matching", boundaryOther[:29] + "... " + boundaryThread, false},
	} {
		for _, dev := range []bool{false, true} {
			t.Run(tc.name+"_dev_"+strconv.FormatBool(dev), func(t *testing.T) {
				root := t.TempDir()
				tui := startCodexBoundaryTUI(t)
				socket, reports, _ := fakeHerdR(t, fakeHerdROptions{workingDir: root, socketPath: "session/herdr.sock", requestsExpected: 30,
					codexPanes: []fakeCodexPane{{paneID: "w1:p1", title: tc.title, processPID: tui.PID, processArgv: []any{"codex", "resume", boundaryOther}}}})
				executable := ""
				want := []string{"al", providerCodex, resumeVerb, boundaryThread}
				if dev {
					executable = sourceTestExecutable(t)
					want = []string{commandEnv, EnvDevBypass + "=1", EnvDevExecutable + "=" + executable, executable, providerCodex, resumeVerb, boundaryThread}
				}
				writeCodexBoundaryLaunch(t, root, "title", socket, "w1:p1", executable, tui)
				if err := codexBoundaryHook(t, root, codexBoundaryPayload(boundaryThread)); err != nil {
					t.Fatal(err)
				}
				if !tc.match {
					if len(reports) != 0 {
						t.Fatal("foreign first token reported")
					}
					return
				}
				if len(reports) != 1 {
					t.Fatalf("reports = %d", len(reports))
				}
				params := (<-reports)["params"].(map[string]any)
				if params[requestPaneIDKey] != "w1:p1" || !equalStrings(interfaceStrings(params[requestResumeArgvKey]), want) {
					t.Fatalf("recipe = %#v; want %#v", params, want)
				}
				assertBoundaryStored(t, root, "w1:p1", want)
			})
		}
	}
}

func TestHandleForRootCodexMultipleRecordedPanes(t *testing.T) {
	for _, same := range []bool{false, true} {
		t.Run("same_thread_"+strconv.FormatBool(same), func(t *testing.T) {
			root := t.TempDir()
			a, b := startCodexBoundaryTUI(t), startCodexBoundaryTUI(t)
			firstTitle, authority := boundaryOther+" "+boundaryThread, "herdr:codex:foreign"
			if same {
				firstTitle, authority = boundaryThread[:29]+"...", ""
			}
			socket, reports, _ := fakeHerdR(t, fakeHerdROptions{workingDir: root, socketPath: "session/herdr.sock", requestsExpected: 80, reportsExpected: 2, distinctPanes: true,
				codexPanes: []fakeCodexPane{
					{paneID: "w1:p1", title: firstTitle, authority: authority, processPID: a.PID},
					{paneID: "w1:p2", title: boundaryThread[:29] + "...", processPID: b.PID},
				}})
			writeCodexBoundaryLaunch(t, root, "first-launcher", socket, "w1:p1", "", a)
			selected := writeCodexBoundaryLaunch(t, root, "attached", socket, "w1:p2", "", b)
			rewriteCodexBoundaryLaunch(t, root, "duplicate-record", selected)
			if err := codexBoundaryHook(t, root, codexBoundaryPayload(boundaryThread)); err != nil {
				t.Fatal(err)
			}
			wantReports := 1
			if same {
				wantReports = 2
			}
			if len(reports) != wantReports {
				t.Fatalf("reports = %d, want %d", len(reports), wantReports)
			}
			want := []string{"al", providerCodex, resumeVerb, boundaryThread}
			for range wantReports {
				params := (<-reports)["params"].(map[string]any)
				pane := params[requestPaneIDKey].(string)
				if !same && pane != "w1:p2" {
					t.Fatalf("first launcher was claimed: %#v", params)
				}
				assertBoundaryStored(t, root, pane, want)
			}
			if err := codexBoundaryHook(t, root, codexBoundaryPayload(boundaryThread)); err != nil {
				t.Fatal(err)
			}
			if len(reports) != 0 {
				t.Fatal("durable/live recipe emitted duplicate report")
			}
			if _, err := os.Stat(filepath.Join(root, ".agent-layer/tmp/herdr-hook-errors")); !os.IsNotExist(err) {
				t.Fatalf("unrelated pane generated receipt: %v", err)
			}
		})
	}
}

func TestHandleForRootCodexChildSelection(t *testing.T) {
	for _, selected := range []bool{false, true} {
		t.Run("selected_"+strconv.FormatBool(selected), func(t *testing.T) {
			root := t.TempDir()
			tui := startCodexBoundaryTUI(t)
			title := boundaryThread[:29] + "..."
			if selected {
				title = boundaryOther[:29] + "..."
			}
			socket, reports, _ := fakeHerdR(t, fakeHerdROptions{workingDir: root, socketPath: "session/herdr.sock", requestsExpected: 30, codexPanes: []fakeCodexPane{{paneID: "w1:p1", title: title, processPID: tui.PID}}})
			writeCodexBoundaryLaunch(t, root, "child", socket, "w1:p1", "", tui)
			begun := time.Now()
			if err := codexBoundaryHook(t, root, `{"hook_event_name":"UserPromptSubmit","session_id":"`+boundaryThread+`","agent_id":"`+boundaryOther+`"}`); err != nil {
				t.Fatal(err)
			}
			if !selected {
				if len(reports) != 0 || time.Since(begun) > time.Second {
					t.Fatal("background child claimed parent or waited")
				}
				return
			}
			assertBoundaryStored(t, root, "w1:p1", []string{"al", providerCodex, resumeVerb, boundaryOther})
		})
	}
}

func TestHandleForRootCodexPaintAndSelectionRaces(t *testing.T) {
	for _, tc := range []struct {
		name, title, next string
		stale, optout     bool
		wantReports       int
	}{
		{name: "first_paint", title: "workspace", next: boundaryThread[:29] + "...", wantReports: 1},
		{name: "selection_before_write", title: boundaryThread, next: boundaryOther, wantReports: 0},
		{name: "selection_after_write", title: boundaryThread, next: boundaryOther, stale: true, wantReports: 1},
		{name: "optout", title: boundaryThread, optout: true, wantReports: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldPoll, oldWait := persistPollEvery, persistWait
			persistPollEvery, persistWait = time.Millisecond, 5*time.Millisecond
			t.Cleanup(func() { persistPollEvery, persistWait = oldPoll, oldWait })
			root := t.TempDir()
			tui := startCodexBoundaryTUI(t)
			opts := fakeHerdROptions{workingDir: root, socketPath: "session/herdr.sock", requestsExpected: 30, staleFirst: tc.stale, codexPanes: []fakeCodexPane{{paneID: "w1:p1", title: tc.title, processPID: tui.PID}}}
			if tc.stale {
				opts.switchCodexTitleAfterReport = tc.next
			} else {
				opts.switchCodexTitleAfterPaneList = tc.next
			}
			socket, reports, _ := fakeHerdR(t, opts)
			writeCodexBoundaryLaunch(t, root, "paint", socket, "w1:p1", "", tui)
			if tc.optout {
				if err := os.Mkdir(filepath.Join(root, ".codex"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, ".codex/config.toml"), []byte("[tui]\nterminal_title = []\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			begun := time.Now()
			if err := codexBoundaryHook(t, root, codexBoundaryPayload(boundaryThread)); err != nil {
				t.Fatal(err)
			}
			if len(reports) != tc.wantReports {
				t.Fatalf("reports = %d, want %d", len(reports), tc.wantReports)
			}
			if tc.optout && time.Since(begun) > time.Second {
				t.Fatal("optout waited")
			}
		})
	}
}

func TestHandleForRootCodexRecordAndAPIFailures(t *testing.T) {
	for _, tc := range []struct {
		name, authority, processError, payload, config string
		mutate                                         func(*launchContext)
		wantError                                      string
	}{
		{name: "stale_start", mutate: func(c *launchContext) { c.ProcessStart = "stale" }},
		{name: "dead_pid", mutate: func(c *launchContext) { c.PID = 999999; c.DevBypass = true; c.DevExecutable = "/absent/dev/al" }},
		{name: "wrong_root", mutate: func(c *launchContext) { c.ProjectRoot += "/foreign" }},
		{name: "foreign_authority", authority: "herdr:codex:foreign", wantError: "another Codex recovery authority"},
		{name: "process_api", processError: "unavailable", wantError: "process lookup"},
		{name: "wrong_foreground"},
		{name: "unrecognized_foreground", wantError: "unrecognized foreground descendant"},
		{name: "identity_mismatch", wantError: "filename does not match"},
		{name: "malformed_payload", payload: "{", wantError: "read codex HerdR hook event"},
		{name: "malformed_config", config: "[tui]\nterminal_title = 12\n", wantError: "decode Codex terminal title"},
		{name: "missing_id", payload: `{"hook_event_name":"UserPromptSubmit"}`, wantError: "omitted its session identifier"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tui := startCodexBoundaryTUI(t)
			foreground := tui.PID
			if tc.name == "wrong_foreground" {
				foreground = startCodexBoundaryTUI(t).PID
			}
			processName := ""
			if tc.name == "unrecognized_foreground" {
				processName = "sh"
			}
			socket, reports, _ := fakeHerdR(t, fakeHerdROptions{workingDir: root, socketPath: "session/herdr.sock", requestsExpected: 30, codexProcessError: tc.processError, codexPanes: []fakeCodexPane{{paneID: "w1:p1", title: boundaryThread[:29] + "...", authority: tc.authority, processPID: foreground, processName: processName, processArgv: []any{"sh"}}}})
			canonicalRoot, err := canonicalDirectory(root)
			if err != nil {
				t.Fatal(err)
			}
			record := launchContext{Version: 1, PID: tui.PID, ProcessStart: tui.ProcessStart, ProjectRoot: canonicalRoot, Provider: providerCodex, SocketPath: socket, LaunchCWD: canonicalRoot, PaneID: "w1:p1"}
			if tc.mutate != nil {
				tc.mutate(&record)
			}
			rewriteCodexBoundaryLaunch(t, root, "record", record)
			if tc.name == "identity_mismatch" {
				record.PID = startCodexBoundaryTUI(t).PID
				_, record.ProcessStart, _ = processLineage(record.PID)
				writeLaunchContextFixture(t, filepath.Join(root, ".agent-layer/tmp/runs/record", launchContextName(tui.PID, tui.ProcessStart)), record)
			}
			if tc.config != "" {
				if err := os.Mkdir(filepath.Join(root, ".codex"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, ".codex/config.toml"), []byte(tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			payload := tc.payload
			if payload == "" {
				payload = codexBoundaryPayload(boundaryThread)
			}
			err = codexBoundaryHook(t, root, payload)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("error = %v, want %s", err, tc.wantError)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(reports) != 0 {
				t.Fatal("unsafe association reported")
			}
		})
	}
	t.Run("missing_id_without_live_record", func(t *testing.T) {
		if err := codexBoundaryHook(t, t.TempDir(), `{"hook_event_name":"UserPromptSubmit"}`); err != nil {
			t.Fatal(err)
		}
	})
}

func TestHandleForRootCodexCanonicalRecordsAndNewerLiveSession(t *testing.T) {
	root := t.TempDir()
	tui := startCodexBoundaryTUI(t)
	socket, reports, _ := fakeHerdR(t, fakeHerdROptions{workingDir: root, socketPath: "session/herdr.sock", requestsExpected: 30, codexPanes: []fakeCodexPane{{paneID: "w1:p1", title: boundaryThread[:29] + "...", processPID: tui.PID, liveSource: sourceCodexAL, liveAgent: providerCodex, liveKind: "id", liveValue: boundaryOther}}})
	writeCodexBoundaryLaunch(t, root, "live", socket, "w1:p1", "", tui)
	deadDir := filepath.Join(root, ".agent-layer/tmp/runs/dead")
	if err := os.MkdirAll(deadDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "absent-history"), filepath.Join(deadDir, launchContextName(999999, "dead"))); err != nil {
		t.Fatal(err)
	}
	want := []string{"al", providerCodex, resumeVerb, boundaryThread}
	writeCodexStoredResume(t, root, "w1", 1, "1", want)
	original := readDirFunc
	reads := map[string]int{}
	readDirFunc = func(p string) ([]os.DirEntry, error) { reads[p]++; return original(p) }
	t.Cleanup(func() { readDirFunc = original })
	if err := codexBoundaryHook(t, root, codexBoundaryPayload(boundaryThread)); err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 {
		t.Fatalf("newer live association suppressed selected recipe: %d reports", len(reports))
	}
	for _, n := range reads {
		if n != 1 {
			t.Fatalf("repeated run scan: %#v", reads)
		}
	}
	if len(reads) != 3 {
		t.Fatalf("run scan = %#v", reads)
	}
	assertBoundaryStored(t, root, "w1:p1", want)
}

func sourceTestExecutable(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(path)
}

func startCodexBoundaryTUI(t *testing.T) launchContext {
	t.Helper()
	process := exec.Command("sleep", "30") //nolint:gosec // test-owned retained TUI PID fixture.
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = process.Process.Kill(); _ = process.Wait() })
	_, start, err := processLineage(process.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	return launchContext{PID: process.Process.Pid, ProcessStart: start}
}

func writeCodexBoundaryLaunch(t *testing.T, root, name, socket, paneID, executable string, identity launchContext) launchContext {
	t.Helper()
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	context := launchContext{Version: 1, PID: identity.PID, ProcessStart: identity.ProcessStart, ProjectRoot: canonicalRoot, Provider: providerCodex, SocketPath: socket, PaneID: paneID, DevBypass: executable != "", DevExecutable: executable}
	if !filepath.IsAbs(socket) {
		context.LaunchCWD = canonicalRoot
	}
	rewriteCodexBoundaryLaunch(t, root, name, context)
	return context
}

func rewriteCodexBoundaryLaunch(t *testing.T, root, name string, context launchContext) {
	t.Helper()
	directory := filepath.Join(root, ".agent-layer", "tmp", "runs", name)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, launchContextName(context.PID, context.ProcessStart))
	writeLaunchContextFixture(t, path, context)
}

func assertBoundaryStored(t *testing.T, root, pane string, argv []string) {
	t.Helper()
	path := filepath.Join(root, "session", "session.json")
	stored, err := storedResumeAt(path, pane, providers[providerCodex], argv)
	if err != nil || !stored {
		t.Fatalf("canonical persisted command for %s = %t, %v", pane, stored, err)
	}
}

func writeCodexStoredResume(t *testing.T, root, workspace string, publicNumber int, internalPane string, argv []string) {
	t.Helper()
	document := map[string]any{
		"workspaces": []any{map[string]any{
			"id":                  workspace,
			"public_pane_numbers": map[string]any{internalPane: publicNumber},
			"tabs": []any{map[string]any{
				"panes": map[string]any{internalPane: map[string]any{
					"agent_resume": map[string]any{"source": sourceCodexAL, "agent": providerCodex, "argv": argv},
				}},
			}},
		}},
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "session", "session.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestHandleForRootCodexUsesPreexistingBijectiveBase32PersistedRecipes(t *testing.T) {
	// HerdR v0.9.3 src/workspace.rs: literal public numbers, independent internal keys.
	oldPoll, oldWait := persistPollEvery, persistWait
	persistPollEvery, persistWait = time.Millisecond, 25*time.Millisecond
	t.Cleanup(func() { persistPollEvery, persistWait = oldPoll, oldWait })
	for _, tc := range []struct {
		name, paneID, workspace, internalPane string
		publicNumber                          int
	}{
		{name: "bijective_base32_letter", paneID: "wV:pF", workspace: "wV", publicNumber: 15, internalPane: "47"},
		{name: "bijective_base32_beyond_H", paneID: "wV:pJ", workspace: "wV", publicNumber: 18, internalPane: "59"},
		{name: "bijective_base32_last_letter", paneID: "wV:pZ", workspace: "wV", publicNumber: 31, internalPane: "71"},
		{name: "bijective_base32_zero_symbol", paneID: "wV:p0", workspace: "wV", publicNumber: 32, internalPane: "79"},
		{name: "bijective_base32_thirty_six", paneID: "wV:p14", workspace: "wV", publicNumber: 36, internalPane: "89"},
		{name: "bijective_base32_multidigit", paneID: "wV:p10", workspace: "wV", publicNumber: 64, internalPane: "83"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tui := startCodexBoundaryTUI(t)
			socket, reports, _ := fakeHerdR(t, fakeHerdROptions{
				workingDir:       root,
				socketPath:       "session/herdr.sock",
				canonicalPane:    tc.paneID,
				workspace:        tc.workspace,
				publicNumber:     tc.publicNumber,
				internalPane:     tc.internalPane,
				requestsExpected: 30,
				codexPanes: []fakeCodexPane{{
					paneID:     tc.paneID,
					title:      boundaryThread[:29] + "...",
					processPID: tui.PID,
					liveSource: sourceCodexAL,
					liveAgent:  providerCodex,
					liveKind:   "id",
					liveValue:  boundaryThread,
				}},
			})
			want := []string{"al", providerCodex, resumeVerb, boundaryThread}
			writeCodexBoundaryLaunch(t, root, "bijective_base32-"+tc.name, socket, tc.paneID, "", tui)
			writeCodexStoredResume(t, root, tc.workspace, tc.publicNumber, tc.internalPane, want)
			if err := codexBoundaryHook(t, root, codexBoundaryPayload(boundaryThread)); err != nil {
				t.Fatal(err)
			}
			if len(reports) != 0 {
				t.Fatalf("preexisting recipe reported again: %d reports", len(reports))
			}
			assertBoundaryStored(t, root, tc.paneID, want)
		})
	}
}

func TestHandleForRootCodexNewForegroundLaunchInSamePane(t *testing.T) {
	root := t.TempDir()
	old, newer := startCodexBoundaryTUI(t), startCodexBoundaryTUI(t)
	socket, reports, _ := fakeHerdR(t, fakeHerdROptions{workingDir: root, socketPath: "session/herdr.sock", requestsExpected: 40,
		codexPanes: []fakeCodexPane{{paneID: "w1:p1", title: boundaryThread[:29] + "...", processPID: newer.PID}}})
	writeCodexBoundaryLaunch(t, root, "old", socket, "w1:p1", "", old)
	executable := sourceTestExecutable(t)
	writeCodexBoundaryLaunch(t, root, "new", socket, "w1:p1", executable, newer)
	if err := codexBoundaryHook(t, root, codexBoundaryPayload(boundaryThread)); err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 {
		t.Fatalf("reports = %d", len(reports))
	}
	want := []string{commandEnv, EnvDevBypass + "=1", EnvDevExecutable + "=" + executable, executable, providerCodex, resumeVerb, boundaryThread}
	params := (<-reports)["params"].(map[string]any)
	if !equalStrings(interfaceStrings(params[requestResumeArgvKey]), want) {
		t.Fatalf("new foreground recipe = %#v", params)
	}
	assertBoundaryStored(t, root, "w1:p1", want)
}
