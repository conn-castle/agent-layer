#!/usr/bin/env python3
"""Verify generated Codex MCP startup/context using installed Codex, without inference."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import queue
import shlex
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import time

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("fixture", ROOT / "scripts/test-muse-native.py")
f = importlib.util.module_from_spec(spec)
spec.loader.exec_module(f)
KEYS = ("AL_DISPATCH_ACTIVE", "AL_RUN_ID", "AL_RUN_DIR",
        "AL_DEV_BYPASS_VERSION_DISPATCH", "AL_DEV_EXECUTABLE")


def record_and_exec(path, al):
    with Path(path).open("a") as output:
        output.write(json.dumps({key: os.environ.get(key) for key in KEYS}) + "\n")
    os.execv(al, [al, *sys.argv[4:]])


def probe(codex, work, env, case, depth):
    messages = []
    pending = queue.Queue()
    with (case / "stderr.log").open("w") as stderr:
        process = subprocess.Popen(codex, cwd=work, env=env,
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=stderr,
                                   text=True, start_new_session=True)
        def read():
            for line in process.stdout:
                pending.put(json.loads(line))
            pending.put(None)
        reader = threading.Thread(target=read, daemon=True)
        reader.start()
        def call(method, params):
            request_id = len(messages) + 1
            process.stdin.write(json.dumps({"id": request_id, "method": method, "params": params}) + "\n")
            process.stdin.flush()
            deadline = time.monotonic() + 45
            while True:
                response = pending.get(timeout=max(.01, deadline - time.monotonic()))
                f.require(response is not None, "Codex RPC closed unexpectedly")
                messages.append(response)
                if response.get("id") == request_id:
                    f.require("error" not in response, str(response))
                    return response["result"]
        try:
            call("initialize", {"clientInfo": {"name": "mcp_regression", "version": "1"},
                                "capabilities": {"experimentalApi": True}})
            status = call("mcpServerStatus/list", {})
            server = next(row for row in status["data"] if row["name"] == "agent-layer")
            f.require(len(server.get("tools", {})) == 7, f"Missing dispatch tools: {server}")
            if depth:
                thread = call("thread/start", {"cwd": str(work), "ephemeral": True, "approvalPolicy": "never"})
                result = call("mcpServer/tool/call", {"threadId": thread["thread"]["id"],
                              "server": "agent-layer", "tool": "dispatch_start",
                              "arguments": {"agent": "codex", "prompt": "Must be blocked"}})
                f.require("nested dispatch is blocked at depth 99" in json.dumps(result),
                          f"Dispatch depth guard was lost: {result}")
        finally:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.wait()
            process.stdin.close()
            reader.join(timeout=5)
            process.stdout.close()
            (case / "rpc.json").write_text(json.dumps(messages, indent=2))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--al", required=True, type=Path)
    parser.add_argument("--codex", default="codex")
    args = parser.parse_args()
    al = args.al.resolve(strict=True)
    output = ROOT / ".agent-layer/tmp/codex-mcp-native"
    output.mkdir(parents=True, exist_ok=True)
    case = Path(tempfile.mkdtemp(dir=output))
    home, work, bin_dir = (case / name for name in ("home", "workspace", "bin"))
    for path in (home, work, bin_dir):
        path.mkdir()
    # Any PATH-based fallback must fail: source can have any filename or spaces.
    (bin_dir / "al").write_text("#!/bin/sh\necho WRONG_AL_SELECTED >&2\nexit 65\n")
    (bin_dir / "al").chmod(0o700)
    env = {"HOME": str(home), "PATH": str(bin_dir) + os.pathsep + os.environ["PATH"],
           "CODEX_HOME": str(work / ".codex"), "AL_NO_NETWORK": "1",
           "OPENAI_API_KEY": "synthetic-fixture", "OPENAI_BASE_URL": "http://127.0.0.1:1"}
    f.run(["git", "-c", "init.templateDir=", "init", "--quiet"], work, env, case / "git")
    source = work / ".agent-layer"
    (source / "instructions").mkdir(parents=True)
    (source / "skills").mkdir()
    config = f.project_config(1, case / "unused").split("[[mcp.servers]]")[0]
    config = config.replace("enabled = true", "enabled = false").replace("[agents.codex]\nenabled = false", "[agents.codex]\nenabled = true")
    (source / "config.toml").write_text(config)
    (source / ".env").write_text("")
    (source / "commands.allow").write_text("")
    (source / "gitignore.block").write_bytes((ROOT / "internal/templates/gitignore.block").read_bytes())
    f.run([str(al), "sync"], work, {**env, "AL_DEV_BYPASS_VERSION_DISPATCH": "1"}, case / "sync")
    # No cached binary: lost bypass would fail before the MCP initialize response.
    (source / "al.version").write_text("0.0.1\n")
    for label in ("context", "unset"):
        current = case / label
        current.mkdir()
        recorded = current / "environment.jsonl"
        wrapper = current / "source executable"
        wrapper.write_text("#!/bin/sh\nexec " + " ".join(shlex.quote(x) for x in
                           [sys.executable, str(Path(__file__).resolve()), "--record", str(recorded), str(al)]) + ' "$@"\n')
        wrapper.chmod(0o700)
        launch_env = {**env, "AL_DEV_BYPASS_VERSION_DISPATCH": "1", "AL_DEV_EXECUTABLE": str(wrapper)}
        if label == "context":
            launch_env.update(AL_DISPATCH_ACTIVE="99", AL_RUN_ID="native-parent", AL_RUN_DIR=str(current))
        probe([args.codex, "app-server", "--stdio"], work, launch_env, current, label == "context")
        rows = [json.loads(line) for line in recorded.read_text().splitlines()]
        f.require(rows and all(row == {key: launch_env.get(key) for key in KEYS} for row in rows),
                  f"Native child context differs: {rows}")
    # Exercise the actual al launcher with no executable selector supplied.
    # A client shim only normalizes its CLI arguments to the non-inference RPC mode.
    native = shutil.which(args.codex)
    f.require(native, "Codex executable missing")
    (bin_dir / "codex").write_text("#!/bin/sh\nexec " + shlex.quote(native) + " app-server --stdio\n")
    (bin_dir / "codex").chmod(0o700)
    bootstrap = case / "absolute-launch"
    bootstrap.mkdir()
    probe([str(al), "codex"], work, {**env, "AL_DEV_BYPASS_VERSION_DISPATCH": "1"}, bootstrap, False)
    print(f"PASS: Codex generated MCP startup, runtime context, unset context, depth guard, executable selection; evidence {case}")


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--record":
        record_and_exec(sys.argv[2], sys.argv[3])
    else:
        main()
