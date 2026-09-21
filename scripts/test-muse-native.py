#!/usr/bin/env python3
"""Run pinned native Muse against real al sync output and loopback-only fixtures.

No account, provider API, container, or user configuration is used. Synthetic
HOME/XDG paths isolate the test, not the production Muse integration.
"""
import argparse
import contextlib
import hashlib
import http.server
import json
import os
from pathlib import Path
import platform
import signal
import shlex
import subprocess
import sys
import tempfile
import threading
import time

ROOT = Path(__file__).resolve().parents[1]
PIN = ROOT / "scripts/test-muse-native/release.json"
MARKER = "NATIVE_MUSE_MCP_VERIFIED"


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def rpc_result(request, tag):
    method = request["method"]
    if method == "initialize":
        return {"protocolVersion": "2024-11-05", "capabilities": {"tools": {}},
                "serverInfo": {"name": tag, "version": "1"}}
    if method == "tools/list":
        return {"tools": [{"name": "probe_" + tag, "description": "Return a fixture marker",
                           "inputSchema": {"type": "object", "properties": {}}}]}
    if method == "tools/call":
        return {"content": [{"type": "text", "text": MARKER + ":" + tag}]}
    raise RuntimeError("Unexpected MCP method: " + method)


def rpc_response(request, tag):
    if request["method"] == "server/discover":
        return {"jsonrpc": "2.0", "id": request["id"], "error": {"code": -32601, "message": "Method not found"}}
    return {"jsonrpc": "2.0", "id": request["id"], "result": rpc_result(request, tag)}


def stdio_server(log, tag):
    with Path(log).open("a") as output:
        output.write("START:" + tag + "\n")
    for line in sys.stdin:
        request = json.loads(line)
        if "id" not in request:
            continue
        if request["method"] == "tools/call":
            with Path(log).open("a") as output:
                output.write("CALL:" + tag + ":" + os.environ.get("PROBE_VALUE", "") + "\n")
        print(json.dumps(rpc_response(request, tag)), flush=True)


def artifact_for_host(pin):
    os_name = {"Linux": "linux", "Darwin": "macos"}.get(platform.system())
    arch = {"arm64": "aarch64", "aarch64": "aarch64",
            "x86_64": "x86", "AMD64": "x86"}.get(platform.machine())
    key = f"{arch}_{os_name}"
    require(key in pin["artifacts"], f"Unsupported native Muse test host: {key}")
    return key, pin["artifacts"][key]


def verify_binary(path, artifact):
    require(path.stat().st_size == artifact["size"], f"Muse size mismatch: {path}")
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    require(digest.hexdigest() == artifact["checksum"], f"Muse SHA-256 mismatch: {path}")


def native_binary(override, output):
    pin = json.loads(PIN.read_text())
    key, artifact = artifact_for_host(pin)
    binary = Path(override).resolve() if override else output / "bin" / pin["version"] / key / "muse"
    if not binary.exists():
        require(not override, f"Native Muse binary does not exist: {binary}")
        binary.parent.mkdir(parents=True, exist_ok=True)
        partial = binary.with_suffix(".download")
        print(f"Downloading native Muse {pin['version']} for {key}", flush=True)
        subprocess.run(["curl", "--fail", "--location", "--silent", "--show-error",
                        "--retry", "2", "--connect-timeout", "30", "--max-time", "300",
                        "--output", str(partial), artifact["url"]], check=True)
        verify_binary(partial, artifact)
        partial.chmod(0o700)
        partial.replace(binary)
    verify_binary(binary, artifact)
    return binary, pin["version"]


def fixture_binary(binary, output):
    """Enforce synthetic auth at every launch, including sanitized MCP children.

    HOME alone does not isolate macOS Keychain. The pinned native file backend
    must be selected inside the executable boundary, not just its parent's env.
    No fixture needs to save credentials or access an OS credential store.
    """
    wrapper = output / "fixture-bin" / "muse"
    wrapper.parent.mkdir(parents=True, exist_ok=True)
    wrapper.write_text("#!/bin/sh\n"
                       "export TBH_CREDENTIAL_BACKEND=file\n"
                       "export META_API_KEY=local-fixture-not-a-real-key\n"
                       "export TBH_DISABLE_TELEMETRY=1\n"
                       "exec " + shlex.quote(str(binary)) + ' "$@"\n')
    wrapper.chmod(0o700)
    return wrapper


# Only workspaces created by this invocation are eligible for cleanup.
_fixture_workspaces = []


def register_workspace(al, work, env):
    _fixture_workspaces.append((str(al), work, env.copy()))


@contextlib.contextmanager
def fixture_guard():
    """Stop on cancellation or loss of the launching parent; reap async runs."""
    parent = os.getppid()
    done = threading.Event()

    def interrupted(signum, _frame):
        raise SystemExit(128 + signum)

    previous = {sig: signal.signal(sig, interrupted) for sig in (signal.SIGTERM, signal.SIGINT)}

    def watch_parent():
        while not done.wait(0.2):
            if os.getppid() != parent:
                os.kill(os.getpid(), signal.SIGTERM)
                return

    watcher = threading.Thread(target=watch_parent, daemon=True)
    watcher.start()
    try:
        yield
    finally:
        done.set()
        watcher.join(timeout=1)
        # A second TERM must not interrupt cleanup and strand another worker.
        for sig in previous:
            signal.signal(sig, signal.SIG_IGN)
        failures = []
        for al, work, env in _fixture_workspaces:
            for record in (work / ".agent-layer/tmp/runs").glob("*/dispatch.json"):
                try:
                    state = json.loads(record.read_text())
                except (ValueError, OSError):
                    failures.append(str(record))
                    continue
                if state.get("termination_confirmed"):
                    continue
                invocation = record.parent.name
                try:
                    reply = subprocess.run([al, "dispatch", "cancel", invocation], cwd=work,
                                           env=env, capture_output=True, text=True, timeout=20)
                    if reply.returncode != 0 or not json.loads(reply.stdout).get("termination_confirmed"):
                        failures.append(str(record))
                except (subprocess.TimeoutExpired, ValueError, OSError):
                    failures.append(str(record))
        for sig, handler in previous.items():
            signal.signal(sig, handler)
        require(not failures, "Fixture worker termination unconfirmed: " + ", ".join(failures))


def run(command, cwd, env, log, timeout=60, pending_tool=None, journals=None):
    prior = set(journals.glob("**/session.jsonl")) if journals else set()
    # A fresh group bounds Muse and its MCP children, including on a timeout.
    with log.with_suffix(".stdout.jsonl").open("w") as stdout, log.with_suffix(".stderr.log").open("w") as stderr:
        process = None
        try:
            process = subprocess.Popen(command, cwd=cwd, env=env, stdout=stdout,
                                       stderr=stderr, start_new_session=True)
            if pending_tool:
                deadline = time.monotonic() + timeout
                found = False
                while process.poll() is None and time.monotonic() < deadline:
                    for journal in set(journals.glob("**/session.jsonl")) - prior:
                        for line in journal.read_text().splitlines():
                            try:
                                event = json.loads(line).get("payload", {}).get("event", {})
                            except json.JSONDecodeError:
                                continue  # The active journal may end in an unfinished write.
                            if event.get("presentation_phase") == "human_pending" and event.get("tool_name") == pending_tool:
                                found = True
                    if found:
                        break
                    time.sleep(0.1)
                require(found, f"Expected native approval for {pending_tool}; see {log}")
                code = 0
            else:
                code = process.wait(timeout=timeout)
        finally:
            if process is not None:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait()
    require(code == 0, f"Command exited {code}: {command[0]}; see {log.with_suffix('.stderr.log')}")
    return log.with_suffix(".stdout.jsonl").read_text()


class Fixture(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def send(self, body, status=200, content_type="application/json"):
        data = body.encode()
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        # Muse's model discovery, not a request to any external provider.
        self.send(json.dumps({"object": "list", "data": [{"id": "fixture-model", "object": "model",
                  "metadata": {"muse-code": {"release_date": "2026-01-01", "is_hidden": False,
                                             "limit": {"context": 1000000, "output": 1024}}}}]}))

    def do_POST(self):
        request = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))))
        state = self.server.state
        if "/messages" in self.path:
            if "count_tokens" in self.path:
                self.send(json.dumps({"input_tokens": 100}))
            else:
                state.setdefault("claude_catalogs", []).append(sorted(tool["name"] for tool in request.get("tools", [])))
                state.setdefault("claude_guidance_counts", []).append(json.dumps(request).count("NATIVE_MUSE_PROJECT_GUIDANCE"))
                self.send(json.dumps({"id": "msg_fixture", "type": "message", "role": "assistant",
                    "model": request.get("model"), "content": [{"type": "text", "text": "Fixture done"}],
                    "stop_reason": "end_turn", "stop_sequence": None, "usage": {"input_tokens": 100, "output_tokens": 10}}))
            return
        if self.path == "/mcp":
            state["headers"].append(self.headers.get("Authorization"))
            if "id" not in request:
                self.send("", 202)
                return
            if request["method"] == "tools/call":
                state["http_calls"] += 1
            self.send(json.dumps(rpc_response(request, "http")))
            return
        state.setdefault("model_inputs", []).append(json.dumps(request))
        definitions = [(tool, outer.get("name", "")) for outer in request.get("tools", [])
                       for tool in (outer.get("tools", []) if outer.get("type") == "namespace" else [outer])]
        state["catalog"].update((tool.get("name", ""), namespace) for tool, namespace in definitions)
        state.setdefault("tool_definitions", {}).update((tool.get("name"), tool) for tool, _ in definitions)
        response = {"id": "resp_fixture", "object": "response", "model": "fixture-model",
                    "status": "in_progress", "output": []}
        frames = [{"type": "response.created", "sequence_number": 1, "response": response}]
        next_tag = next((tag for tag in ("stdio", "http") if tag not in state["requested"]
                         and any(tool.get("name") == "probe_" + tag for tool, _ in definitions)), None)
        forced = state.get("next_call")
        if not forced and state.get("call_plan"):
            forced = state["call_plan"](request)
        if forced and any(tool.get("name") == forced[0].split("__")[-1] for tool, _ in definitions):
            state.pop("next_call", None)
            frames.append({"type": "response.function_call_arguments.done", "sequence_number": 2,
                           "output_index": 0, "item_id": "fc_forced", "name": forced[0],
                           "call_id": "call_forced", "arguments": json.dumps(forced[1])})
        elif next_tag:
            callback = state.pop("before_" + next_tag, None)
            if callback:
                callback()
            state["requested"].add(next_tag)
            frames.append({"type": "response.function_call_arguments.done", "sequence_number": 2,
                           "output_index": 0, "item_id": "fc_" + next_tag,
                           "name": "mcp__fixture_" + next_tag + "__probe_" + next_tag,
                           "call_id": "call_" + next_tag, "arguments": "{}"})
        elif "command" not in state["requested"] and any(tool.get("name") == "bash" for tool, _ in definitions):
            callback = state.pop("before_command", None)
            if callback:
                callback()
            state["requested"].add("command")
            frames.append({"type": "response.function_call_arguments.done", "sequence_number": 2,
                           "output_index": 0, "item_id": "fc_command", "name": "bash",
                           "call_id": "call_command", "arguments": json.dumps({"command": state.get("command", "printf NATIVE_MUSE_COMMAND_VERIFIED"), "description": "Verify selective command grant"})})
        else:
            frames.append({"type": "response.output_text.delta", "sequence_number": 2,
                           "output_index": 0, "item_id": "msg_fixture", "content_index": 0,
                           "delta": "Fixture complete"})
        frames.append({"type": "response.completed", "sequence_number": 3,
                       "response": {**response, "status": "completed",
                                    "usage": {"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
        self.send("".join("data: " + json.dumps(frame) + "\n\n" for frame in frames),
                  content_type="text/event-stream")


def project_config(port, starts, mode="all"):
    lines = ['[approvals]', 'mode = "' + mode + '"']
    for agent in ("claude", "claude_vscode", "codex", "antigravity", "grok", "copilot_cli", "vscode", "muse"):
        lines += [f"[agents.{agent}]", "enabled = " + str(agent in ("claude", "muse")).lower()]
    lines += ['[agents.claude.agent_specific]', 'enableAllProjectMcpServers = true']
    lines += ['[warnings]', 'version_update_on_sync = false', 'noise_mode = "quiet"']
    for name, clients in (("stdio", ["muse"]), ("excluded", ["claude"])):
        lines += ['[[mcp.servers]]', f'id = "fixture-{name}"', 'enabled = true',
                  'transport = "stdio"', 'clients = ' + json.dumps(clients),
                  'command = ' + json.dumps(sys.executable),
                  'args = ' + json.dumps([str(Path(__file__).resolve()), "--stdio", str(starts), name]),
                  'env = { PROBE_VALUE = "${AL_FIXTURE_SECRET}" }']
    lines += ['[[mcp.servers]]', 'id = "fixture-http"', 'enabled = true', 'transport = "http"',
              'http_transport = "streamable"', f'url = "http://127.0.0.1:{port}/mcp"',
              'headers = { Authorization = "Bearer ${AL_FIXTURE_SECRET}" }']
    return "\n".join(lines) + "\n"


def verify_credential_backends(data_home):
    """Check native evidence, not just the environment supplied by the fixture."""
    observed = 0
    for journal in (data_home / "muse/sessions").glob("**/session.jsonl"):
        for line in journal.read_text().splitlines():
            event = json.loads(line)
            if event.get("payload_type") == "session.opened.observed":
                backend = event["payload"]["record"]["credential_backend"]
                require(backend == "file", f"Unexpected native credential backend {backend!r}: {journal}")
                observed += 1
    require(observed > 0, f"Native credential backend evidence missing: {data_home}")


def verify_development_mcp(al, command, work, home, env, case, state):
    """Exercise the generated shell launcher with a deliberately incompatible pin.

    The cached pin is a local executable that fails with a marker, so this
    regression needs no released binary download or external credentials.
    """
    pinned_version = "0.0.1"
    os_name = {"Darwin": "darwin", "Linux": "linux"}[platform.system()]
    arch = {"arm64": "arm64", "aarch64": "arm64", "x86_64": "amd64", "AMD64": "amd64"}[platform.machine()]
    cache = home / ("Library/Caches" if os_name == "darwin" else ".cache")
    binary = cache / "agent-layer/versions" / pinned_version / f"{os_name}-{arch}" / f"al-{os_name}-{arch}"
    binary.parent.mkdir(parents=True, exist_ok=True)
    marker = case / "incompatible-pin-invoked"
    binary.write_text("#!/bin/sh\nprintf 'incompatible pinned fixture\\n' > " + shlex.quote(str(marker)) + "\nexit 65\n")
    binary.chmod(0o700)
    pin = work / ".agent-layer/al.version"
    pin.write_text(pinned_version + "\n")
    development_env = {**env, "AL_DEV_BYPASS_VERSION_DISPATCH": "1"}
    previous_requested, previous_catalog = state["requested"], state["catalog"]
    try:
        run([str(al), "sync"], work, development_env, case / "sync-development-pin")
        generated = json.loads((work / ".mcp.json").read_text())["mcpServers"]["agent-layer"]
        require(generated["command"] == "/bin/sh", "Regression must exercise the generated shell launcher")
        wrong_bin = case / "wrong-al-bin"
        wrong_bin.mkdir()
        wrong_al = wrong_bin / "al"
        wrong_al.write_text("#!/bin/sh\necho WRONG_AL_SELECTED >&2\nexit 65\n")
        wrong_al.chmod(0o700)
        explicit_env = {**development_env, "AL_DEV_EXECUTABLE": str(al),
                        "PATH": str(wrong_bin) + os.pathsep + development_env["PATH"]}
        for label, launch_env in (("development", development_env), ("explicit-source", explicit_env), ("pinned", env)):
            state["requested"], state["catalog"] = {"stdio", "http", "command"}, set()
            log = case / ("mcp-" + label)
            run(command, work, launch_env, log, timeout=60)
            tools = {name for name, _ in state["catalog"] if name.startswith("dispatch_")}
            expected = {"dispatch_options", "dispatch_start", "dispatch_wait", "dispatch_continue",
                        "dispatch_cancel", "dispatch_inspect", "dispatch_output"}
            require(tools == (expected if label != "pinned" else set()),
                    f"Unexpected dispatch catalog for {label}: {tools}")
            require(marker.exists() == (label == "pinned"), f"Incorrect version-pin behavior for {label}")
            require("ignored this session" not in log.with_suffix(".stderr.log").read_text(),
                    "Generated instruction link triggered a native precedence warning")
    finally:
        pin.unlink()
        state["requested"], state["catalog"] = previous_requested, previous_catalog


def exercise(al, muse, output, version, custom_xdg, claude=None):
    label = "custom-xdg" if custom_xdg else "default-xdg"
    case = Path(tempfile.mkdtemp(prefix=label + "-", dir=output / "runs"))
    home, work, bin_dir = (case / name for name in ("home", "workspace", "bin"))
    for path in (home, work, bin_dir):
        path.mkdir()
    (bin_dir / "al").symlink_to(al)
    (bin_dir / "muse").symlink_to(muse)
    env = {"HOME": str(home), "PATH": str(bin_dir) + os.pathsep + os.environ["PATH"],
           "TBH_DISABLE_TELEMETRY": "1", "TBH_CREDENTIAL_BACKEND": "file",
           "META_API_KEY": "local-fixture-not-a-real-key"}
    config_home = home / ".config"
    if custom_xdg:
        config_home = case / "native-config"
        env.update(XDG_CONFIG_HOME=str(config_home), XDG_DATA_HOME=str(case / "native-data"))
    state = {"requested": set(), "catalog": set(), "http_calls": 0, "headers": []}
    api = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Fixture)
    api.state = state
    threading.Thread(target=api.serve_forever, daemon=True).start()
    try:
        starts = case / "mcp-starts.txt"
        native_dir = config_home / "muse"
        native_dir.mkdir(parents=True)
        settings = {"schema_version": 1,
                    "endpoint_transport": {"base_url": f"http://127.0.0.1:{api.server_port}", "auth": "bearer"},
                    "mcpServers": {"user-control": {"type": "stdio", "command": sys.executable,
                        "args": [str(Path(__file__).resolve()), "--stdio", str(starts), "user"], "mode": "required"}}}
        settings_path = native_dir / "settings.json"
        settings_path.write_text(json.dumps(settings))
        original_settings = settings_path.read_bytes()
        run(["git", "-c", "init.templateDir=", "init", "--quiet"], work, env, case / "git-init")
        source = work / ".agent-layer"
        (source / "instructions").mkdir(parents=True)
        (source / "instructions/fixture.md").write_text("NATIVE_MUSE_PROJECT_GUIDANCE\n")
        (source / "skills/native-fixture").mkdir(parents=True)
        (source / "skills/native-fixture/SKILL.md").write_text("---\nname: native-fixture\ndescription: Native fixture skill selection probe\n---\nFixture skill body.\n")
        (source / "config.toml").write_text(project_config(api.server_port, starts))
        (source / ".env").write_text("AL_FIXTURE_SECRET=fixture-value\n")
        (source / "gitignore.block").write_bytes((ROOT / "internal/templates/gitignore.block").read_bytes())
        (source / "commands.allow").write_text("printf NATIVE_MUSE_COMMAND_VERIFIED\n")
        register_workspace(al, work, env)
        run([str(al), "sync"], work, env, case / "sync")
        generated = json.loads((work / ".mcp.json").read_text())
        require(generated["mcpServers"]["fixture-excluded"]["enabled"] is False, "Muse exclusion missing")
        require((work / ".mcp.json").stat().st_mode & 0o777 == 0o600, "Resolved MCP values are not private")
        skills = json.loads(run([str(muse), "skills", "list", "--workspace", str(work),
                                 "--trust-workspace", "--json"], work, env, case / "skills"))
        require(sum(skill["name"] == "native-fixture" for skill in skills["skills"]) == 1,
                "Muse selected a generated skill more than once")
        for tree in (work / ".agents/skills", work / ".claude/skills"):
            require(tree.is_dir() and not tree.is_symlink(), "Existing real skill trees were changed")
        claude_instructions = work / ".claude/CLAUDE.md"
        require(claude_instructions.is_symlink() and claude_instructions.readlink() == Path("../AGENTS.md"),
                "Claude must share canonical project instructions through the relative link")
        if claude:
            claude_env = {**env, "CLAUDE_CONFIG_DIR": str(home / ".claude"),
                          "ANTHROPIC_API_KEY": "synthetic-claude-token", "ANTHROPIC_BASE_URL": f"http://127.0.0.1:{api.server_port}",
                          "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1", "DISABLE_AUTOUPDATER": "1",
                          "AL_FIXTURE_SECRET": "fixture-value"}
            command = [claude, "-p", "Report the fixture tool catalog", "--model", "claude-sonnet-4-6",
                       "--setting-sources", "project", "--no-session-persistence", "--output-format", "stream-json",
                       "--verbose", "--tools", "", "--max-turns", "1"]
            run(command, work, claude_env, case / "claude-shared", timeout=60)
            require(state.get("claude_catalogs"), "Claude did not request its model with a tool catalog")
            require(state.get("claude_guidance_counts") and all(count == 1 for count in state["claude_guidance_counts"]),
                    "Claude must load generated project guidance exactly once through the link")
            shared_catalog = state["claude_catalogs"][-1]
            require(any("fixture-excluded" in tool for tool in shared_catalog), "Claude lost its selected server")
            require(not any("fixture-stdio" in tool for tool in shared_catalog), "Claude acquired a Muse-only server")
            baseline = project_config(api.server_port, starts).replace("[agents.muse]\nenabled = true", "[agents.muse]\nenabled = false")
            (source / "config.toml").write_text(baseline)
            run([str(al), "sync"], work, env, case / "sync-claude-baseline")
            run(command, work, claude_env, case / "claude-baseline", timeout=60)
            require(shared_catalog == state["claude_catalogs"][-1], "Enabling Muse changed Claude's effective tool catalog")
            (case / "claude-catalogs.json").write_text(json.dumps(state["claude_catalogs"], indent=2))
            (source / "config.toml").write_text(project_config(api.server_port, starts))
            run([str(al), "sync"], work, env, case / "sync-restore-muse")
            starts.write_text("")  # Subsequent connection assertions observe Muse only.
            state["headers"].clear()
        # Exercise the native headless runtime against actual al sync output.
        # The interactive al muse launcher has separate Go and shell coverage.
        native_command = [str(muse), "exec", "--workspace", str(work),
                      "--trust-workspace", "--provider", "meta", "--model", "fixture-model",
                      "--approval-mode", "untrusted", "--approval-judge", "off", "--json", "Run the fixture"]
        verify_development_mcp(al, native_command, work, home, env, case, state)
        starts.write_text("")
        state["headers"].clear()
        state["http_calls"] = 0
        stdout = run(native_command, work, env, case / "muse", timeout=90)
        require("ignored this session" not in (case / "muse.stderr.log").read_text(),
                "Native Muse warned about duplicate generated instructions")
        requests = [json.loads(raw) for raw in state.get("model_inputs", [])]
        (case / "model-requests.json").write_text(json.dumps(requests, indent=2))
        lines = starts.read_text().splitlines()
        require("START:user" in lines, "Native user MCP server did not start (possible XDG redirect)")
        require("START:excluded" not in lines, "Claude-only server started in Muse")
        require("CALL:stdio:fixture-value" in lines, "Project stdio tool did not receive resolved environment")
        require(state["http_calls"] == 1, "Project HTTP tool was not called exactly once")
        require(state["headers"] and set(state["headers"]) == {"Bearer fixture-value"}, "HTTP secret resolution failed")
        # Native reminder observers can reach the endpoint before the main
        # agent. Identify main turns by their shell tool, rather than arrival
        # order, and require exactly one instruction copy on every main turn.
        main_requests = [request for request in requests
                         if any(tool.get("name") == "bash" for outer in request.get("tools", [])
                                for tool in (outer.get("tools", []) if outer.get("type") == "namespace" else [outer]))]
        require(main_requests and all(json.dumps(request).count("NATIVE_MUSE_PROJECT_GUIDANCE") == 1
                                      for request in main_requests),
                "Muse main-agent requests must contain generated project instructions exactly once")
        require("NATIVE_MUSE_COMMAND_VERIFIED" in stdout, "Allowlisted command did not run")
        require(all(MARKER + ":" + tag in stdout for tag in ("stdio", "http")), "Native tool results missing")
        require(any(name == "dispatch_start" for name, _ in state["catalog"]), "Built-in Agent Dispatch tools missing")
        require(settings_path.read_bytes() == original_settings, "Native user settings were modified")
        require(not (work / ".muse-config").exists() and not (work / ".muse-data").exists(), "Legacy isolated storage created")
        # Hook cwd is the native workspace root even when launched below it.
        nested = work / "nested"
        nested.mkdir()
        state["requested"].clear()
        nested_output = run(native_command, nested, env, case / "subdirectory", timeout=90)
        require(all(MARKER + ":" + tag in nested_output for tag in ("stdio", "http")), "Subdirectory launch lost MCP grants")
        # Exercise the real dispatch worker and pending-approval observer too.
        state["requested"].clear()
        dispatched = json.loads(run([str(al), "dispatch", "start", "--agent", "muse",
                                     "--model", "fixture-model", "--prompt", "Run the fixture"],
                                    work, env, case / "dispatch-start"))
        invocation = dispatched["invocation_id"]
        stopped = False
        try:
            result = json.loads(run([str(al), "dispatch", "wait", invocation,
                                     "--condition", "termination_confirmed"],
                                    work, env, case / "dispatch-wait", timeout=90))
            stopped = result.get("termination_confirmed", False)
            require(stopped and result.get("state") == "completed", f"Native dispatch did not complete: {result}")
        finally:
            if not stopped:
                run([str(al), "dispatch", "cancel", invocation], work, env, case / "dispatch-cancel")
                proof = json.loads(run([str(al), "dispatch", "wait", invocation,
                                        "--condition", "termination_confirmed"],
                                       work, env, case / "dispatch-stopped", timeout=90))
                require(proof.get("termination_confirmed"), "Native fixture dispatch termination unconfirmed")
        # Native continuation must use the same caller-assigned session and
        # retain conversation context, not merely produce another terminal event.
        state["requested"].clear()
        continued = json.loads(run([str(al), "dispatch", "continue", dispatched["handle"],
                                    "--prompt", "Continue the fixture"], work, env, case / "continue"))
        continuation = continued["invocation_id"]
        continuation_stopped = False
        try:
            resumed = json.loads(run([str(al), "dispatch", "wait", continuation,
                                      "--condition", "termination_confirmed"], work, env,
                                     case / "continue-wait", timeout=90))
            continuation_stopped = resumed.get("termination_confirmed", False)
            require(continuation_stopped and resumed.get("state") == "completed",
                    f"Native continuation failed: {resumed}")
        finally:
            if not continuation_stopped:
                run([str(al), "dispatch", "cancel", continuation], work, env, case / "continue-cancel")
                proof = json.loads(run([str(al), "dispatch", "wait", continuation,
                                        "--condition", "termination_confirmed"], work, env,
                                       case / "continue-stopped", timeout=90))
                require(proof.get("termination_confirmed"), "Continuation termination unconfirmed")
        for run_id in (invocation, continuation):
            run_dir = source / "tmp/runs" / run_id
            require((run_dir / "result.md").is_file(), "Dispatch answer missing")
            require(not (run_dir / "prompt.txt").exists(), "Private dispatch prompt was retained")
        initial_record = json.loads((source / "tmp/runs" / invocation / "dispatch.json").read_text())
        resumed_record = json.loads((source / "tmp/runs" / continuation / "dispatch.json").read_text())
        require(initial_record["provider_session_id"] == resumed_record["provider_session_id"],
                "Continuation changed provider session")
        journals = (case / "native-data" if custom_xdg else home / ".local/share") / "muse/sessions"
        for mode, pending_tool in (("none", "mcp__fixture_stdio__probe_stdio"),
                                   ("commands", "mcp__fixture_stdio__probe_stdio"),
                                   ("mcp", "bash")):
            (source / "config.toml").write_text(project_config(api.server_port, starts, mode))
            run([str(al), "sync"], work, env, case / ("sync-" + mode))
            state["requested"].clear()
            blocked = run(native_command, work, env, case / ("blocked-" + mode),
                          timeout=30, pending_tool=pending_tool, journals=journals)
            require("NATIVE_MUSE_COMMAND_VERIFIED" not in blocked, "Unapproved command ran")
            if mode != "mcp":
                require(MARKER not in blocked, "Unapproved MCP tool ran")
        # The stable hook must revoke within the already-running session.
        (source / "config.toml").write_text(project_config(api.server_port, starts))
        run([str(al), "sync"], work, env, case / "sync-live-revoke")
        original_hooks = (work / ".muse/hooks.json").read_bytes()
        state["requested"].clear()
        state["before_http"] = lambda: (source / "config.toml").write_text(project_config(api.server_port, starts, "commands"))
        before_http = state["http_calls"]
        revoked = run(native_command, work, env, case / "live-mcp-revoke", timeout=30,
                      pending_tool="mcp__fixture_http__probe_http", journals=journals)
        require(MARKER + ":stdio" in revoked, "Live revocation probe did not first perform an allowed call")
        require(state["http_calls"] == before_http, "Live MCP revocation allowed another call")
        require((work / ".muse/hooks.json").read_bytes() == original_hooks, "Revocation relied on replacing hook registration")
        # Command policy is also reread by the native parser after a sync.
        (source / "config.toml").write_text(project_config(api.server_port, starts))
        run([str(al), "sync"], work, env, case / "sync-live-command")
        def revoke_commands():
            (source / "commands.allow").write_text("")
            run([str(al), "sync"], work, env, case / "live-command-sync")
        state["requested"].clear()
        state["before_command"] = revoke_commands
        revoked = run(native_command, work, env, case / "live-command-revoke", timeout=30,
                      pending_tool="bash", journals=journals)
        require("NATIVE_MUSE_COMMAND_VERIFIED" not in revoked, "Native command grant was not revoked")
        (source / "commands.allow").write_text("printf NATIVE_MUSE_COMMAND_VERIFIED\n")
        # A granted argv prefix cannot authorize an unmatched compound suffix.
        run([str(al), "sync"], work, env, case / "sync-compound")
        compound_marker = work / "unapproved-compound-marker"
        state["command"] = "printf NATIVE_MUSE_COMMAND_VERIFIED && touch " + str(compound_marker)
        state["requested"].clear()
        run(native_command, work, env, case / "compound-prefix", timeout=30,
            pending_tool="bash", journals=journals)
        require(not compound_marker.exists(), "Native parser granted an unmatched compound suffix")
        state.pop("command")
        # Native explicit denials must still win over the generated project hook.
        (source / "config.toml").write_text(project_config(api.server_port, starts))
        run([str(al), "sync"], work, env, case / "sync-deny")
        policy_path = native_dir / "approval-policy.json"
        policy = json.loads(policy_path.read_text())
        policy["rules"].append({"effect": "deny", "durability": "local_persistent", "reason": "fixture user denial",
                                "rule": {"kind": "tool_action", "tool_name": "mcp__fixture_stdio__probe_stdio"}})
        policy_path.write_text(json.dumps(policy))
        run([str(al), "sync"], work, env, case / "sync-preserve-deny")
        state["requested"].clear()
        before = starts.read_text().count("CALL:stdio:")
        denied = run(native_command, work, env, case / "native-deny", timeout=90)
        require(starts.read_text().count("CALL:stdio:") == before, "Project hook overrode a native denial")
        require("policy" in denied.lower(), "Native denial evidence missing")
        # Explicit parser-backed shell deny must win over our allow prefix too.
        policy["rules"].append({"effect": "deny", "durability": "local_persistent", "reason": "fixture command denial",
                                "rule": {"kind": "shell_command_argv_prefix", "argv_prefix": ["printf"], "workspace_root": str(work)}})
        policy_path.write_text(json.dumps(policy))
        run([str(al), "sync"], work, env, case / "sync-shell-deny")
        state["requested"].clear()
        denied = run(native_command, work, env, case / "native-shell-deny", timeout=90)
        require("NATIVE_MUSE_COMMAND_VERIFIED" not in denied and "local_persistent:argv_prefix" in denied,
                "Explicit shell denial did not win")
        policy["rules"].pop()
        # Sync must preserve a user's denying hook, and that denial must win.
        policy["rules"].pop()
        policy_path.write_text(json.dumps(policy))
        hooks_path = work / ".muse/hooks.json"
        hooks = json.loads(hooks_path.read_text())
        deny_reply = json.dumps({"hookSpecificOutput": {"hookEventName": "PermissionRequest",
                                                       "decision": {"behavior": "deny"}}})
        hooks["hooks"]["PermissionRequest"].insert(0, {
            "matcher": "mcp__fixture_stdio__.*",
            "hooks": [{"type": "command", "command": "printf '%s\\n' '" + deny_reply + "'"}]})
        hooks_path.write_text(json.dumps(hooks))
        run([str(al), "sync"], work, env, case / "sync-user-hook")
        state["requested"].clear()
        run(native_command, work, env, case / "user-hook-deny", timeout=90)
        require(starts.read_text().count("CALL:stdio:") == before, "Generated hook overrode the user's denying hook")
        verify_credential_backends(Path(env.get("XDG_DATA_HOME") or home / ".local/share"))
        report = {"version": version, "platform": platform.platform(), "case": label,
                  "status": "passed", "catalog": sorted(state["catalog"]), "starts": lines}
        (case / "report.json").write_text(json.dumps(report, indent=2) + "\n")
        print(f"PASS {label}: native grants, live revocation, compound commands, user denials, dispatch/resume; evidence {case}", flush=True)
    finally:
        api.shutdown()
        api.server_close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--al", type=Path, required=True, help="Agent Layer binary built from this checkout")
    parser.add_argument("--muse", help="Use an already downloaded native binary (checksum still enforced)")
    parser.add_argument("--claude", help="Optionally verify native Claude catalog equivalence on the generated shared file")
    parser.add_argument("--output", type=Path, default=ROOT / ".agent-layer/tmp/muse-native")
    args = parser.parse_args()
    output = args.output.resolve()
    (output / "runs").mkdir(parents=True, exist_ok=True)
    al = args.al.resolve(strict=True)
    with fixture_guard():
        binary, version = native_binary(args.muse, output)
        binary = fixture_binary(binary, output)
        for custom in (False, True):
            exercise(al, binary, output, version, custom, args.claude)


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--stdio":
        stdio_server(sys.argv[2], sys.argv[3])
    else:
        main()
