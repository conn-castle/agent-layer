package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/agentdispatch"
)

func TestDispatchCLISessionMCPValidationAndOptions(t *testing.T) {
	root := newReservationTestRepo(t, "")
	mcpDir := t.TempDir()

	// 1. `al dispatch options` reports per-agent session_mcp_servers support.
	cmd := newDispatchCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"options"})
	t.Setenv("AL_NO_NETWORK", "1")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("al dispatch options: %v (stderr=%s)", err, stderr.String())
	}
	var options agentdispatch.OptionsResponse
	if err := json.Unmarshal(stdout.Bytes(), &options); err != nil {
		t.Fatalf("decode options JSON %q: %v", stdout.String(), err)
	}
	support := make(map[string]bool, len(options.Agents))
	for _, agent := range options.Agents {
		support[agent.Agent] = agent.SessionMCPServers.Supported
	}
	for agent, want := range map[string]bool{
		agentdispatch.AgentCodex:       true,
		agentdispatch.AgentClaude:      true,
		agentdispatch.AgentMuse:        true,
		agentdispatch.AgentAntigravity: true,
		agentdispatch.AgentGrok:        true,
	} {
		if support[agent] != want {
			t.Errorf("options[%s].session_mcp_servers.supported = %t, want %t", agent, support[agent], want)
		}
	}

	// 2. Empty --mcp-config fails before launch with ExitUsage.
	_, stderrText, code := runDispatchCLI(t, "start", "--agent", "codex", "--mcp-config", "", "--prompt", "hi")
	if code != agentdispatch.ExitUsage {
		t.Fatalf("empty --mcp-config exit %d, want %d: %s", code, agentdispatch.ExitUsage, stderrText)
	}

	// 3. Collision with reserved `agent-layer` fails before launch with ExitUsage on all harnesses (including ACP ones).
	colliding := filepath.Join(mcpDir, "collide.json")
	if err := os.WriteFile(colliding, []byte(`{"mcpServers":{"agent-layer":{"command":"/bin/true"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{"codex", "grok", "antigravity"} {
		_, stderrText, code = runDispatchCLI(t, "start", "--agent", agent, "--mcp-config", colliding, "--prompt", "hi")
		if code != agentdispatch.ExitUsage || !strings.Contains(stderrText, "agent-layer") {
			t.Fatalf("%s colliding --mcp-config exit %d stderr=%q", agent, code, stderrText)
		}
	}

	// 5. Verify repo working tree was not polluted.
	if _, err := os.Stat(filepath.Join(root, ".mcp.json")); err == nil {
		data, _ := os.ReadFile(filepath.Join(root, ".mcp.json")) // #nosec G304 -- test repository .mcp.json path.
		if strings.Contains(string(data), `"wf"`) {
			t.Fatalf(".mcp.json was modified with session MCP server")
		}
	}
}

func TestDispatchSessionMCPWrapperEndToEndWithBuiltBinary(t *testing.T) {
	tmp := t.TempDir()
	alBin := filepath.Join(tmp, "al")
	build := exec.Command("go", "build", "-o", alBin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build al binary: %v\n%s", err, out)
	}

	// Create a fake stdio MCP server inside customBin (not on PATH) that records
	// cwd, env, and argv then exits 0.
	customBin := filepath.Join(tmp, "custom-bin")
	workDir := filepath.Join(tmp, "workdir")
	runDir := filepath.Join(tmp, "rundir")
	outFile := filepath.Join(tmp, "observed.txt")
	for _, dir := range []string{customBin, workDir, runDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	serverScript := filepath.Join(customBin, "wf-server")
	script := "#!/bin/sh\n" +
		"printf 'CWD=%s\\n' \"$PWD\" > " + outFile + "\n" +
		"printf 'PATH=%s\\n' \"$PATH\" >> " + outFile + "\n" +
		"printf 'MARKER=%s\\n' \"$WF_MARKER\" >> " + outFile + "\n" +
		"printf 'ARGS=%s\\n' \"$*\" >> " + outFile + "\n"
	if err := os.WriteFile(serverScript, []byte(script), 0o700); err != nil { // #nosec G306 -- synthetic executable test script.
		t.Fatal(err)
	}

	spec := map[string]any{
		"work_dir": workDir,
		"base_env": []string{"AMBIENT_MUSE_SURVIVOR=ok"},
		"servers": []map[string]any{{
			"name":    "wf",
			"command": "wf-server",
			"args":    []string{"--step", "${LITERAL}"},
			"env": map[string]string{
				"PATH":      customBin + ":/usr/bin:/bin",
				"WF_MARKER": "val-${LITERAL}",
			},
		}},
	}
	specBytes, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(tmp, "private-spec.json")
	if err := os.WriteFile(specPath, specBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	specSum := sha256.Sum256(specBytes)
	specDigest := hex.EncodeToString(specSum[:])

	run := exec.Command(alBin, "__dispatch-session-mcp", "--run-dir", runDir, "--server", "wf", "--spec", specPath, "--digest", specDigest)
	run.Dir = tmp
	// Simulate Muse scrubbing the wrapper's inherited environment down to minimal allowlist.
	run.Env = []string{"PATH=/usr/bin:/bin"}
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("al __dispatch-session-mcp failed: %v\n%s", err, out)
	}
	observed, err := os.ReadFile(outFile) // #nosec G304 -- test temp output file.
	if err != nil {
		t.Fatalf("read observed output: %v", err)
	}
	text := string(observed)
	realWorkDir, _ := filepath.EvalSymlinks(workDir)
	for _, want := range []string{
		"CWD=" + realWorkDir + "\n",
		"PATH=" + customBin + ":/usr/bin:/bin\n",
		"MARKER=val-${LITERAL}\n",
		"ARGS=--step ${LITERAL}\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("observed wrapper execution:\n%s\nmissing %q", text, want)
		}
	}

	// Verify --verify-handshake records initialized+tools_listed for a valid MCP
	// server and failed=true when a server exits before completing handshake.
	hsRunDir := filepath.Join(tmp, "hs-run")
	if err := os.MkdirAll(hsRunDir, 0o700); err != nil {
		t.Fatal(err)
	}
	mcpOK := filepath.Join(customBin, "mcp-ok")
	mcpOKScript := "#!/bin/sh\nwhile IFS= read -r line; do\n" +
		"  id=\"$(printf '%s\\n' \"$line\" | sed -n 's/.*\"id\":\\([0-9][0-9]*\\).*/\\1/p')\"\n" +
		"  case \"$line\" in\n" +
		"    *'\"method\":\"initialize\"'*) printf '{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"protocolVersion\":\"2024-11-05\"}}\\n' \"$id\" ;;\n" +
		"    *'\"method\":\"tools/list\"'*) printf '{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"tools\":[]}}\\n' \"$id\" ;;\n" +
		"  esac\n" +
		"done\n"
	if err := os.WriteFile(mcpOK, []byte(mcpOKScript), 0o700); err != nil { // #nosec G306 -- synthetic executable test fixture.
		t.Fatal(err)
	}
	mcpNoisy := filepath.Join(customBin, "mcp-noisy")
	mcpNoisyScript := "#!/bin/sh\ni=0\nwhile [ $i -lt 5000 ]; do\n  printf '{\"jsonrpc\":\"2.0\",\"method\":\"noise\",\"params\":{\"pad\":\"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\"}}\\n'\n  i=$((i + 1))\ndone\n"
	if err := os.WriteFile(mcpNoisy, []byte(mcpNoisyScript), 0o700); err != nil { // #nosec G306 -- synthetic executable test fixture.
		t.Fatal(err)
	}
	hsSpec := map[string]any{
		"work_dir": workDir,
		"servers": []map[string]any{
			{"name": "ok-srv", "command": mcpOK},
			{"name": "bad-srv", "command": "/bin/false"},
			{"name": "noisy-srv", "command": mcpNoisy},
		},
	}
	hsBytes, err := json.Marshal(hsSpec)
	if err != nil {
		t.Fatal(err)
	}
	hsSpecPath := filepath.Join(tmp, "private-hs-spec.json")
	if err := os.WriteFile(hsSpecPath, hsBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	hsSum := sha256.Sum256(hsBytes)
	hsDigest := hex.EncodeToString(hsSum[:])

	hsTmp := filepath.Join(tmp, "hs-tmp")
	if err := os.MkdirAll(hsTmp, 0o700); err != nil {
		t.Fatal(err)
	}
	okCmd := exec.Command(alBin, "__dispatch-session-mcp", "--run-dir", hsRunDir, "--server", "ok-srv", "--spec", hsSpecPath, "--digest", hsDigest, "--verify-handshake")
	okCmd.Env = append(os.Environ(), "TMPDIR="+hsTmp)
	okCmd.Stdin = strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{}}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\",\"params\":{}}\n")
	if out, err := okCmd.CombinedOutput(); err != nil {
		t.Fatalf("--verify-handshake ok-srv failed: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(hsRunDir, "session-mcp-handshake", "ok-srv.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("handshake status must not be written inside workspace runDir: %v", statErr)
	}
	okMatches, err := filepath.Glob(filepath.Join(hsTmp, "agent-layer-dispatch-*", "runs", "*", "session-mcp-handshake", "ok-srv.json"))
	if err != nil || len(okMatches) != 1 {
		t.Fatalf("locate ok-srv handshake status in private runtime dir: matches=%v err=%v", okMatches, err)
	}
	okStatusBytes, err := os.ReadFile(okMatches[0]) // #nosec G304 -- test handshake status file in private TMPDIR.
	if err != nil {
		t.Fatalf("read ok-srv handshake status: %v", err)
	}
	if !strings.Contains(string(okStatusBytes), `"initialized":true`) || !strings.Contains(string(okStatusBytes), `"tools_listed":true`) {
		t.Fatalf("ok-srv handshake status unexpected: %s", okStatusBytes)
	}

	badCmd := exec.Command(alBin, "__dispatch-session-mcp", "--run-dir", hsRunDir, "--server", "bad-srv", "--spec", hsSpecPath, "--digest", hsDigest, "--verify-handshake")
	badCmd.Env = append(os.Environ(), "TMPDIR="+hsTmp)
	badCmd.Stdin = strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{}}\n")
	_ = badCmd.Run()
	badMatches, err := filepath.Glob(filepath.Join(hsTmp, "agent-layer-dispatch-*", "runs", "*", "session-mcp-handshake", "bad-srv.json"))
	if err != nil || len(badMatches) != 1 {
		t.Fatalf("locate bad-srv handshake status in private runtime dir: matches=%v err=%v", badMatches, err)
	}
	badStatusBytes, err := os.ReadFile(badMatches[0]) // #nosec G304 -- test handshake status file in private TMPDIR.
	if err != nil {
		t.Fatalf("read bad-srv handshake status: %v", err)
	}
	if !strings.Contains(string(badStatusBytes), `"failed":true`) {
		t.Fatalf("bad-srv handshake status did not record failed=true: %s", badStatusBytes)
	}

	// Verify closing wrapper stdout while child keeps writing never deadlocks Wait.
	noisyCmd := exec.Command(alBin, "__dispatch-session-mcp", "--run-dir", hsRunDir, "--server", "noisy-srv", "--spec", hsSpecPath, "--digest", hsDigest, "--verify-handshake")
	noisyCmd.Env = append(os.Environ(), "TMPDIR="+hsTmp)
	noisyOut, err := noisyCmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := noisyCmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = noisyOut.Close()
	noisyDone := make(chan error, 1)
	go func() { noisyDone <- noisyCmd.Wait() }()
	select {
	case <-noisyDone:
	case <-time.After(10 * time.Second):
		_ = noisyCmd.Process.Kill()
		t.Fatal("handshake wrapper deadlocked in Wait after stdout closed (EPIPE)")
	}
}

func TestDispatchSessionMCPFullLifecycleParallelAndCancel(t *testing.T) {
	tmp := t.TempDir()
	alBin := filepath.Join(tmp, "al")
	build := exec.Command("go", "build", "-o", alBin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build al binary: %v\n%s", err, out)
	}

	root := newReservationTestRepo(t, "")
	fakeBin := filepath.Join(tmp, "fake-bin")
	customBin := filepath.Join(tmp, "custom-bin")
	logsDir := filepath.Join(tmp, "logs")
	for _, dir := range []string{fakeBin, customBin, logsDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	// Fake session MCP server in customBin (NOT on outer PATH): records its
	// runtime context or sleeps until killed when asked to linger.
	mcpScript := `#!/bin/sh
set -eu
out="$1"
mode="${2:-once}"
printf 'TOKEN=%s CWD=%s PATH=%s ARGS=%s\n' "${WF_TOKEN:-}" "$PWD" "$PATH" "$*" >> "$out"
if [ "$mode" = "linger" ]; then
  printf '%s\n' "$$" > "${out}.pid"
  exec sleep 60
fi
`
	if err := os.WriteFile(filepath.Join(customBin, "wf-mcp"), []byte(mcpScript), 0o700); err != nil { // #nosec G306 -- synthetic executable test script.
		t.Fatal(err)
	}

	// Fake codex CLI: reports supported version, logs exact -c mcp_servers.*
	// flags, invokes every staged `al __dispatch-session-mcp` command it was
	// passed, and emits valid Codex JSONL events.
	codexScript := `#!/bin/sh
set -eu
if [ "${1:-}" = "--version" ]; then
  echo "codex-cli 0.172.0"
  exit 0
fi
prompt="$(cat)"
args_log="${FAKE_CODEX_LOG_DIR}/args.log"
mcp_cmds=""
is_resume=0
thread_id="thread-e2e-$$"
prev=""
for arg in "$@"; do
  if [ "$prev" = "-c" ]; then
    case "$arg" in
      mcp_servers.*)
        printf '%s\t%s\n' "$prompt" "$arg" >> "$args_log"
        # Extract command and args from:
        # mcp_servers.<name>={command="...",args=["__dispatch-session-mcp","--run-dir","...","--server","..."],...}
        cmd_path="$(printf '%s' "$arg" | sed -n 's/.*command="\([^"]*\)".*/\1/p')"
        run_dir="$(printf '%s' "$arg" | sed -n 's/.*"--run-dir","\([^"]*\)".*/\1/p')"
        srv_name="$(printf '%s' "$arg" | sed -n 's/.*"--server","\([^"]*\)".*/\1/p')"
        spec_path="$(printf '%s' "$arg" | sed -n 's/.*"--spec","\([^"]*\)".*/\1/p')"
        digest="$(printf '%s' "$arg" | sed -n 's/.*"--digest","\([^"]*\)".*/\1/p')"
        if [ -n "$cmd_path" ] && [ -n "$run_dir" ] && [ -n "$srv_name" ] && [ -n "$spec_path" ] && [ -n "$digest" ]; then
          if [ "$prompt" = "hang-mcp" ]; then
            "$cmd_path" __dispatch-session-mcp --run-dir "$run_dir" --server "$srv_name" --spec "$spec_path" --digest "$digest" </dev/null >/dev/null 2>&1 &
          else
            "$cmd_path" __dispatch-session-mcp --run-dir "$run_dir" --server "$srv_name" --spec "$spec_path" --digest "$digest"
          fi
        fi
        ;;
    esac
    prev=""
    continue
  fi
  case "$arg" in
    -c) prev="$arg" ;;
    resume) is_resume=1 ;;
    thread-e2e-*) thread_id="$arg" ;;
  esac
done

printf '{"type":"thread.started","thread_id":"%s"}\n' "$thread_id"
if [ "$prompt" = "fail-once" ]; then
  printf '{"type":"turn.failed","error":{"message":"simulated failure"}}\n'
  exit 1
fi
if [ "$prompt" = "hang-mcp" ]; then
  sleep 60
  exit 0
fi
printf '{"type":"item.completed","item":{"type":"agent_message","text":"ok:%s"}}\n' "$prompt"
printf '{"type":"turn.completed"}\n'
`
	if err := os.WriteFile(filepath.Join(fakeBin, "codex"), []byte(codexScript), 0o700); err != nil { // #nosec G306 -- synthetic executable test script.
		t.Fatal(err)
	}

	writeMCPConfig := func(filename, token, outPath, mode string) string {
		cfgPath := filepath.Join(tmp, filename)
		payload := map[string]any{
			"mcpServers": map[string]any{
				"wf-b": map[string]any{
					"command": "wf-mcp",
					"args":    []string{outPath, mode, "b"},
					"env": map[string]string{
						"PATH":     customBin + ":/usr/bin:/bin",
						"WF_TOKEN": token + "-b-${LITERAL}",
					},
				},
				"wf-a": map[string]any{
					"command": "wf-mcp",
					"args":    []string{outPath, mode, "a"},
					"env": map[string]string{
						"PATH":     customBin + ":/usr/bin:/bin",
						"WF_TOKEN": token + "-a-${LITERAL}",
					},
				},
			},
		}
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return cfgPath
	}

	runAl := func(args ...string) (string, string, int) {
		cmd := exec.Command(alBin, args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"PATH="+fakeBin+":/usr/bin:/bin",
			"AL_NO_NETWORK=1",
			"FAKE_CODEX_LOG_DIR="+logsDir,
		)
		var outBuf, errBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		err := cmd.Run()
		code := 0
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				code = exitErr.ExitCode()
			} else {
				t.Fatalf("exec al %v: %v", args, err)
			}
		}
		return outBuf.String(), errBuf.String(), code
	}

	mustJSON := func(raw string, label string) dispatchCLIResult {
		var res dispatchCLIResult
		if err := json.Unmarshal([]byte(raw), &res); err != nil {
			t.Fatalf("%s invalid JSON %q: %v", label, raw, err)
		}
		return res
	}

	// 1. Start -> Wait -> Continue -> Continue-after-failure -> Continue-after-cancel.
	obsMain := filepath.Join(logsDir, "main.obs")
	cfgMain := writeMCPConfig("main.json", "main", obsMain, "once")

	out, stderr, code := runAl("dispatch", "start", "--agent", "codex", "--mcp-config", cfgMain, "--prompt", "start")
	if code != 0 {
		t.Fatalf("start failed (%d): stdout=%s stderr=%s", code, out, stderr)
	}
	started := mustJSON(out, "start")
	if started.Handle == "" {
		t.Fatalf("expected non-empty handle: %#v", started)
	}
	if out, stderr, code = runAl("dispatch", "wait", "--condition", "termination_confirmed", started.Handle); code != 0 {
		pStderr, _ := os.ReadFile(filepath.Join(root, ".agent-layer", "state", "runs", started.InvocationID, "provider.stderr")) // #nosec G304 -- test run diagnostic log.
		t.Fatalf("wait start failed (%d): stdout=%s stderr=%s provider_stderr=%q", code, out, stderr, string(pStderr))
	}
	if waited := mustJSON(out, "wait start"); waited.State != "completed" || !waited.TerminationConfirmed {
		t.Fatalf("expected completed+confirmed start: %#v", waited)
	}

	// Continue without re-supplying --mcp-config.
	if out, stderr, code = runAl("dispatch", "continue", started.Handle, "--prompt", "continue-ok"); code != 0 {
		t.Fatalf("continue-ok failed (%d): stdout=%s stderr=%s", code, out, stderr)
	}
	if out, stderr, code = runAl("dispatch", "wait", "--condition", "termination_confirmed", started.Handle); code != 0 {
		t.Fatalf("wait continue-ok failed (%d): stdout=%s stderr=%s", code, out, stderr)
	}
	if waited := mustJSON(out, "wait continue-ok"); waited.State != "completed" || !waited.TerminationConfirmed {
		t.Fatalf("expected completed+confirmed continue-ok: %#v", waited)
	}

	// Continue that fails, then continue again — session MCP servers must persist.
	if out, stderr, code = runAl("dispatch", "continue", started.Handle, "--prompt", "fail-once"); code != 0 {
		t.Fatalf("continue fail-once launch failed (%d): stdout=%s stderr=%s", code, out, stderr)
	}
	if out, stderr, code = runAl("dispatch", "wait", "--condition", "termination_confirmed", started.Handle); code != 0 {
		t.Fatalf("wait fail-once failed (%d): stdout=%s stderr=%s", code, out, stderr)
	}
	if waited := mustJSON(out, "wait fail-once"); waited.State != "failed" || !waited.TerminationConfirmed {
		t.Fatalf("expected failed+confirmed fail-once: %#v", waited)
	}
	if out, stderr, code = runAl("dispatch", "continue", started.Handle, "--prompt", "after-fail"); code != 0 {
		t.Fatalf("continue after-fail failed (%d): stdout=%s stderr=%s", code, out, stderr)
	}
	if out, stderr, code = runAl("dispatch", "wait", "--condition", "termination_confirmed", started.Handle); code != 0 {
		t.Fatalf("wait after-fail failed (%d): stdout=%s stderr=%s", code, out, stderr)
	}
	if waited := mustJSON(out, "wait after-fail"); waited.State != "completed" || !waited.TerminationConfirmed {
		t.Fatalf("expected completed+confirmed after-fail: %#v", waited)
	}

	// Continue that gets cancelled, then continue again — session MCP servers must persist.
	if out, stderr, code = runAl("dispatch", "continue", started.Handle, "--prompt", "hang-mcp"); code != 0 {
		t.Fatalf("continue hang-mcp failed (%d): stdout=%s stderr=%s", code, out, stderr)
	}
	if out, stderr, code = runAl("dispatch", "cancel", started.Handle); code != 0 {
		t.Fatalf("cancel started handle failed (%d): stdout=%s stderr=%s", code, out, stderr)
	}
	if out, stderr, code = runAl("dispatch", "continue", started.Handle, "--prompt", "after-cancel"); code != 0 {
		t.Fatalf("continue after-cancel failed (%d): stdout=%s stderr=%s", code, out, stderr)
	}
	if out, stderr, code = runAl("dispatch", "wait", "--condition", "termination_confirmed", started.Handle); code != 0 {
		t.Fatalf("wait after-cancel failed (%d): stdout=%s stderr=%s", code, out, stderr)
	}
	if waited := mustJSON(out, "wait after-cancel"); waited.State != "completed" || !waited.TerminationConfirmed {
		t.Fatalf("expected completed+confirmed after-cancel: %#v", waited)
	}

	// Verify deterministic ordering (wf-a before wf-b) and identical specs across invocations.
	argsBytes, err := os.ReadFile(filepath.Join(logsDir, "args.log")) // #nosec G304 -- test log file.
	if err != nil {
		t.Fatalf("read args.log: %v", err)
	}
	var perPrompt = map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(argsBytes)), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 {
			perPrompt[parts[0]] = append(perPrompt[parts[0]], parts[1])
		}
	}
	for _, prompt := range []string{"start", "continue-ok", "fail-once", "after-fail", "after-cancel"} {
		got := perPrompt[prompt]
		if len(got) != 2 {
			t.Fatalf("prompt %q received %d mcp_servers flags, want 2: %v", prompt, len(got), got)
		}
		if !strings.HasPrefix(got[0], "mcp_servers.wf-a=") || !strings.HasPrefix(got[1], "mcp_servers.wf-b=") {
			t.Fatalf("prompt %q server order not deterministic (wf-a then wf-b): %v", prompt, got)
		}
	}
	obsMainData, err := os.ReadFile(obsMain) // #nosec G304 -- test observation log.
	if err != nil {
		t.Fatalf("read main.obs: %v", err)
	}
	realRoot, _ := filepath.EvalSymlinks(root)
	if !strings.Contains(string(obsMainData), "TOKEN=main-a-${LITERAL} CWD="+realRoot+" ") {
		t.Fatalf("main.obs missing expected token/cwd:\n%s", string(obsMainData))
	}

	// 2. Parallel isolation: two concurrent conversations in the same checkout.
	obsParA := filepath.Join(logsDir, "par-a.obs")
	obsParB := filepath.Join(logsDir, "par-b.obs")
	cfgParA := writeMCPConfig("par-a.json", "parA", obsParA, "once")
	cfgParB := writeMCPConfig("par-b.json", "parB", obsParB, "once")

	outA, stderrA, codeA := runAl("dispatch", "start", "--agent", "codex", "--mcp-config", cfgParA, "--prompt", "parallel-a")
	outB, stderrB, codeB := runAl("dispatch", "start", "--agent", "codex", "--mcp-config", cfgParB, "--prompt", "parallel-b")
	if codeA != 0 || codeB != 0 {
		t.Fatalf("parallel starts failed: A=(%d %s) B=(%d %s)", codeA, stderrA, codeB, stderrB)
	}
	resA := mustJSON(outA, "parallel-a")
	resB := mustJSON(outB, "parallel-b")
	if out, stderr, code = runAl("dispatch", "wait", resA.Handle); code != 0 {
		t.Fatalf("wait parallel-a failed (%d): %s %s", code, out, stderr)
	}
	if out, stderr, code = runAl("dispatch", "wait", resB.Handle); code != 0 {
		t.Fatalf("wait parallel-b failed (%d): %s %s", code, out, stderr)
	}
	dataA, err := os.ReadFile(obsParA) // #nosec G304 -- test observation log.
	if err != nil {
		t.Fatal(err)
	}
	dataB, err := os.ReadFile(obsParB) // #nosec G304 -- test observation log.
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dataA), "TOKEN=parA-a-") || strings.Contains(string(dataA), "parB") {
		t.Fatalf("parallel-a isolation violated:\n%s", string(dataA))
	}
	if !strings.Contains(string(dataB), "TOKEN=parB-a-") || strings.Contains(string(dataB), "parA") {
		t.Fatalf("parallel-b isolation violated:\n%s", string(dataB))
	}

	// 3. Real Cancel kills lingering session MCP process group before termination_confirmed,
	// and conversation remains continuable afterwards with the same session MCP servers.
	obsCancel := filepath.Join(logsDir, "cancel.obs")
	cfgCancel := writeMCPConfig("cancel.json", "cancelTok", obsCancel, "linger")
	outC, stderrC, codeC := runAl("dispatch", "start", "--agent", "codex", "--mcp-config", cfgCancel, "--prompt", "hang-mcp")
	if codeC != 0 {
		t.Fatalf("start hang-mcp failed (%d): %s %s", codeC, outC, stderrC)
	}
	resC := mustJSON(outC, "hang-mcp")
	pidFile := obsCancel + ".pid"
	var mcpPID int
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if raw, readErr := os.ReadFile(pidFile); readErr == nil { // #nosec G304 -- test PID file.
			if parsed, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil && parsed > 0 {
				mcpPID = parsed
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if mcpPID == 0 {
		t.Fatalf("lingering session MCP server did not write PID file %s within timeout", pidFile)
	}

	cancelOut, cancelStderr, cancelCode := runAl("dispatch", "cancel", resC.Handle)
	if cancelCode != 0 {
		t.Fatalf("cancel failed (%d): stdout=%s stderr=%s", cancelCode, cancelOut, cancelStderr)
	}
	cancelled := mustJSON(cancelOut, "cancel")
	if !cancelled.TerminationConfirmed {
		t.Fatalf("expected termination_confirmed=true after cancel: %#v", cancelled)
	}
	// Session MCP server process group must already be dead when termination_confirmed is reported.
	if err := syscall.Kill(-mcpPID, 0); err != syscall.ESRCH {
		t.Fatalf("session MCP process group %d still alive after termination_confirmed: %v", mcpPID, err)
	}
	// Verify repo working tree has no session MCP servers written to .mcp.json.
	if data, err := os.ReadFile(filepath.Join(root, ".mcp.json")); err == nil && strings.Contains(string(data), "wf-a") { // #nosec G304 -- test repository .mcp.json path.
		t.Fatalf(".mcp.json was polluted with session MCP servers: %s", string(data))
	}
}
