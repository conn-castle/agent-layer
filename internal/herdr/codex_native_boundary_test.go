package herdr

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestHandleForRootCodexNativeBoundary is deliberately a public-hook test. Its
// control server re-execs this test binary, then spawns a one-shot hook as its
// child. That makes the kernel-authenticated Unix peer an actual hook
// ancestor; a separately-started hook is a sibling and must be rejected.
func TestHandleForRootCodexNativeBoundary(t *testing.T) {
	switch os.Getenv("AL_TEST_CODEX_BOUNDARY_MODE") {
	case "server":
		runCodexBoundaryServer()
		os.Exit(0)
	case "hook":
		runCodexBoundaryHookMode()
		os.Exit(0)
	}

	t.Run("detached_hook_uses_selected_title_not_stale_environment_or_process_argv", func(t *testing.T) {
		root := t.TempDir()
		home, controlSocket := newCodexBoundaryControlSocket(t, root)
		oldID := "01a1138f-7df0-7fa0-94ae-820334783a28"
		selectedID := "01a1138f-7df0-7fa0-94ae-820334783a29"
		server := startCodexBoundaryServer(t, root, controlSocket, []string{selectedID})
		tui := startCodexBoundaryTUI(t)
		socket, reports, _ := fakeHerdR(t, fakeHerdROptions{
			requestsExpected: 20,
			workingDir:       root,
			socketPath:       "session/herdr.sock",
			codexPanes: []fakeCodexPane{{
				paneID: "w1:p1", title: "✳ " + selectedID + " | selected after picker",
				processPID: tui.PID, processArgv: []any{sourceTestExecutable(t), "resume", oldID},
			}},
		})
		writeCodexBoundaryLaunch(t, root, "picker", socket, "w1:p1", home, sourceTestExecutable(t), tui)
		result := server.invoke(t, boundaryHookInstruction{
			Root: root, Provider: providerCodex,
			Payload: `{"hook_event_name":"UserPromptSubmit","session_id":"` + selectedID + `"}`,
			Environ: []string{EnvEnabled + "=1", EnvSocketPath + "=/stale.sock", EnvPaneID + "=stale", "CODEX_THREAD_ID=" + oldID, EnvDispatch + "=1"},
		})
		if result.Err != "" || result.Out != "" {
			t.Fatalf("detached hook result = %#v", result)
		}
		params := (<-reports)["params"].(map[string]any)
		argv := interfaceStrings(params[requestResumeArgvKey])
		want := []string{commandEnv, EnvDevBypass + "=1", EnvDevExecutable + "=" + sourceTestExecutable(t), sourceTestExecutable(t), providerCodex, resumeVerb, selectedID}
		if params["pane_id"] != "w1:p1" || params["source"] != "al:codex" || !equalStrings(argv, want) {
			t.Fatalf("report pane/source/argv = %q %q %#v, want w1:p1 al:codex %#v", params["pane_id"], params["source"], argv, want)
		}
		assertBoundaryStored(t, root, "w1:p1", want)
	})

	t.Run("slow_managed_initialize_still_registers_selected_thread", func(t *testing.T) {
		t.Setenv("AL_TEST_CODEX_INITIALIZE_DELAY_MS", "900")
		root := t.TempDir()
		home, controlSocket := newCodexBoundaryControlSocket(t, root)
		id := "b1a1138f-7df0-7fa0-94ae-820334783a29"
		server := startCodexBoundaryServer(t, root, controlSocket, []string{id})
		tui := startCodexBoundaryTUI(t)
		socket, reports, _ := fakeHerdR(t, fakeHerdROptions{requestsExpected: 30, workingDir: root, socketPath: "session/herdr.sock", codexPanes: []fakeCodexPane{{paneID: "w1:p1", title: "✳ " + id + " | selected", processPID: tui.PID, processArgv: []any{sourceTestExecutable(t)}}}})
		writeCodexBoundaryLaunch(t, root, "slow-init", socket, "w1:p1", home, sourceTestExecutable(t), tui)
		result := server.invoke(t, boundaryHookInstruction{Root: root, Provider: providerCodex, Payload: `{"hook_event_name":"UserPromptSubmit","session_id":"` + id + `"}`})
		if result.Err != "" || result.Out != "" {
			t.Fatalf("slow initialize hook = %#v", result)
		}
		params := (<-reports)["params"].(map[string]any)
		want := []string{commandEnv, EnvDevBypass + "=1", EnvDevExecutable + "=" + sourceTestExecutable(t), sourceTestExecutable(t), providerCodex, resumeVerb, id}
		if !equalStrings(interfaceStrings(params[requestResumeArgvKey]), want) {
			t.Fatalf("slow initialize recipe = %#v, want %#v", params, want)
		}
		assertBoundaryStored(t, root, "w1:p1", want)
	})

	t.Run("ancestor_managed_response_failure_is_durable", func(t *testing.T) {
		root := t.TempDir()
		home, controlSocket := newCodexBoundaryControlSocket(t, root)
		id := "c1a1138f-7df0-7fa0-94ae-820334783a29"
		server := startCodexBoundaryServer(t, root, controlSocket, []string{"invalid-native-id"})
		_, start, err := processLineage(os.Getpid())
		if err != nil {
			t.Fatal(err)
		}
		socket, reports, _ := fakeHerdR(t, fakeHerdROptions{requestsExpected: 20, workingDir: root, socketPath: "session/herdr.sock", codexPanes: []fakeCodexPane{{paneID: "w1:p1", title: "✳ " + id + " | selected", processPID: server.command.Process.Pid, processArgv: []any{sourceTestExecutable(t)}}}})
		writeCodexBoundaryLaunch(t, root, "managed-failure", socket, "w1:p1", home, sourceTestExecutable(t), launchContext{PID: os.Getpid(), ProcessStart: start})
		result := server.invoke(t, boundaryHookInstruction{Root: root, Provider: providerCodex, Payload: `{"hook_event_name":"UserPromptSubmit","session_id":"` + id + `"}`})
		if !strings.Contains(result.Err, "invalid ID") || result.Out != "" || result.ErrOut == "" || len(reports) != 0 {
			t.Fatalf("managed failure = %#v reports=%d", result, len(reports))
		}
		receipts, err := os.ReadDir(filepath.Join(root, ".agent-layer", "tmp", "herdr-hook-errors"))
		if err != nil || len(receipts) == 0 {
			t.Fatalf("managed failure receipt = %#v, %v", receipts, err)
		}
	})

	// The server is a child of this re-executed test process, which is recorded
	// as pane A. This is the stock auto-start topology: pane B's hook has A in
	// its ancestry solely because A owns the shared server. It must still route
	// by B's live title, including when a dispatch boundary sits at A.
	t.Run("server_child_of_recorded_launch_routes_only_selected_peer_pane", func(t *testing.T) {
		for _, withBoundary := range []bool{false, true} {
			t.Run(fmt.Sprintf("dispatch_boundary_%t", withBoundary), func(t *testing.T) {
				root := t.TempDir()
				home, controlSocket := newCodexBoundaryControlSocket(t, root)
				firstID := "81a1138f-7df0-7fa0-94ae-820334783a28"
				secondID := "81a1138f-7df0-7fa0-94ae-820334783a29"
				server := startCodexBoundaryServer(t, root, controlSocket, []string{firstID, secondID})
				_, parentStart, err := processLineage(os.Getpid())
				if err != nil {
					t.Fatal(err)
				}
				firstLaunch := launchContext{PID: os.Getpid(), ProcessStart: parentStart}
				secondLaunch := startCodexBoundaryTUI(t)
				socket, reports, _ := fakeHerdR(t, fakeHerdROptions{
					requestsExpected: 40, workingDir: root, socketPath: "session/herdr.sock",
					codexPanes: []fakeCodexPane{
						{paneID: "w1:p1", title: "✳ " + firstID + " | first", processPID: server.command.Process.Pid, processArgv: []any{sourceTestExecutable(t)}},
						{paneID: "w1:p2", title: "✳ " + secondID + " | second", processPID: secondLaunch.PID, processArgv: []any{sourceTestExecutable(t)}},
					},
				})
				writeCodexBoundaryLaunch(t, root, "auto-start-first", socket, "w1:p1", home, sourceTestExecutable(t), firstLaunch)
				writeCodexBoundaryLaunch(t, root, "attached-second", socket, "w1:p2", home, sourceTestExecutable(t), secondLaunch)
				// A boundary above the server must not make B borrow A; it is not between
				// the hook and the authenticated managed peer.
				if withBoundary {
					boundaryDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "auto-start-boundary")
					if err := os.MkdirAll(boundaryDir, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(boundaryDir, dispatchBoundaryName(firstLaunch.PID, firstLaunch.ProcessStart)), []byte(fmt.Sprintf("%d %s\n", firstLaunch.PID, firstLaunch.ProcessStart)), 0o600); err != nil {
						t.Fatal(err)
					}
				}

				result := server.invoke(t, boundaryHookInstruction{Root: root, Provider: providerCodex, Payload: `{"hook_event_name":"UserPromptSubmit","session_id":"` + secondID + `"}`})
				if result.Err != "" || result.Out != "" {
					t.Fatalf("second-pane result = %#v", result)
				}
				params := (<-reports)["params"].(map[string]any)
				if params["pane_id"] != "w1:p2" || !contains(interfaceStrings(params[requestResumeArgvKey]), secondID) {
					t.Fatalf("shared-server hook reported the wrong pane: %#v", params)
				}
				// The managed server stays available, but B visibly owns another thread:
				// a plain native mismatch is definitive and never overwrites A.
				mismatch := "81a1138f-7df0-7fa0-94ae-820334783a2a"
				result = server.invoke(t, boundaryHookInstruction{Root: root, Provider: providerCodex, Payload: `{"hook_event_name":"UserPromptSubmit","session_id":"` + mismatch + `"}`})
				if result.Err != "" || result.Out != "" || len(reports) != 0 {
					t.Fatalf("mismatched title mutated a pane: result=%#v reports=%d", result, len(reports))
				}
			})
		}
	})

	t.Run("selection_change_after_first_report_does_not_retry_old_thread", func(t *testing.T) {
		root := t.TempDir()
		home, controlSocket := newCodexBoundaryControlSocket(t, root)
		oldID := "91a1138f-7df0-7fa0-94ae-820334783a28"
		newID := "91a1138f-7df0-7fa0-94ae-820334783a29"
		server := startCodexBoundaryServer(t, root, controlSocket, []string{oldID, newID})
		tui := startCodexBoundaryTUI(t)
		socket, reports, _ := fakeHerdR(t, fakeHerdROptions{
			requestsExpected: 30, staleFirst: true, workingDir: root, socketPath: "session/herdr.sock",
			switchCodexTitleAfterReport: "✳ " + newID + " | selected after picker",
			codexPanes:                  []fakeCodexPane{{paneID: "w1:p1", title: "✳ " + oldID + " | old selection", processPID: tui.PID, processArgv: []any{sourceTestExecutable(t)}}},
		})
		writeCodexBoundaryLaunch(t, root, "retry-switch", socket, "w1:p1", home, sourceTestExecutable(t), tui)
		result := server.invoke(t, boundaryHookInstruction{Root: root, Provider: providerCodex, Payload: `{"hook_event_name":"UserPromptSubmit","session_id":"` + oldID + `"}`, PersistPollMilliseconds: 1, PersistWaitMilliseconds: 5})
		if result.Err != "" || result.Out != "" {
			t.Fatalf("switching hook result = %#v", result)
		}
		if len(reports) != 1 {
			t.Fatalf("selection switch issued %d reports, want exactly the first attempt", len(reports))
		}
		params := (<-reports)["params"].(map[string]any)
		if !contains(interfaceStrings(params[requestResumeArgvKey]), oldID) {
			t.Fatalf("first report did not carry the original selected thread: %#v", params)
		}
	})

	t.Run("embedded_first_prompt_waits_for_its_title_before_reporting", func(t *testing.T) {
		root := t.TempDir()
		home, _ := newCodexBoundaryControlSocket(t, root)
		id := "92a1138f-7df0-7fa0-94ae-820334783a29"
		socket, reports, _ := fakeHerdR(t, fakeHerdROptions{
			requestsExpected: 5, workingDir: root, socketPath: "session/herdr.sock",
			switchCodexTitleAfterPaneList: "✳ " + id + " | first paint",
			codexPanes:                    []fakeCodexPane{{paneID: "w1:p1", title: "", processPID: os.Getpid(), processArgv: []any{sourceTestExecutable(t)}}},
		})
		_, start, err := processLineage(os.Getpid())
		if err != nil {
			t.Fatal(err)
		}
		writeCodexBoundaryLaunch(t, root, "embedded-first-paint", socket, "w1:p1", home, sourceTestExecutable(t), launchContext{PID: os.Getpid(), ProcessStart: start})
		var out, errOut bytes.Buffer
		if err := HandleForRoot(providerCodex, root, strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"`+id+`"}`), &out, &errOut, nil); err != nil || out.String() != "" {
			t.Fatalf("embedded first-prompt result = %v output=%q diagnostic=%q", err, out.String(), errOut.String())
		}
		params := (<-reports)["params"].(map[string]any)
		want := []string{commandEnv, EnvDevBypass + "=1", EnvDevExecutable + "=" + sourceTestExecutable(t), sourceTestExecutable(t), providerCodex, resumeVerb, id}
		if params["pane_id"] != "w1:p1" || !equalStrings(interfaceStrings(params[requestResumeArgvKey]), want) {
			t.Fatalf("embedded first-prompt recipe = %#v, want pane w1:p1 argv %#v", params, want)
		}
		assertBoundaryStored(t, root, "w1:p1", want)
	})

	t.Run("disabled_project_titles_do_not_wait_for_paint", func(t *testing.T) {
		root := t.TempDir()
		home, controlSocket := newCodexBoundaryControlSocket(t, root)
		id := "93a1138f-7df0-7fa0-94ae-820334783a29"
		server := startCodexBoundaryServer(t, root, controlSocket, []string{id})
		if err := os.MkdirAll(filepath.Join(root, ".codex"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".codex", "config.toml"), []byte("[tui]\nterminal_title = []\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, start, err := processLineage(os.Getpid())
		if err != nil {
			t.Fatal(err)
		}
		socket, reports, _ := fakeHerdR(t, fakeHerdROptions{
			requestsExpected: 1, workingDir: root, socketPath: "session/herdr.sock",
			codexPanes: []fakeCodexPane{{paneID: "w1:p1", title: "", processPID: server.command.Process.Pid, processArgv: []any{sourceTestExecutable(t)}}},
		})
		writeCodexBoundaryLaunch(t, root, "disabled-title", socket, "w1:p1", home, sourceTestExecutable(t), launchContext{PID: os.Getpid(), ProcessStart: start})
		started := time.Now()
		result := server.invoke(t, boundaryHookInstruction{Root: root, Provider: providerCodex, Payload: `{"hook_event_name":"UserPromptSubmit","session_id":"` + id + `"}`})
		if result.Err != "" || result.Out != "" {
			t.Fatalf("disabled-title result = %#v", result)
		}
		if elapsed := time.Since(started); elapsed >= time.Second {
			t.Fatalf("disabled title waited %s for paint", elapsed)
		}
		if len(reports) != 0 {
			t.Fatalf("disabled title reported despite no ownership evidence: %d reports", len(reports))
		}
	})

	t.Run("dead_record_with_missing_development_executable_does_not_block_live_pane", func(t *testing.T) {
		root := t.TempDir()
		home, controlSocket := newCodexBoundaryControlSocket(t, root)
		id := "a1a1138f-7df0-7fa0-94ae-820334783a29"
		server := startCodexBoundaryServer(t, root, controlSocket, []string{id})
		tui := startCodexBoundaryTUI(t)
		socket, reports, _ := fakeHerdR(t, fakeHerdROptions{requestsExpected: 20, workingDir: root, socketPath: "session/herdr.sock", codexPanes: []fakeCodexPane{{paneID: "w1:p1", title: "✳ " + id + " | live", processPID: tui.PID, processArgv: []any{sourceTestExecutable(t)}}}})
		live := writeCodexBoundaryLaunch(t, root, "live", socket, "w1:p1", home, sourceTestExecutable(t), tui)
		dead := live
		dead.PID, dead.ProcessStart = 999999, "dead-process"
		dead.DevExecutable = filepath.Join(root, "deleted", "al")
		rewriteCodexBoundaryLaunch(t, root, "dead-missing-exe", dead)
		truncatedDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "dead-truncated")
		if err := os.MkdirAll(truncatedDir, 0o700); err != nil {
			t.Fatal(err)
		}
		truncatedPath := filepath.Join(truncatedDir, launchContextName(999999, "dead-truncated"))
		if err := os.WriteFile(truncatedPath, []byte(`{"version":2`), 0o600); err != nil {
			t.Fatal(err)
		}
		canonicalRoot, err := canonicalDirectory(root)
		if err != nil {
			t.Fatal(err)
		}
		if err := rememberCodexLiveLaunch(canonicalRoot, truncatedPath); err != nil {
			t.Fatal(err)
		}
		result := server.invoke(t, boundaryHookInstruction{Root: root, Provider: providerCodex, Payload: `{"hook_event_name":"UserPromptSubmit","session_id":"` + id + `"}`})
		if result.Err != "" || result.Out != "" {
			t.Fatalf("live registration blocked by dead record: %#v", result)
		}
		if params := (<-reports)["params"].(map[string]any); params["pane_id"] != "w1:p1" {
			t.Fatalf("live record report = %#v", params)
		}
	})

	t.Run("same_loaded_thread_in_two_live_panes_persists_identical_recipes_and_shortcuts", func(t *testing.T) {
		root := t.TempDir()
		home, controlSocket := newCodexBoundaryControlSocket(t, root)
		id := "11a1138f-7df0-7fa0-94ae-820334783a29"
		server := startCodexBoundaryServer(t, root, controlSocket, []string{id})
		firstTUI, secondTUI := startCodexBoundaryTUI(t), startCodexBoundaryTUI(t)
		socket, reports, _ := fakeHerdR(t, fakeHerdROptions{
			reportsExpected: 2, paneGetsExpected: 4, requestsExpected: 100, distinctPanes: true, workingDir: root, socketPath: "session/herdr.sock",
			codexPanes: []fakeCodexPane{
				{paneID: "w1:p1", title: "✳ " + id + " | first", processPID: firstTUI.PID, processArgv: []any{sourceTestExecutable(t), "resume", "old-thread"}},
				{paneID: "w1:p2", title: "✳ " + id + " | second", processPID: secondTUI.PID, processArgv: []any{sourceTestExecutable(t), "resume", "old-thread"}},
			},
		})
		writeCodexBoundaryLaunch(t, root, "one", socket, "w1:p1", home, sourceTestExecutable(t), firstTUI)
		writeCodexBoundaryLaunch(t, root, "two", socket, "w1:p2", home, sourceTestExecutable(t), secondTUI)
		result := server.invoke(t, boundaryHookInstruction{Root: root, Provider: providerCodex, Payload: `{"hook_event_name":"UserPromptSubmit","session_id":"` + id + `"}`})
		if result.Err != "" || result.Out != "" {
			t.Fatalf("first hook result = %#v", result)
		}
		got := map[string][]string{}
		for range 2 {
			params := (<-reports)["params"].(map[string]any)
			got[params["pane_id"].(string)] = interfaceStrings(params[requestResumeArgvKey])
		}
		if !equalStrings(got["w1:p1"], got["w1:p2"]) {
			t.Fatalf("same-thread pane recipes were not identical: %#v", got)
		}
		for _, pane := range []string{"w1:p1", "w1:p2"} {
			want := []string{commandEnv, EnvDevBypass + "=1", EnvDevExecutable + "=" + sourceTestExecutable(t), sourceTestExecutable(t), providerCodex, resumeVerb, id}
			if !equalStrings(got[pane], want) {
				t.Fatalf("%s argv = %#v, want %#v", pane, got[pane], want)
			}
			assertBoundaryStored(t, root, pane, want)
		}
		shortcut := server.invoke(t, boundaryHookInstruction{Root: root, Provider: providerCodex, Payload: `{"hook_event_name":"UserPromptSubmit","session_id":"` + id + `"}`})
		if shortcut.Err != "" || shortcut.Out != "" {
			t.Fatalf("stored-command shortcut result = %#v", shortcut)
		}
		if len(reports) != 0 {
			t.Fatalf("exact stored commands emitted a duplicate report: %#v", <-reports)
		}
	})

	t.Run("unsafe_associations_do_not_report", func(t *testing.T) {
		cases := []struct {
			name, title   string
			loaded        []string
			mutate        func(t *testing.T, root, home string, launch *launchContext)
			wantError     bool
			wantErrorText string
		}{
			{name: "sibling_control_server", loaded: []string{"01a1138f-7df0-7fa0-94ae-820334783a29"}, wantError: true},
			// Neither valid full ID has a rollout file. The loaded server must
			// still reject their shared title prefix, rather than guessing from disk.
			{name: "ambiguous_prefix_with_unpersisted_loaded_ids", title: "✳ 21a1138f-7df0-7fa0-94ae-82033... | ambiguous", loaded: []string{"21a1138f-7df0-7fa0-94ae-820334783a29", "21a1138f-7df0-7fa0-94ae-82033bbbbbbb"}, wantError: true, wantErrorText: "matches multiple loaded threads"},
			{name: "stale_pid_start", title: "✳ 31a1138f-7df0-7fa0-94ae-820334783a29 | stale", loaded: []string{"31a1138f-7df0-7fa0-94ae-820334783a29"}, mutate: func(_ *testing.T, _ string, _ string, launch *launchContext) { launch.ProcessStart = "stale-start" }},
			{name: "wrong_home", title: "✳ 41a1138f-7df0-7fa0-94ae-820334783a29 | wrong home", loaded: []string{"41a1138f-7df0-7fa0-94ae-820334783a29"}, mutate: func(_ *testing.T, root string, _ string, launch *launchContext) {
				launch.CodexHome = filepath.Join(root, "other-home")
			}, wantError: true},
			{name: "wrong_root", title: "✳ 51a1138f-7df0-7fa0-94ae-820334783a29 | wrong root", loaded: []string{"51a1138f-7df0-7fa0-94ae-820334783a29"}, mutate: func(_ *testing.T, root string, _ string, launch *launchContext) {
				launch.ProjectRoot = filepath.Join(root, "other-project")
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				root := t.TempDir()
				home, controlSocket := newCodexBoundaryControlSocket(t, root)
				id := "01a1138f-7df0-7fa0-94ae-820334783a29"
				if len(tc.loaded) > 0 {
					id = tc.loaded[0]
				}
				if tc.title == "" {
					tc.title = "✳ " + id + " | selected"
				}
				server := startCodexBoundaryServer(t, root, controlSocket, tc.loaded)
				tui := startCodexBoundaryTUI(t)
				socket, reports, _ := fakeHerdR(t, fakeHerdROptions{
					requestsExpected: 3, workingDir: root, socketPath: "session/herdr.sock",
					codexPanes: []fakeCodexPane{{
						paneID: "w1:p1", title: tc.title, processPID: tui.PID,
						processArgv: []any{sourceTestExecutable(t), "resume", "old-thread"},
					}},
				})
				launch := writeCodexBoundaryLaunch(t, root, tc.name, socket, "w1:p1", home, sourceTestExecutable(t), tui)
				if tc.mutate != nil {
					tc.mutate(t, root, home, &launch)
					rewriteCodexBoundaryLaunch(t, root, tc.name, launch)
				}
				var result boundaryHookResult
				if tc.name == "sibling_control_server" {
					result = runCodexBoundaryHook(t, boundaryHookInstruction{Root: root, Provider: providerCodex, Payload: `{"hook_event_name":"UserPromptSubmit","session_id":"` + id + `"}`})
				} else {
					result = server.invoke(t, boundaryHookInstruction{Root: root, Provider: providerCodex, Payload: `{"hook_event_name":"UserPromptSubmit","session_id":"` + id + `"}`})
				}
				if tc.wantError && result.Err == "" {
					t.Fatalf("unsafe association returned success: %#v", result)
				}
				if tc.wantErrorText != "" && !strings.Contains(result.Err, tc.wantErrorText) {
					t.Fatalf("unsafe association error = %q, want %q", result.Err, tc.wantErrorText)
				}
				if result.Out != "" || len(reports) != 0 {
					t.Fatalf("unsafe association output/report = %#v reports=%d", result, len(reports))
				}
			})
		}
	})

	t.Run("selected_child_uses_unique_selected_title_never_parent_argv", func(t *testing.T) {
		root := t.TempDir()
		home, controlSocket := newCodexBoundaryControlSocket(t, root)
		parentID := "61a1138f-7df0-7fa0-94ae-820334783a28"
		childID := "61a1138f-7df0-7fa0-94ae-820334783a29"
		server := startCodexBoundaryServer(t, root, controlSocket, []string{childID})
		tui := startCodexBoundaryTUI(t)
		socket, reports, _ := fakeHerdR(t, fakeHerdROptions{requestsExpected: 20, workingDir: root, socketPath: "session/herdr.sock", codexPanes: []fakeCodexPane{{paneID: "w1:p1", title: "✳ " + childID + " | selected child", processPID: tui.PID, processArgv: []any{sourceTestExecutable(t), "resume", parentID}}}})
		writeCodexBoundaryLaunch(t, root, "selected-child", socket, "w1:p1", home, sourceTestExecutable(t), tui)
		result := server.invoke(t, boundaryHookInstruction{Root: root, Provider: providerCodex, Payload: `{"hook_event_name":"UserPromptSubmit","session_id":"` + parentID + `","agent_id":"` + childID + `"}`})
		if result.Err != "" || result.Out != "" {
			t.Fatalf("selected child result = %#v", result)
		}
		argv := interfaceStrings((<-reports)["params"].(map[string]any)[requestResumeArgvKey])
		if !contains(argv, childID) || contains(argv, parentID) {
			t.Fatalf("selected child argv incorrectly used parent process argv: %#v", argv)
		}
	})

	t.Run("embedded_stale_managed_residue_requires_live_matching_pane", func(t *testing.T) {
		for _, residue := range []string{"dangling_alias", "refused_socket"} {
			for _, matchingTitle := range []bool{true, false} {
				t.Run(residue+"_matching_title_"+strconv.FormatBool(matchingTitle), func(t *testing.T) {
					root := t.TempDir()
					home, controlSocket := newCodexBoundaryControlSocket(t, root)
					if residue == "refused_socket" {
						listener, err := net.Listen("unix", controlSocket)
						if err != nil {
							t.Fatal(err)
						}
						listener.(*net.UnixListener).SetUnlinkOnClose(false)
						if err := listener.Close(); err != nil {
							t.Fatal(err)
						}
						info, err := os.Stat(controlSocket)
						if err != nil || info.Mode()&os.ModeSocket == 0 {
							t.Fatalf("refused fixture must retain an unlistening socket: %v", err)
						}
					}
					id := "c1a1138f-7df0-7fa0-94ae-820334783a29"
					titleID := id
					if !matchingTitle {
						titleID = "c1a1138f-7df0-7fa0-94ae-820334783a28"
					}
					requests := 2 // canonical pane plus title-only embedded revalidation.
					if matchingTitle {
						requests = 4 // plus process validation and report.
					}
					socket, reports, _ := fakeHerdR(t, fakeHerdROptions{
						requestsExpected: requests, workingDir: root, socketPath: "session/herdr.sock",
						codexPanes: []fakeCodexPane{{paneID: "w1:p1", title: "✳ " + titleID + " | embedded", processPID: os.Getpid(), processArgv: []any{"codex"}}},
					})
					_, start, err := processLineage(os.Getpid())
					if err != nil {
						t.Fatal(err)
					}
					writeCodexBoundaryLaunch(t, root, residue, socket, "w1:p1", home, sourceTestExecutable(t), launchContext{PID: os.Getpid(), ProcessStart: start})
					var out bytes.Buffer
					err = HandleForRoot(providerCodex, root, strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"`+id+`"}`), &out, io.Discard, nil)
					if err != nil || out.String() != "" {
						t.Fatalf("embedded stale-residue result = %v, output %q", err, out.String())
					}
					if matchingTitle {
						params := (<-reports)["params"].(map[string]any)
						if params["pane_id"] != "w1:p1" || !contains(interfaceStrings(params[requestResumeArgvKey]), id) {
							t.Fatalf("matching embedded pane report = %#v", params)
						}
					} else if len(reports) != 0 {
						t.Fatalf("different embedded title reported %#v", <-reports)
					}
				})
			}
		}
	})

	t.Run("embedded_nested_thread_and_retry_races_are_safe_noops", func(t *testing.T) {
		root := t.TempDir()
		home, _ := newCodexBoundaryControlSocket(t, root) // dangling alias: embedded compatibility path.
		parentID := "d1a1138f-7df0-7fa0-94ae-820334783a28"
		nestedID := "d1a1138f-7df0-7fa0-94ae-820334783a29"
		socket, reports, _ := fakeHerdR(t, fakeHerdROptions{requestsExpected: 1, workingDir: root, socketPath: "session/herdr.sock", codexPanes: []fakeCodexPane{{paneID: "w1:p1", title: "✳ " + nestedID + " | nested", processPID: os.Getpid(), processArgv: []any{"codex"}}}})
		_, start, err := processLineage(os.Getpid())
		if err != nil {
			t.Fatal(err)
		}
		writeCodexBoundaryLaunch(t, root, "nested", socket, "w1:p1", home, sourceTestExecutable(t), launchContext{PID: os.Getpid(), ProcessStart: start})
		if err := HandleForRoot(providerCodex, root, strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"`+nestedID+`"}`), io.Discard, io.Discard, []string{"CODEX_THREAD_ID=" + parentID}); err != nil {
			t.Fatalf("embedded nested Codex must no-op: %v", err)
		}
		if len(reports) != 0 {
			t.Fatalf("embedded nested Codex reported %#v", <-reports)
		}

		oldPoll, oldWait := persistPollEvery, persistWait
		persistPollEvery, persistWait = time.Millisecond, 5*time.Millisecond
		defer func() { persistPollEvery, persistWait = oldPoll, oldWait }()
		root = t.TempDir()
		home, _ = newCodexBoundaryControlSocket(t, root)
		oldID := "e1a1138f-7df0-7fa0-94ae-820334783a28"
		newID := "e1a1138f-7df0-7fa0-94ae-820334783a29"
		socket, reports, _ = fakeHerdR(t, fakeHerdROptions{requestsExpected: 5, staleFirst: true, workingDir: root, socketPath: "session/herdr.sock", switchCodexTitleAfterReport: "✳ " + newID + " | switched", codexPanes: []fakeCodexPane{{paneID: "w1:p1", title: "✳ " + oldID + " | selected", processPID: os.Getpid(), processArgv: []any{"codex"}}}})
		writeCodexBoundaryLaunch(t, root, "retry", socket, "w1:p1", home, sourceTestExecutable(t), launchContext{PID: os.Getpid(), ProcessStart: start})
		if err := HandleForRoot(providerCodex, root, strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"`+oldID+`"}`), io.Discard, io.Discard, nil); err != nil {
			t.Fatalf("embedded switch retry must no-op: %v", err)
		}
		if len(reports) != 1 {
			t.Fatalf("embedded switch issued %d reports, want one", len(reports))
		}
	})

	t.Run("managed_probe_is_bounded_before_embedded_fallback", func(t *testing.T) {
		t.Setenv("AL_TEST_CODEX_INITIALIZE_DELAY_MS", "4000")
		root := t.TempDir()
		home, controlSocket := newCodexBoundaryControlSocket(t, root)
		_ = startCodexBoundaryServer(t, root, controlSocket, []string{"f1a1138f-7df0-7fa0-94ae-820334783a29"})
		_, start, err := processLineage(os.Getpid())
		if err != nil {
			t.Fatal(err)
		}
		writeCodexBoundaryLaunch(t, root, "bounded", filepath.Join(root, "missing-herdr.sock"), "w1:p1", home, sourceTestExecutable(t), launchContext{PID: os.Getpid(), ProcessStart: start})
		started := time.Now()
		err = HandleForRoot(providerCodex, root, strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"f1a1138f-7df0-7fa0-94ae-820334783a29"}`), io.Discard, io.Discard, nil)
		if err == nil || time.Since(started) > 3500*time.Millisecond {
			t.Fatalf("managed probe error/elapsed = %v/%s, want bounded failure", err, time.Since(started))
		}
	})

	t.Run("durable_recipe_with_newer_live_session_reports_selected_thread", func(t *testing.T) {
		root := t.TempDir()
		home, controlSocket := newCodexBoundaryControlSocket(t, root)
		selectedID := "f1a1138f-7df0-7fa0-94ae-820334783a28"
		liveID := "f1a1138f-7df0-7fa0-94ae-820334783a29"
		server := startCodexBoundaryServer(t, root, controlSocket, []string{selectedID, liveID})
		tui := startCodexBoundaryTUI(t)
		socket, reports, _ := fakeHerdR(t, fakeHerdROptions{requestsExpected: 30, workingDir: root, socketPath: "session/herdr.sock", codexPanes: []fakeCodexPane{{paneID: "w1:p1", title: "✳ " + selectedID + " | selected", liveSource: sourceCodexAL, liveAgent: providerCodex, liveKind: "id", liveValue: liveID, processPID: tui.PID, processArgv: []any{sourceTestExecutable(t)}}}})
		writeCodexBoundaryLaunch(t, root, "live-session-race", socket, "w1:p1", home, sourceTestExecutable(t), tui)
		argv := []string{commandEnv, EnvDevBypass + "=1", EnvDevExecutable + "=" + sourceTestExecutable(t), sourceTestExecutable(t), providerCodex, resumeVerb, selectedID}
		writeCodexStoredResume(t, root, "w1:p1", argv)
		result := server.invoke(t, boundaryHookInstruction{Root: root, Provider: providerCodex, Payload: `{"hook_event_name":"UserPromptSubmit","session_id":"` + selectedID + `"}`})
		if result.Err != "" || result.Out != "" {
			t.Fatalf("live-session race hook = %#v", result)
		}
		params := (<-reports)["params"].(map[string]any)
		if params["pane_id"] != "w1:p1" || !contains(interfaceStrings(params[requestResumeArgvKey]), selectedID) {
			t.Fatalf("live-session race reported %#v, want selected %s", params, selectedID)
		}
	})

	t.Run("matched_authority_or_api_failure_is_loud_and_durable", func(t *testing.T) {
		for _, tc := range []struct {
			name, authority, processError string
		}{
			{name: "foreign_authority", authority: "herdr:codex:other"},
			{name: "process_api_failure", processError: "unavailable"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				root := t.TempDir()
				home, controlSocket := newCodexBoundaryControlSocket(t, root)
				id := "71a1138f-7df0-7fa0-94ae-820334783a29"
				server := startCodexBoundaryServer(t, root, controlSocket, []string{id})
				tui := startCodexBoundaryTUI(t)
				socket, reports, _ := fakeHerdR(t, fakeHerdROptions{requestsExpected: 3, workingDir: root, socketPath: "session/herdr.sock", codexProcessError: tc.processError, codexPanes: []fakeCodexPane{{paneID: "w1:p1", title: "✳ " + id + " | selected", authority: tc.authority, processPID: tui.PID, processArgv: []any{sourceTestExecutable(t)}}}})
				writeCodexBoundaryLaunch(t, root, tc.name, socket, "w1:p1", home, sourceTestExecutable(t), tui)
				result := server.invoke(t, boundaryHookInstruction{Root: root, Provider: providerCodex, Payload: `{"hook_event_name":"UserPromptSubmit","session_id":"` + id + `"}`})
				if result.Err == "" || result.Out != "" || len(reports) != 0 {
					t.Fatalf("matched failure result/report = %#v reports=%d", result, len(reports))
				}
				entries, err := os.ReadDir(filepath.Join(root, ".agent-layer", "tmp", "herdr-hook-errors"))
				if err != nil || len(entries) == 0 {
					t.Fatalf("durable hook diagnostic = %#v, %v", entries, err)
				}
			})
		}
	})
}

type boundaryHookInstruction struct {
	Root, Provider, Payload                          string
	Environ                                          []string
	PersistPollMilliseconds, PersistWaitMilliseconds int
}

type boundaryHookResult struct {
	Err, Out, ErrOut string
}

type codexBoundaryServer struct {
	input   io.WriteCloser
	output  *bufio.Scanner
	command *exec.Cmd
}

func startCodexBoundaryServer(t *testing.T, workingDir, socket string, loaded []string) *codexBoundaryServer {
	t.Helper()
	encoded, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestHandleForRootCodexNativeBoundary$") //nolint:gosec // test binary and arguments are test-controlled.
	command.Dir = workingDir
	command.Env = append(os.Environ(), "AL_TEST_CODEX_BOUNDARY_MODE=server", "AL_TEST_CODEX_BOUNDARY_SOCKET="+socket, "AL_TEST_CODEX_BOUNDARY_LOADED="+string(encoded))
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	server := &codexBoundaryServer{input: input, output: bufio.NewScanner(stdout), command: command}
	if !server.output.Scan() || server.output.Text() != "ready" {
		t.Fatalf("control server did not become ready: line=%q stderr=%s", server.output.Text(), stderr.String())
	}
	t.Cleanup(func() {
		_ = input.Close()
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	return server
}

func (server *codexBoundaryServer) invoke(t *testing.T, instruction boundaryHookInstruction) boundaryHookResult {
	t.Helper()
	data, err := json.Marshal(instruction)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.input.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
	if !server.output.Scan() {
		t.Fatalf("control server stopped before hook result: %v", server.output.Err())
	}
	var result boundaryHookResult
	if err := json.Unmarshal(server.output.Bytes(), &result); err != nil {
		t.Fatalf("decode hook result %q: %v", server.output.Text(), err)
	}
	return result
}

func runCodexBoundaryServer() {
	socket := os.Getenv("AL_TEST_CODEX_BOUNDARY_SOCKET")
	var loaded []string
	if err := json.Unmarshal([]byte(os.Getenv("AL_TEST_CODEX_BOUNDARY_LOADED")), &loaded); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	defer func() { _ = listener.Close() }()
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer func() { _ = connection.Close() }()
		for {
			_, message, err := connection.ReadMessage()
			if err != nil {
				return
			}
			var rpc map[string]any
			if json.Unmarshal(message, &rpc) != nil {
				return
			}
			id, hasID := rpc["id"]
			if !hasID {
				continue
			}
			var result map[string]any
			switch rpc["method"] {
			case "initialize":
				if delay, _ := strconv.Atoi(os.Getenv("AL_TEST_CODEX_INITIALIZE_DELAY_MS")); delay > 0 {
					time.Sleep(time.Duration(delay) * time.Millisecond)
				}
				result = map[string]any{"serverInfo": map[string]any{"name": "boundary-fixture"}}
			case "thread/loaded/list":
				data := make([]any, len(loaded))
				for index := range loaded {
					data[index] = loaded[index]
				}
				result = map[string]any{"data": data}
			default:
				return
			}
			if connection.WriteJSON(map[string]any{"id": id, "result": result}) != nil {
				return
			}
		}
	})}
	go func() { _ = server.Serve(listener) }()
	fmt.Println("ready")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var instruction boundaryHookInstruction
		if err := json.Unmarshal(scanner.Bytes(), &instruction); err != nil {
			fmt.Printf(`{"Err":%q}`+"\n", err.Error())
			continue
		}
		result := runCodexBoundaryHookProcess(instruction)
		data, _ := json.Marshal(result)
		fmt.Println(string(data))
	}
}

func runCodexBoundaryHookMode() {
	var instruction boundaryHookInstruction
	if err := json.NewDecoder(os.Stdin).Decode(&instruction); err != nil {
		fmt.Printf(`{"Err":%q}`+"\n", err.Error())
		return
	}
	data, _ := json.Marshal(runCodexBoundaryHookProcessLocal(instruction))
	fmt.Println(string(data))
}

func runCodexBoundaryHookProcess(instruction boundaryHookInstruction) boundaryHookResult {
	command := exec.Command(os.Args[0], "-test.run=^TestHandleForRootCodexNativeBoundary$") //nolint:gosec // test binary and arguments are test-controlled.
	command.Dir = instruction.Root
	command.Env = append(os.Environ(), "AL_TEST_CODEX_BOUNDARY_MODE=hook")
	data, _ := json.Marshal(instruction)
	command.Stdin = bytes.NewReader(data)
	output, err := command.CombinedOutput()
	if err != nil {
		return boundaryHookResult{Err: fmt.Sprintf("hook helper failed: %v: %s", err, output)}
	}
	var result boundaryHookResult
	if err := json.Unmarshal(bytes.TrimSpace(output), &result); err != nil {
		return boundaryHookResult{Err: fmt.Sprintf("decode hook helper output %q: %v", output, err)}
	}
	return result
}

func runCodexBoundaryHookProcessLocal(instruction boundaryHookInstruction) boundaryHookResult {
	oldPoll, oldWait := persistPollEvery, persistWait
	if instruction.PersistPollMilliseconds > 0 {
		persistPollEvery = time.Duration(instruction.PersistPollMilliseconds) * time.Millisecond
	}
	if instruction.PersistWaitMilliseconds > 0 {
		persistWait = time.Duration(instruction.PersistWaitMilliseconds) * time.Millisecond
	}
	defer func() { persistPollEvery, persistWait = oldPoll, oldWait }()
	var out, errOut bytes.Buffer
	err := HandleForRoot(instruction.Provider, instruction.Root, strings.NewReader(instruction.Payload), &out, &errOut, instruction.Environ)
	result := boundaryHookResult{Out: out.String(), ErrOut: errOut.String()}
	if err != nil {
		result.Err = err.Error()
	}
	return result
}

func runCodexBoundaryHook(t *testing.T, instruction boundaryHookInstruction) boundaryHookResult {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestHandleForRootCodexNativeBoundary$") //nolint:gosec // test binary and arguments are test-controlled.
	command.Dir = instruction.Root
	command.Env = append(os.Environ(), "AL_TEST_CODEX_BOUNDARY_MODE=hook")
	data, err := json.Marshal(instruction)
	if err != nil {
		t.Fatal(err)
	}
	command.Stdin = bytes.NewReader(data)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	var result boundaryHookResult
	if err := json.Unmarshal(bytes.TrimSpace(output), &result); err != nil {
		t.Fatalf("decode direct hook result %q: %v", output, err)
	}
	return result
}

func newCodexBoundaryControlSocket(t *testing.T, root string) (string, string) {
	t.Helper()
	home := filepath.Join(root, "home", ".codex")
	advertised := filepath.Join(home, codexControlSocket)
	if err := os.MkdirAll(filepath.Dir(advertised), 0o700); err != nil {
		t.Fatal(err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(advertised))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(filepath.Join(parent, filepath.Base(advertised))))
	// Native Codex keeps this rendezvous under the short system /tmp alias.
	// Do not use Go's per-user TMPDIR here: on Darwin its expanded path plus a
	// 64-character hash exceeds sockaddr_un before the hook can dial it.
	directory := filepath.Join(string(os.PathSeparator), "tmp", "codex-daemon-"+strconv.Itoa(os.Geteuid()))
	created := false
	if err := os.Mkdir(directory, 0o700); err == nil {
		created = true
	} else if !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm()&0o077 != 0 || int(stat.Uid) != os.Geteuid() {
		t.Fatalf("native rendezvous directory is not private and owned: %s %#o", directory, info.Mode().Perm())
	}
	physical := filepath.Join(directory, fmt.Sprintf("%x", digest))
	if _, err := os.Lstat(physical); err == nil {
		t.Fatalf("refusing to reuse an existing native control socket: %s", physical)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := os.Symlink(physical, advertised); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(advertised)
		_ = os.Remove(physical)
		if created {
			_ = os.Remove(directory)
		}
	})
	return home, physical
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

func writeCodexBoundaryLaunch(t *testing.T, root, name, socket, paneID, home, executable string, identity launchContext) launchContext {
	t.Helper()
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	context := launchContext{Version: 2, PID: identity.PID, ProcessStart: identity.ProcessStart, ProjectRoot: canonicalRoot, Provider: providerCodex, SocketPath: socket, LaunchCWD: canonicalRoot, PaneID: paneID, CodexHome: home, DevBypass: true, DevExecutable: executable}
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
	if context.Provider != providerCodex {
		return
	}
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(canonicalRoot, ".agent-layer", "tmp", "runs", name, launchContextName(context.PID, context.ProcessStart))
	if err := rememberCodexLiveLaunch(canonicalRoot, recordPath); err != nil {
		t.Fatal(err)
	}
}

func assertBoundaryStored(t *testing.T, root, pane string, argv []string) {
	t.Helper()
	path := filepath.Join(root, "session", "session.json")
	stored, err := storedResumeAt(path, pane, providers[providerCodex], argv)
	if err != nil || !stored {
		t.Fatalf("canonical persisted command for %s = %t, %v", pane, stored, err)
	}
}

func writeCodexStoredResume(t *testing.T, root, pane string, argv []string) {
	t.Helper()
	workspace, number, err := splitCanonicalPaneID(pane)
	if err != nil {
		t.Fatal(err)
	}
	internal := strconv.Itoa(number)
	document := map[string]any{
		"workspaces": []any{map[string]any{
			"id":                  workspace,
			"public_pane_numbers": map[string]any{internal: number},
			"tabs": []any{map[string]any{
				"panes": map[string]any{internal: map[string]any{
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

func TestLiveCodexLaunchRecordsSkipsConcurrentlyDeletedIndex(t *testing.T) {
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
	deleted := launchContext{Version: 2, PID: 888888, ProcessStart: "concurrent-delete", ProjectRoot: canonicalRoot, Provider: providerCodex, SocketPath: "/tmp/herdr.sock", PaneID: "w1:p2", CodexHome: filepath.Join(root, ".codex")}
	deletedRecord := filepath.Join(runDir, launchContextName(deleted.PID, deleted.ProcessStart))
	writeLaunchContextFixture(t, deletedRecord, deleted)
	if err := rememberCodexLiveLaunch(canonicalRoot, deletedRecord); err != nil {
		t.Fatal(err)
	}
	liveDir := filepath.Join(canonicalRoot, ".agent-layer", "tmp", codexLiveLaunchIndexDir)
	deletedIndex := filepath.Join(liveDir, filepath.Base(deletedRecord))
	original := readDirFunc
	t.Cleanup(func() { readDirFunc = original })
	readDirFunc = func(path string) ([]os.DirEntry, error) {
		entries, err := original(path)
		if path == liveDir {
			if removeErr := os.Remove(deletedIndex); removeErr != nil {
				t.Fatalf("delete index concurrently: %v", removeErr)
			}
		}
		return entries, err
	}
	records, err := liveCodexLaunchRecords(root)
	if err != nil {
		t.Fatalf("concurrent index deletion failed the scan: %v", err)
	}
	if len(records) != 1 || records[0].PaneID != "w1:p1" {
		t.Fatalf("records after concurrent deletion = %#v", records)
	}
}
