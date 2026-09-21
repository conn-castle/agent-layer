#!/usr/bin/env python3
"""Pinned loopback regressions for Muse MCP child selectors and native user input.

Uses only isolated synthetic homes and credentials. Run alongside test-muse-native.py.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import http.server

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("native_fixture", ROOT / "scripts/test-muse-native.py")
f = importlib.util.module_from_spec(spec)
spec.loader.exec_module(f)
KEYS = ("XDG_CONFIG_HOME", "XDG_DATA_HOME", "GH_CONFIG_DIR")


def child(log, tag):
    selectors = {key: os.environ.get(key) for key in KEYS}
    gh = subprocess.run(["gh", "config", "get", "fixture_selector"], text=True, capture_output=True, check=True).stdout.strip()
    with Path(log).open("a") as output:
        output.write(json.dumps({"tag": tag, "selectors": selectors, "gh": gh}) + "\n")
    for line in sys.stdin:
        request = json.loads(line)
        if "id" not in request:
            continue
        if request["method"] == "tools/call" and tag == "shared":
            start = subprocess.run(["al", "dispatch", "start", "--agent", "muse", "--model", "fixture-model", "--prompt", "Nested MCP child fixture"], capture_output=True, text=True)
            Path(log + ".start-debug.json").write_text(json.dumps({"cwd": os.getcwd(), "stdout": start.stdout, "stderr": start.stderr, "exit": start.returncode}, indent=2))
            start.check_returncode()
            started = json.loads(start.stdout)
            invocation = started["invocation_id"]
            try:
                result = json.loads(subprocess.run(["al", "dispatch", "wait", invocation, "--condition", "termination_confirmed"], capture_output=True, text=True, check=True, timeout=90).stdout)
                f.require(result.get("state") == "completed" and result.get("termination_confirmed"), str(result))
                Path(log + ".nested.json").write_text(json.dumps({"start": started, "wait": result}, indent=2))
            finally:
                subprocess.run(["al", "dispatch", "cancel", invocation], capture_output=True, check=False)
        print(json.dumps(f.rpc_response(request, tag)), flush=True)


def exercise(args, muse, label):
    case = Path(tempfile.mkdtemp(prefix=label + "-", dir=args.output))
    home, work, bin_dir = (case / name for name in ("home", "workspace", "bin"))
    for path in (home, work, bin_dir):
        path.mkdir()
    (bin_dir / "al").symlink_to(args.al)
    (bin_dir / "muse").symlink_to(muse)
    env = {"HOME": str(home), "PATH": str(bin_dir) + os.pathsep + os.environ["PATH"],
           "TBH_DISABLE_TELEMETRY": "1", "TBH_CREDENTIAL_BACKEND": "file", "META_API_KEY": "synthetic"}
    if label == "empty":
        env.update({key: "" for key in KEYS})
    elif label == "custom":
        env.update({key: str(case / key.lower()) for key in KEYS})
    config = Path(env.get("XDG_CONFIG_HOME") or home / ".config")
    data = Path(env.get("XDG_DATA_HOME") or home / ".local/share")
    gh = Path(env.get("GH_CONFIG_DIR") or config / "gh")
    gh.mkdir(parents=True)
    (gh / "config.yml").write_text("fixture_selector: " + label + "\n")
    override = case / "override-gh"
    override.mkdir()
    (override / "config.yml").write_text("fixture_selector: explicit\n")
    native = config / "muse"
    native.mkdir(parents=True)
    state = {"requested": {"stdio", "http", "command"}, "catalog": set(), "http_calls": 0, "headers": []}
    api = http.server.ThreadingHTTPServer(("127.0.0.1", 0), f.Fixture)
    api.state = state
    threading.Thread(target=api.serve_forever, daemon=True).start()
    try:
        (native / "settings.json").write_text(json.dumps({"schema_version": 1, "endpoint_transport": {"base_url": f"http://127.0.0.1:{api.server_port}", "auth": "bearer"}}))
        f.run(["git", "-c", "init.templateDir=", "init", "--quiet"], work, env, case / "git-init")
        source = work / ".agent-layer"
        (source / "instructions").mkdir(parents=True)
        (source / "instructions/fixture.md").write_text("Loopback fixture\n")
        (source / "skills").mkdir()
        (source / "commands.allow").write_text("")
        (source / "gitignore.block").write_bytes((ROOT / "internal/templates/gitignore.block").read_bytes())
        lines = ['[approvals]', 'mode = "all"', '[warnings]', 'version_update_on_sync = false']
        for agent in ("claude", "claude_vscode", "codex", "antigravity", "grok", "copilot_cli", "vscode", "muse"):
            lines += [f"[agents.{agent}]", "enabled = " + str(agent in ("claude", "muse")).lower()]
        lines += ['[agents.claude.agent_specific]', 'enableAllProjectMcpServers = true']
        log = case / "children.jsonl"
        for tag in ("shared", "override"):
            lines += ['[[mcp.servers]]', f'id = "{tag}"', 'enabled = true', 'transport = "stdio"',
                      'clients = ["muse", "claude"]', 'command = ' + json.dumps(sys.executable),
                      'args = ' + json.dumps([str(Path(__file__).resolve()), "--child", str(log), tag])]
            if tag == "override":
                lines += ['env = { GH_CONFIG_DIR = ' + json.dumps(str(override)) + ' }']
        config_text = "\n".join(lines) + "\n"
        (source / "config.toml").write_text(config_text)
        f.register_workspace(args.al, work, env)
        f.run([str(args.al), "sync"], work, env, case / "sync")
        state["next_call"] = ("mcp__shared__probe_shared", {})
        command = [str(muse), "exec", "--workspace", str(work), "--trust-workspace", "--provider", "meta", "--model", "fixture-model", "--approval-mode", "untrusted", "--approval-judge", "off", "--json", "Native child fixture"]
        stdout = f.run(command, work, env, case / "muse", timeout=120)
        f.require(f.MARKER + ":shared" in stdout, "MCP child call failed")
        nested = json.loads(Path(str(log) + ".nested.json").read_text())
        invocation = nested["start"]["invocation_id"]
        record = json.loads((source / "tmp/runs" / invocation / "dispatch.json").read_text())
        (case / "nested-record.json").write_text(json.dumps(record, indent=2))
        f.require(list((data / "muse/sessions").glob("**/" + record["provider_session_id"] + "/session.jsonl")), "Nested dispatch used the wrong native data home")
        # Call the actual generated Agent Dispatch server as well as the
        # instrumented child: the worker must retain its native configuration.
        state["next_call"] = ("mcp__agent_layer__dispatch_start", {"agent": "muse", "model": "fixture-model", "prompt": "Builtin MCP child fixture"})
        def builtin_wait(request):
            if state.get("builtin_wait_requested"):
                return None
            for item in request.get("input", []):
                if not isinstance(item, dict) or item.get("type") != "function_call_output":
                    continue
                try:
                    value = json.loads(item.get("output", ""))
                except (ValueError, TypeError):
                    continue
                if isinstance(value, dict) and value.get("invocation_id"):
                    state["builtin_wait_requested"] = True
                    return ("mcp__agent_layer__dispatch_wait", {"invocation_id": value["invocation_id"], "condition": "termination_confirmed"})
            return None
        state["call_plan"] = builtin_wait
        builtin_env = {**env, "AL_RUN_ID": "native-fixture-parent", "AL_DISPATCH_ACTIVE": "0"}
        builtin_stdout = f.run(command, work, builtin_env, case / "builtin-mcp", timeout=90)
        state.pop("call_plan")
        f.require(state.get("builtin_wait_requested"), "Native caller never waited through builtin MCP")
        tool_results = [json.loads(line)["payload"] for line in builtin_stdout.splitlines()
                        if json.loads(line).get("payload_type") == "tool.result"]
        started = next((json.loads(row["text"]) for row in tool_results
                        if row.get("correlation_facts", {}).get("tool_name") == "mcp__agent_layer__dispatch_start"), None)
        f.require(started is not None, "Builtin dispatch_start result missing")
        builtin_id = started["invocation_id"]
        stopped = False
        try:
            result = json.loads(f.run([str(args.al), "dispatch", "wait", builtin_id, "--condition", "termination_confirmed"], work, env, case / "builtin-wait", timeout=90))
            stopped = result.get("termination_confirmed", False)
            f.require(stopped and result.get("state") == "completed", str(result))
        finally:
            if not stopped:
                f.run([str(args.al), "dispatch", "cancel", builtin_id], work, env, case / "builtin-cancel")
                proof = json.loads(f.run([str(args.al), "dispatch", "wait", builtin_id, "--condition", "termination_confirmed"], work, env, case / "builtin-stopped", timeout=90))
                f.require(proof.get("termination_confirmed"), "Builtin fixture termination unconfirmed")
        builtin_record = json.loads((source / "tmp/runs" / builtin_id / "dispatch.json").read_text())
        f.require(list((data / "muse/sessions").glob("**/" + builtin_record["provider_session_id"] + "/session.jsonl")), "Builtin dispatch used the wrong native data home")
        f.require(builtin_record.get("parent_run_id") == "native-fixture-parent", "Builtin dispatch lost parent run context")
        state["next_call"] = ("mcp__agent_layer__dispatch_start", {"agent": "muse", "model": "fixture-model", "prompt": "Must be blocked"})
        blocked = f.run(command, work, {**env, "AL_DISPATCH_ACTIVE": "99"}, case / "depth-blocked", timeout=60)
        f.require("nested dispatch is blocked at depth 99" in blocked, "Native MCP lost dispatch depth guard")
        if label == "custom":
            f.require(not (home / ".local/share/muse").exists(), "Nested Muse lost custom data home")
            f.require(not (home / ".config/muse").exists(), "Nested Muse lost custom config home")
        observed = [json.loads(line) for line in log.read_text().splitlines()]
        for row in observed:
            expected = {key: env.get(key, "") for key in KEYS}
            if row["tag"] == "override":
                expected["GH_CONFIG_DIR"] = str(override)
            f.require(row["selectors"] == expected, f"Muse child selectors: {row}")
            f.require(row["gh"] == ("explicit" if row["tag"] == "override" else label), f"gh fallback: {row}")
        if args.claude:
            claude_env = {**env, "CLAUDE_CONFIG_DIR": str(home / ".claude"), "ANTHROPIC_API_KEY": "synthetic", "ANTHROPIC_BASE_URL": f"http://127.0.0.1:{api.server_port}", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1", "DISABLE_AUTOUPDATER": "1"}
            claude_command = [args.claude, "-p", "Report tools", "--model", "claude-sonnet-4-6", "--setting-sources", "project", "--no-session-persistence", "--output-format", "stream-json", "--verbose", "--tools", "", "--max-turns", "1"]
            begin = len(observed)
            f.run(claude_command, work, claude_env, case / "claude-shared")
            shared = [json.loads(line) for line in log.read_text().splitlines()][begin:]
            f.require({r["tag"] for r in shared} == {"shared", "override"}, "Claude did not start shared servers")
            catalog = state["claude_catalogs"][-1]
            (source / "config.toml").write_text(config_text.replace("[agents.muse]\nenabled = true", "[agents.muse]\nenabled = false"))
            f.run([str(args.al), "sync"], work, env, case / "sync-disabled")
            begin += len(shared)
            f.run(claude_command, work, claude_env, case / "claude-baseline")
            baseline = [json.loads(line) for line in log.read_text().splitlines()][begin:]
            f.require(catalog == state["claude_catalogs"][-1], "Claude catalog changed")
            # Empty and unset selectors are equivalent for the native consumers.
            normalize = lambda rows: sorted((r["tag"], r["gh"], sorted((k, v or "") for k, v in r["selectors"].items())) for r in rows)
            f.require(normalize(shared) == normalize(baseline), f"Claude child behavior changed: {shared} vs {baseline}")
            (source / "config.toml").write_text(config_text)
            f.run([str(args.al), "sync"], work, env, case / "sync-restored")
        # Real headless dispatch: native cancellation reaches the next model turn.
        question = state["tool_definitions"].get("request_user_input")
        f.require(question is not None, "Native request_user_input missing")
        (case / "user-input-schema.json").write_text(json.dumps(question, indent=2))
        state["next_call"] = ("request_user_input", {"questions": [{"id": "fixture", "header": "Fixture", "question": "Choose", "options": [{"label": "One", "description": "First"}, {"label": "Two", "description": "Second"}]}]})
        started = json.loads(f.run([str(args.al), "dispatch", "start", "--agent", "muse", "--model", "fixture-model", "--prompt", "Ask fixture question"], work, env, case / "input-start"))
        invocation = started["invocation_id"]
        stopped = False
        try:
            result = json.loads(f.run([str(args.al), "dispatch", "wait", invocation, "--condition", "termination_confirmed"], work, env, case / "input-wait", timeout=90))
            stopped = result.get("termination_confirmed", False)
            f.require(result.get("state") == "completed" and stopped, str(result))
        finally:
            if not stopped:
                f.run([str(args.al), "dispatch", "cancel", invocation], work, env, case / "input-cancel")
                proof = json.loads(f.run([str(args.al), "dispatch", "wait", invocation, "--condition", "termination_confirmed"], work, env, case / "input-stopped", timeout=90))
                f.require(proof.get("termination_confirmed"), "Input fixture termination unconfirmed")
        (case / "model-inputs.json").write_text(json.dumps(state["model_inputs"], indent=2))
        outputs = [item.get("output") for raw in state["model_inputs"] for item in json.loads(raw).get("input", [])
                   if isinstance(item, dict) and item.get("type") == "function_call_output"]
        expected = {"status": "cancelled", "answers": [], "reason": "headless_auto_resolve"}
        f.require(any(isinstance(output, str) and output.startswith("{") and json.loads(output) == expected for output in outputs),
                  "Native cancelled user-input result never reached model")
        f.verify_credential_backends(data)
        (case / "result.json").write_text(json.dumps({"case": label, "child_selectors": True, "nested_dispatch": True, "builtin_dispatch": True, "gh_fallback": True, "claude_equivalent": bool(args.claude), "native_user_input_cancelled": True}, indent=2))
        print(f"PASS {label}: {case}", flush=True)
    finally:
        api.shutdown()


def policy_lock(args, muse):
    """Exercise native persistent approval under the same OS lock used by sync."""
    import fcntl
    import queue
    import signal
    import time
    import uuid

    case = Path(tempfile.mkdtemp(prefix="policy-lock-", dir=args.output))
    home, work, bin_dir = (case / name for name in ("home", "workspace", "bin"))
    bin_dir.mkdir()
    (bin_dir / "al").symlink_to(args.al)
    (bin_dir / "muse").symlink_to(muse)
    native = home / ".config/muse"
    native.mkdir(parents=True)
    work.mkdir()
    env = {"HOME": str(home), "PATH": str(bin_dir) + os.pathsep + os.environ["PATH"], "TBH_DISABLE_TELEMETRY": "1",
           "TBH_CREDENTIAL_BACKEND": "file", "META_API_KEY": "synthetic"}
    api = http.server.ThreadingHTTPServer(("127.0.0.1", 0), f.Fixture)
    api.state = {"requested": set(), "catalog": set(), "headers": [], "http_calls": 0,
                 "command": "printf native_policy_lock_probe"}
    threading.Thread(target=api.serve_forever, daemon=True).start()
    (native / "settings.json").write_text(json.dumps({"schema_version": 1, "endpoint_transport": {
        "base_url": f"http://127.0.0.1:{api.server_port}", "auth": "bearer"}}))
    messages = queue.Queue()
    request_id = 0
    with (case / "stderr.log").open("w") as stderr, (case / "wire.jsonl").open("w") as wire:
        process = None
        thread = None
        def reader():
            for line in process.stdout:
                wire.write(line)
                wire.flush()
                messages.put(json.loads(line))
        def send(method, params):
            nonlocal request_id
            request_id += 1
            process.stdin.write(json.dumps({"jsonrpc": "2.0", "id": request_id, "method": method, "params": params}) + "\n")
            process.stdin.flush()
            return request_id
        def wait(expected):
            deadline = time.monotonic() + 20
            while time.monotonic() < deadline:
                response = messages.get(timeout=max(.01, deadline - time.monotonic()))
                if response.get("id") == expected:
                    f.require("error" not in response, str(response))
                    return response["result"]
            raise RuntimeError("Native MSP response timeout")
        def uid():
            return str(uuid.UUID(int=(int(time.time() * 1000) << 80) | (7 << 76) | (uuid.uuid4().int & ((1 << 76) - 1))))
        try:
            process = subprocess.Popen([str(muse), "serve", "--trust-workspace"], cwd=work, env=env,
                                       stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=stderr,
                                       text=True, start_new_session=True)
            thread = threading.Thread(target=reader, daemon=True)
            thread.start()
            wait(send("initialize", {"clientInfo": {"name": "fixture", "version": "1"}, "capabilities": {"userInputDialogs": False}}))
            process.stdin.write(json.dumps({"jsonrpc": "2.0", "method": "initialized", "params": {}}) + "\n")
            process.stdin.flush()
            session = wait(send("session/start", {"commandId": uid(), "workspaceRoot": str(work),
                           "approvalMode": "promptUnmatched", "providerId": "meta", "modelId": "fixture-model"}))
            sid = session["session"]["sessionId"]
            wait(send("turn/start", {"commandId": uid(), "sessionId": sid, "input": [{"type": "text", "text": "Run fixture"}]}))
            for _ in range(50):
                pending = wait(send("approval/listPending", {"sessionId": sid}))
                if pending["approvals"]:
                    break
                time.sleep(.2)
            f.require(bool(pending["approvals"]), "Native persistent approval never appeared")
            approval = pending["approvals"][0]
            (case / "pending.json").write_text(json.dumps(approval, indent=2))
            policy = native / "approval-policy.json"
            initial = {"schema_version": 2, "rules": []}
            policy.write_text(json.dumps(initial))
            with (native / "approval-policy.lock").open("a+") as lock:
                fcntl.flock(lock, fcntl.LOCK_EX)
                decision = send("approval/decide", {"sessionId": sid, "commandId": uid(), "approvalId": approval["approvalId"],
                                "requirementId": approval["currentRequirementId"], "choiceId": "allow_local_prefix"})
                time.sleep(1)
                f.require(json.loads(policy.read_text()) == initial, "Native wrote while our policy lock was held")
                with messages.mutex:
                    f.require(not any(row.get("id") == decision for row in messages.queue), "Native acknowledged durable approval while locked")
                concurrent = {"effect": "deny", "durability": "local_persistent", "reason": "synthetic concurrent user entry",
                              "rule": {"kind": "shell_command_argv_prefix", "argv_prefix": ["rm"], "workspace_root": str(work)}}
                initial["rules"].append(concurrent)
                policy.write_text(json.dumps(initial))
                fcntl.flock(lock, fcntl.LOCK_UN)
            result = wait(decision)
            saved = json.loads(policy.read_text())
            f.require(concurrent in saved["rules"], "Native discarded concurrent user rule")
            f.require(any(rule["effect"] == "allow" and rule["rule"].get("argv_prefix") == ["printf", "native_policy_lock_probe"] for rule in saved["rules"]), "Native persistent grant missing")
            (case / "result.json").write_text(json.dumps({"blocked_while_locked": True, "merged_concurrent_rule": True,
                                                       "decision": result, "policy": saved}, indent=2))
            f.verify_credential_backends(home / ".local/share")
            print(f"PASS native policy lock and merge: {case}", flush=True)
        finally:
            if process is not None:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait()
            if thread is not None and thread.is_alive():
                thread.join(timeout=5)
            api.shutdown()
            api.server_close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--al", type=Path, required=True)
    parser.add_argument("--muse", help="Existing checksum-verified Muse binary; otherwise use the pinned cache")
    parser.add_argument("--claude")
    parser.add_argument("--output", type=Path, default=ROOT / ".agent-layer/tmp/muse-native")
    args = parser.parse_args()
    args.al = args.al.resolve()
    args.output = args.output.resolve()
    args.output.mkdir(parents=True, exist_ok=True)
    with f.fixture_guard():
        muse, _ = f.native_binary(args.muse, args.output)
        muse = f.fixture_binary(muse, args.output)
        policy_lock(args, muse)
        for label in ("unset", "empty", "custom"):
            exercise(args, muse, label)


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--child":
        child(sys.argv[2], sys.argv[3])
    else:
        main()
