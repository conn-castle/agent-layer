"""Pinned Pier 0.3.0 treatment adapters for the Agent Layer benchmark.

This module is materialized only into a mode-0700 benchmark staging directory.
It intentionally uploads a secret-free bundle through Pier's Docker API; native
Pier agents continue to own provider authentication and result accounting.
"""

from __future__ import annotations

import base64
import importlib.metadata
import inspect
import json
import os
import subprocess
import tempfile
import urllib.error
import urllib.request
from pathlib import Path

from pier.agents.installed.claude_code import ClaudeCode
from pier.agents.installed.codex import Codex
from pier.models.trial.paths import EnvironmentPaths

EXPECTED_PIER_VERSION = "0.3.0"
REMOTE_BUNDLE = "/tmp/agent-layer-benchmark"
REMOTE_WORKSPACE = "/app"
PIER_CODEX_AUTH = "/tmp/codex-secrets/auth.json"
REMOTE_CODEX_HOME = Codex._REMOTE_CODEX_HOME.as_posix()
REMOTE_PROJECT_CODEX_HOME = f"{REMOTE_WORKSPACE}/.codex"
REMOTE_CODEX_AUTH = f"{REMOTE_CODEX_HOME}/auth.json"
REMOTE_CODEX_MCP_PREFLIGHT = "/tmp/agent-layer-codex-mcp-preflight.json"
REMOTE_CLAUDE_MCP_PREFLIGHT = "/tmp/agent-layer-claude-mcp-preflight.txt"
REMOTE_DISPATCH_OPTIONS_PREFLIGHT = "/tmp/agent-layer-dispatch-options-preflight.json"
# "task" is already a derived plan artifact name in the workflow skills.
REMOTE_SPEC_FILE = f"{REMOTE_WORKSPACE}/.agent-layer/tmp/spec.md"
# Untracked paths the task image already carried before the agent ran. They are
# not part of the base commit, so sweeping them into the submitted patch makes
# the patch create files the verifier already has, and `git apply` then fails.
PREEXISTING_UNTRACKED = "/tmp/agent-layer-preexisting-untracked"
PROJECTED_PATHS = (
    ".gitignore AGENTS.md CLAUDE.md .github/copilot-instructions.md "
    ".agent .agents .agent-layer .codex .copilot .gemini .mcp.json "
    ".agy .claude .claude-config .vscode/mcp.json .vscode/settings.json "
    "docs/agent-layer"
)


def validate_mcp_initialize_response(
    payload: str, content_type: str, requested_protocol_version: str
) -> None:
    """Require a complete MCP InitializeResult, not merely a JSON-RPC 2xx reply."""
    media_type = content_type.split(";", 1)[0].strip().lower()
    if media_type == "text/event-stream":
        data_lines = []
        for line in payload.splitlines():
            if not line:
                if data_lines:
                    break
                continue
            if line.startswith("data:"):
                data_lines.append(line[5:].lstrip())
        if not data_lines:
            raise RuntimeError("SSE MCP initialize response contained no data event")
        payload = "\n".join(data_lines)
    elif media_type != "application/json":
        raise RuntimeError(f"unsupported HTTP MCP initialize response content type {content_type!r}")
    try:
        response = json.loads(payload)
    except json.JSONDecodeError as error:
        raise RuntimeError("HTTP MCP initialize response is not JSON-RPC") from error
    if not isinstance(response, dict) or response.get("jsonrpc") != "2.0":
        raise RuntimeError("HTTP MCP initialize response is not a JSON-RPC object")
    if "error" in response:
        raise RuntimeError("HTTP MCP initialize response contained an error")
    if response.get("id") != 1 or "result" not in response:
        raise RuntimeError("HTTP MCP initialize response did not match request id 1 with a result")
    result = response["result"]
    if not isinstance(result, dict):
        raise RuntimeError("HTTP MCP initialize result is not an object")
    protocol_version = result.get("protocolVersion")
    if protocol_version != requested_protocol_version:
        raise RuntimeError(
            f"HTTP MCP initialize result protocolVersion {protocol_version!r} does not match "
            f"requested {requested_protocol_version!r}"
        )
    if not isinstance(result.get("capabilities"), dict):
        raise RuntimeError("HTTP MCP initialize result has no capabilities object")
    server_info = result.get("serverInfo")
    if not isinstance(server_info, dict):
        raise RuntimeError("HTTP MCP initialize result has no serverInfo object")
    for key in ("name", "version"):
        if not isinstance(server_info.get(key), str) or not server_info[key].strip():
            raise RuntimeError(f"HTTP MCP initialize serverInfo has no {key}")


def _set_response_read_timeout(response, remaining_seconds: float) -> None:
    """Tighten urllib's socket timeout so each read shares one total deadline."""
    # urllib's HTTPResponse exposes this socket chain on the CPython versions
    # used in Pier. Keep the best-effort traversal isolated for offline fakes.
    socket = getattr(getattr(getattr(response, "fp", None), "raw", None), "_sock", None)
    if socket is not None and hasattr(socket, "settimeout"):
        socket.settimeout(remaining_seconds)


def read_mcp_initialize_response(
    response,
    content_type: str,
    requested_protocol_version: str,
    *,
    deadline_seconds: float = 15,
    byte_cap: int = 1024 * 1024,
    event_cap: int = 128,
    monotonic=None,
) -> str:
    """Read one HTTP/SSE initialize reply under one deadline and finite bounds."""
    if monotonic is None:
        from time import monotonic
    if deadline_seconds <= 0 or byte_cap <= 0 or event_cap <= 0:
        raise RuntimeError("invalid MCP initialize response bounds")
    media_type = content_type.split(";", 1)[0].strip().lower()
    started = monotonic()

    def remaining() -> float:
        seconds = deadline_seconds - (monotonic() - started)
        if seconds <= 0:
            raise RuntimeError(f"HTTP MCP initialize response exceeded {deadline_seconds:g} second deadline")
        _set_response_read_timeout(response, seconds)
        return seconds

    if media_type == "application/json":
        remaining()
        payload = response.read(byte_cap + 1)
        remaining()
        if len(payload) > byte_cap:
            raise RuntimeError(f"HTTP MCP initialize response exceeds {byte_cap} byte limit")
        decoded = payload.decode("utf-8")
        validate_mcp_initialize_response(decoded, "application/json", requested_protocol_version)
        return decoded
    if media_type != "text/event-stream":
        raise RuntimeError(f"unsupported HTTP MCP initialize response content type {content_type!r}")

    total_bytes = 0
    completed_events = 0
    data_lines = []
    while True:
        remaining()
        line = response.readline(byte_cap - total_bytes + 1)
        remaining()
        if not line:
            if data_lines:
                payload = "\n".join(data_lines)
                validate_mcp_initialize_response(payload, "application/json", requested_protocol_version)
                return payload
            raise RuntimeError("SSE MCP initialize response contained no data event")
        total_bytes += len(line)
        if total_bytes > byte_cap:
            raise RuntimeError(f"HTTP MCP initialize response exceeds {byte_cap} byte limit")
        try:
            text = line.decode("utf-8")
        except UnicodeDecodeError as error:
            raise RuntimeError("SSE MCP initialize response is not UTF-8") from error
        if text in ("\n", "\r\n"):
            completed_events += 1
            if completed_events > event_cap:
                raise RuntimeError(f"SSE MCP initialize response exceeds {event_cap} events")
            if data_lines:
                payload = "\n".join(data_lines)
                validate_mcp_initialize_response(payload, "application/json", requested_protocol_version)
                return payload
            continue
        if text.startswith("data:"):
            data_lines.append(text[5:].lstrip().rstrip("\r\n"))


class _AgentLayerTreatment:
    """Install only the declared bundle before the native agent executes."""

    def __init__(
        self,
        *args,
        treatment_bundle: str,
        treatment_mode: str,
        treatment_agent: str,
        treatment_model: str,
        treatment_reasoning_effort: str,
        required_dispatch_roles: str = "",
        credential_names: str = "",
        preflight_only: bool = False,
        codex_credentials_path: str | None = None,
        claude_credentials_path: str | None = None,
        **kwargs,
    ):
        try:
            version = importlib.metadata.version("datacurve-pier")
        except importlib.metadata.PackageNotFoundError as error:
            raise RuntimeError("Pier package metadata is unavailable") from error
        if version != EXPECTED_PIER_VERSION:
            raise RuntimeError(f"Agent Layer benchmark adapter requires Pier {EXPECTED_PIER_VERSION}, got {version}")
        self._treatment_bundle = Path(treatment_bundle)
        self._treatment_mode = treatment_mode
        self._treatment_agent = treatment_agent
        self._treatment_model = treatment_model
        self._treatment_reasoning_effort = treatment_reasoning_effort
        self._preflight_only = preflight_only
        self._codex_credentials_path = Path(codex_credentials_path) if codex_credentials_path else None
        self._claude_credentials_path = Path(claude_credentials_path) if claude_credentials_path else None
        self._required_dispatch_roles = [role for role in required_dispatch_roles.split(",") if role]
        self._credential_names = [name for name in credential_names.split(",") if name]
        self._credential_env = {}
        for name in self._credential_names:
            value = os.environ.get(name)
            if not value:
                raise RuntimeError(f"Configured MCP credential {name} is unavailable")
            self._credential_env[name] = value
        if not self._treatment_bundle.is_dir():
            raise RuntimeError("Agent Layer benchmark treatment bundle is missing")
        if treatment_mode not in {"instructions-only", "instructions-and-skills"}:
            raise RuntimeError(f"Unsupported Agent Layer benchmark treatment mode: {treatment_mode}")
        if treatment_agent not in {"claude", "codex"} or not treatment_model or not treatment_reasoning_effort:
            raise RuntimeError("Agent Layer benchmark workflow target is incomplete")
        self._dispatch_config = None
        if treatment_mode == "instructions-and-skills":
            try:
                self._dispatch_config = json.loads(
                    (self._treatment_bundle / "dispatch-targets.json").read_text(encoding="utf-8")
                )
            except (OSError, json.JSONDecodeError) as error:
                raise RuntimeError("Agent Layer benchmark dispatch targets are unavailable") from error
            if self._dispatch_config.get("schema") != "agent-layer-benchmark-dispatch-v2":
                raise RuntimeError("Agent Layer benchmark dispatch target schema is invalid")
        super().__init__(*args, **kwargs)

    async def _install_treatment(self, environment):
        await self.exec_as_agent(
            environment,
            command=(
                f"cd {REMOTE_WORKSPACE} && "
                f"git ls-files --others -z > {PREEXISTING_UNTRACKED}"
            ),
        )
        await environment.upload_dir(self._treatment_bundle, REMOTE_BUNDLE)
        if self._codex_credentials_path:
            if not self._codex_credentials_path.is_file():
                raise RuntimeError("Codex benchmark credential file is missing")
            await self.exec_as_agent(
                environment,
                command=f"mkdir -p {Path(PIER_CODEX_AUTH).parent}",
            )
            await environment.upload_file(self._codex_credentials_path, PIER_CODEX_AUTH)
        if self._claude_credentials_path:
            if not self._claude_credentials_path.is_file():
                raise RuntimeError("Claude benchmark credential file is missing")
            await environment.upload_file(
                self._claude_credentials_path,
                str(EnvironmentPaths.agent_dir / "sessions" / ".credentials.json"),
            )
        preserve = (
            "rm -rf /tmp/agent-layer-original && mkdir -p /tmp/agent-layer-original && "
            f"cd {REMOTE_WORKSPACE} && "
            f"for path in {PROJECTED_PATHS}; do "
            "key=$(printf '%s' \"$path\" | tr / _); "
            "if test -e \"$path\" || test -L \"$path\"; then "
            "mkdir -p \"/tmp/agent-layer-original/$key\" && cp -a \"$path\" \"/tmp/agent-layer-original/$key/value\"; "
            "else : > \"/tmp/agent-layer-original/$key.absent\"; fi; done"
        )
        await self.exec_as_agent(environment, command=preserve)
        install = (
            f"mkdir -p {REMOTE_WORKSPACE}/docs {REMOTE_WORKSPACE}/.agent-layer/tmp "
            f"&& test ! -f {REMOTE_BUNDLE}/AGENTS.md || cp -a {REMOTE_BUNDLE}/AGENTS.md {REMOTE_WORKSPACE}/AGENTS.md "
            f"&& test ! -d {REMOTE_BUNDLE}/docs/agent-layer || cp -a {REMOTE_BUNDLE}/docs/agent-layer {REMOTE_WORKSPACE}/docs/agent-layer "
            f"&& git -C {REMOTE_WORKSPACE} config user.email benchmark@local.invalid "
            f"&& git -C {REMOTE_WORKSPACE} config user.name 'Agent Layer Benchmark' "
            f"&& (git -C {REMOTE_WORKSPACE} show-ref --verify --quiet refs/heads/main || "
            f"git -C {REMOTE_WORKSPACE} branch main HEAD)"
        )
        # Every study mode carries its normal secret-free projection.  Config
        # (including MCP and permissions), instructions, and skills must not
        # disappear merely because this experiment does not use dispatch.
        install += (
            f" && cp -a {REMOTE_BUNDLE}/.agent-layer/. {REMOTE_WORKSPACE}/.agent-layer/ "
            f"&& test ! -d {REMOTE_BUNDLE}/.agents || cp -a {REMOTE_BUNDLE}/.agents {REMOTE_WORKSPACE}/.agents "
            f"&& for path in .codex .claude .copilot .gemini .mcp.json .vscode; do "
            f"test ! -e {REMOTE_BUNDLE}/\"$path\" || cp -a {REMOTE_BUNDLE}/\"$path\" {REMOTE_WORKSPACE}/\"$path\"; done "
        )
        await self.exec_as_agent(
            environment,
            command=install,
        )
        # Agent Layer resolves configuration placeholders from its normal .env
        # boundary. Upload only the declared names from Pier's child environment
        # before sync; projected-path restoration and host-side sanitization keep
        # these values out of submitted patches and preserved evidence.
        if self._credential_env:
            temporary = None
            try:
                with tempfile.NamedTemporaryFile("w", encoding="utf-8", delete=False) as stream:
                    temporary = stream.name
                    for name in self._credential_names:
                        stream.write(f"{name}={self._credential_env[name]}\n")
                os.chmod(temporary, 0o600)
                await environment.upload_file(
                    Path(temporary),
                    f"{REMOTE_WORKSPACE}/.agent-layer/.env",
                )
            finally:
                if temporary:
                    Path(temporary).unlink(missing_ok=True)
        if self._treatment_mode != "instructions-and-skills":
            # Runtime installation is not a skill feature. Config-only and
            # instructions-only projections can contain normal MCP servers.
            await self.exec_as_root(
                environment,
                command=(
                    f"cp {REMOTE_BUNDLE}/.agent-layer/bin/al-linux-* /usr/local/bin/al "
                    "&& chown root:root /usr/local/bin/al && chmod 0755 /usr/local/bin/al"
                ),
            )
            if self._treatment_agent == "codex":
                await self.exec_as_agent(
                    environment,
                    command=(
                        f"rm -rf {REMOTE_PROJECT_CODEX_HOME} {REMOTE_CODEX_HOME} "
                        f"&& mkdir -p {REMOTE_PROJECT_CODEX_HOME} "
                        f"&& ln -s {REMOTE_PROJECT_CODEX_HOME} {REMOTE_CODEX_HOME}"
                    ),
                )
            await self.exec_as_agent(
                environment,
                command=f"cd {REMOTE_WORKSPACE} && /usr/local/bin/al sync",
            )
        if self._treatment_mode == "instructions-and-skills":
            role_targets = [
                *self._dispatch_config["plan_reviewers"],
                self._dispatch_config["implementer"],
                self._dispatch_config["code_reviewer"],
            ]
            unique_targets = list({
                (target["agent"], target["model"], target["reasoning_effort"]): target
                for target in role_targets
            }.values())
            constraints = base64.b64encode(
                json.dumps(
                    {"targets": unique_targets},
                    sort_keys=True,
                ).encode("utf-8")
            ).decode("ascii")
            await self.exec_as_root(
                environment,
                command=(
                    f"cp {REMOTE_BUNDLE}/.agent-layer/bin/al-linux-* /usr/local/bin/al-real "
                    f"&& cp {REMOTE_BUNDLE}/adapter/al_dispatch_gate.py /usr/local/bin/al "
                    f"&& printf '%s' '{constraints}' | base64 -d "
                    "> /etc/agent-layer-benchmark-dispatch.json "
                    "&& chown root:root /usr/local/bin/al-real /usr/local/bin/al "
                    "/etc/agent-layer-benchmark-dispatch.json "
                    "&& chmod 0755 /usr/local/bin/al-real /usr/local/bin/al "
                    "&& chmod 0644 /etc/agent-layer-benchmark-dispatch.json"
                ),
            )
            # Native client configuration is generated, not shipped in the
            # bundle. Without this sync the coordinator's client never receives
            # the built-in Agent Dispatch MCP server or its permission
            # allowlist, and the treatment arm silently loses dispatch.
            if self._treatment_agent == "codex":
                # Pier fixes the coordinator's CODEX_HOME at /tmp/codex-home,
                # while Agent Layer's local Codex configuration path is
                # /app/.codex. Codex deliberately sanitizes stdio MCP server
                # environments, so CODEX_HOME is not guaranteed to reach the
                # Agent Dispatch server. Make both paths name the same physical
                # home before sync so coordinator, MCP server, and dispatched
                # Codex processes share configuration, authentication, and
                # request-level session evidence regardless of inheritance.
                await self.exec_as_agent(
                    environment,
                    command=(
                        f"rm -rf {REMOTE_PROJECT_CODEX_HOME} {REMOTE_CODEX_HOME} "
                        f"&& mkdir -p {REMOTE_PROJECT_CODEX_HOME} "
                        f"&& ln -s {REMOTE_PROJECT_CODEX_HOME} {REMOTE_CODEX_HOME}"
                    ),
                )
            await self.exec_as_agent(
                environment,
                command=f"cd {REMOTE_WORKSPACE} && /usr/local/bin/al-real sync",
            )
            if self._treatment_agent == "codex":
                # Link Pier's credential into the shared home and fail before
                # the paid coordinator starts if it is unavailable.
                await self.exec_as_agent(
                    environment,
                    command=(
                        f"if ! test -r {PIER_CODEX_AUTH}; then "
                        "echo 'Codex benchmark dispatch credential is unavailable' >&2; exit 1; fi "
                        f"&& mkdir -p $(dirname {REMOTE_CODEX_AUTH}) "
                        f"&& ln -sfn {PIER_CODEX_AUTH} {REMOTE_CODEX_AUTH} "
                        f"&& test -r {REMOTE_CODEX_AUTH}"
                    ),
                )
                await self.exec_as_agent(
                    environment,
                    command=(
                        f"cd {REMOTE_WORKSPACE} && mkdir -p \"$CODEX_HOME\" && "
                        "if [ -s ~/.nvm/nvm.sh ]; then . ~/.nvm/nvm.sh; fi; "
                        f"codex mcp get agent-layer --json > {REMOTE_CODEX_MCP_PREFLIGHT} && "
                        f"jq -e '.enabled == true and .transport.type == \"stdio\" "
                        "and .transport.command == \"al\" "
                        "and .transport.args == [\"dispatch\", \"mcp-server\"]' "
                        f"{REMOTE_CODEX_MCP_PREFLIGHT} >/dev/null || "
                        f"{{ echo 'Codex Agent Dispatch MCP preflight failed' >&2; "
                        f"test ! -f {REMOTE_CODEX_MCP_PREFLIGHT} || "
                        f"cat {REMOTE_CODEX_MCP_PREFLIGHT} >&2; exit 1; }}"
                    ),
                    env=self.build_process_env(
                        {"CODEX_HOME": REMOTE_CODEX_HOME}
                    ),
                )
            else:
                # Unlike Codex's configuration-only inspection, Claude's MCP
                # command also starts the approved project server and reports
                # its health. Require the exact shared project entry.
                await self.exec_as_agent(
                    environment,
                    command=(
                        f"cd {REMOTE_WORKSPACE} && "
                        f"claude mcp get agent-layer > {REMOTE_CLAUDE_MCP_PREFLIGHT} && "
                        f"grep -Eq '^[[:space:]]*Status: .* Connected$' "
                        f"{REMOTE_CLAUDE_MCP_PREFLIGHT} && "
                        f"grep -Fq 'Command: al' {REMOTE_CLAUDE_MCP_PREFLIGHT} && "
                        f"grep -Fq 'Args: dispatch mcp-server' "
                        f"{REMOTE_CLAUDE_MCP_PREFLIGHT} || "
                        f"{{ echo 'Claude Agent Dispatch MCP preflight failed' >&2; "
                        f"test ! -f {REMOTE_CLAUDE_MCP_PREFLIGHT} || "
                        f"cat {REMOTE_CLAUDE_MCP_PREFLIGHT} >&2; exit 1; }}"
                    ),
                )
            # Exercise the other side of the coordinator/dispatch boundary
            # before a paid trial: the exact bundled Agent Layer binary must
            # expose the configured target. A setup failure is infrastructure,
            # not a task result.
            provider_shell_setup = (
                "if [ -s ~/.nvm/nvm.sh ]; then . ~/.nvm/nvm.sh; fi; "
                if self._treatment_agent == "codex"
                else ""
            )
            await self.exec_as_agent(
                environment,
                command=(
                    f"{provider_shell_setup}/usr/local/bin/al dispatch options "
                    f"> {REMOTE_DISPATCH_OPTIONS_PREFLIGHT} && "
                    f"jq -e --arg agent '{self._treatment_agent}' "
                    "'any(.agents[]; .agent == $agent and .available == true)' "
                    f"{REMOTE_DISPATCH_OPTIONS_PREFLIGHT} >/dev/null || "
                    f"{{ echo 'Agent Dispatch target preflight failed' >&2; "
                    f"test ! -f {REMOTE_DISPATCH_OPTIONS_PREFLIGHT} || "
                    f"cat {REMOTE_DISPATCH_OPTIONS_PREFLIGHT} >&2; exit 1; }}"
                ),
            )
        await self._preflight_mcp_transports(environment)

    async def _preflight_mcp_transports(self, environment) -> None:
        """Exercise every configured transport before a paid coordinator starts."""
        # Keep the remote task-side parser byte-for-byte coupled to the
        # offline-tested function above. The HTTP response is protocol evidence,
        # not a generic connectivity probe, so a 2xx page or unrelated JSON must
        # not admit a paid task.
        script = (
            "import json, os, re, select, subprocess, sys, urllib.error, urllib.request\n"
            + inspect.getsource(validate_mcp_initialize_response)
            + inspect.getsource(_set_response_read_timeout)
            + inspect.getsource(read_mcp_initialize_response)
            + r'''
p = "/tmp/agent-layer-benchmark/mcp-preflight.json"
mcp_protocol_version = "2025-03-26"
def resolve(value):
    def replace(match):
        name = match.group(1)
        if name == "AL_REPO_ROOT": return "/app"
        if not os.environ.get(name): raise RuntimeError(f"missing configured MCP credential {name}")
        return os.environ[name]
    return re.sub(r"\$\{([A-Z0-9_]+)\}", replace, value)
for s in json.load(open(p, encoding="utf-8")).get("servers", []):
    transport = s["transport"]
    if transport == "stdio":
        env = os.environ.copy(); env.update({k: resolve(v) for k, v in s.get("env", {}).items()})
        proc = subprocess.Popen([resolve(s["command"]), *[resolve(v) for v in s.get("args", [])]], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env)
        try:
            proc.stdin.write(json.dumps({"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":mcp_protocol_version,"capabilities":{},"clientInfo":{"name":"agent-layer-benchmark","version":"1"}}}) + "\n"); proc.stdin.flush()
            if not select.select([proc.stdout], [], [], 15)[0]: raise RuntimeError("initialize timed out")
            line = proc.stdout.readline()
            validate_mcp_initialize_response(line, "application/json", mcp_protocol_version)
        finally:
            if proc.poll() is None: proc.terminate(); proc.wait(timeout=5)
    elif transport == "http":
        request = urllib.request.Request(resolve(s["url"]), data=json.dumps({"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":mcp_protocol_version,"capabilities":{},"clientInfo":{"name":"agent-layer-benchmark","version":"1"}}}).encode(), headers={"Content-Type":"application/json", "Accept":"application/json, text/event-stream", **{k: resolve(v) for k, v in s.get("headers", {}).items()}}, method="POST")
        try:
            with urllib.request.urlopen(request, timeout=15) as response:
                if response.status >= 400: raise RuntimeError(f"HTTP {response.status}")
                content_type = response.headers.get("Content-Type", "")
                read_mcp_initialize_response(response, content_type, mcp_protocol_version)
        except urllib.error.HTTPError as error:
            raise RuntimeError(f"HTTP MCP authentication/service preflight failed: {error.code}") from error
    else: raise RuntimeError(f"unsupported MCP transport {transport}")
'''
        )
        encoded = base64.b64encode(script.encode("utf-8")).decode("ascii")
        await self.exec_as_agent(
            environment,
            command=(
                f"printf '%s' '{encoded}' | base64 -d | python3 || "
                "{ echo 'configured MCP transport preflight failed' >&2; exit 1; }"
            ),
            env=self.build_process_env(self._credential_env),
        )

    async def _collect_evidence(self, environment):
        evidence_dir = EnvironmentPaths.agent_dir / "agent-layer-dispatch"
        dispatch_sessions_dir = EnvironmentPaths.agent_dir / "sessions" / "agent-layer-dispatch"
        await self.exec_as_agent(
            environment,
            command=(
                f"if test -d {REMOTE_WORKSPACE}/.agent-layer/tmp/runs; then "
                f"find {REMOTE_WORKSPACE}/.agent-layer/tmp/runs -mindepth 2 -maxdepth 2 "
                "-name dispatch.json -exec sh -c '"
                "test \"$(jq -r .state \"$1\")\" != running || "
                "al dispatch cancel \"$(jq -r .name \"$1\")\" >/dev/null"
                "' sh {} \\;; fi"
            ),
        )
        if self._treatment_agent == "codex":
            # Pier has copied the shared session tree and removed its /tmp home
            # symlink. The physical repository-local home remains, allowing us
            # to cancel any stragglers before refreshing the captured evidence.
            # Then move Agent Dispatch sessions under a distinct prefix so Pier
            # selects only the coordinator trajectory; the normalizer still
            # walks and prices every individual session.
            await self.exec_as_agent(
                environment,
                command=(
                    "set -eu; "
                    f"sessions={EnvironmentPaths.agent_dir / 'sessions'}; "
                    f"runs={REMOTE_WORKSPACE}/.agent-layer/tmp/runs; "
                    "ids=/tmp/agent-layer-codex-dispatch-session-ids; "
                    f"if test -d {REMOTE_PROJECT_CODEX_HOME}/sessions; then "
                    "mkdir -p \"$sessions\"; "
                    f"cp -a {REMOTE_PROJECT_CODEX_HOME}/sessions/. \"$sessions\"/; fi; "
                    "if test -d \"$sessions\" && test -d \"$runs\"; then "
                    "find \"$runs\" -mindepth 2 -maxdepth 2 -name dispatch.json "
                    "-exec jq -r '.provider_session_id // empty' {} + "
                    "> \"$ids\"; "
                    "sort -u \"$ids\" -o \"$ids\"; "
                    "while IFS= read -r id; do "
                    "test -n \"$id\" || continue; "
                    f"matches=$(find \"$sessions\" -path {dispatch_sessions_dir} -prune "
                    "-o -type f -name \"*$id*.jsonl\" -print); "
                    "count=$(printf '%s\n' \"$matches\" | "
                    "awk 'NF { count++ } END { print count + 0 }'); "
                    "if test \"$count\" -ne 1; then "
                    "echo \"Expected one captured Codex session for dispatch $id, found $count\" >&2; "
                    "exit 1; fi; "
                    "relative=${matches#\"$sessions\"/}; "
                    f"target={dispatch_sessions_dir}/$relative; "
                    "mkdir -p \"$(dirname \"$target\")\"; "
                    "mv \"$matches\" \"$target\"; "
                    "done < \"$ids\"; fi"
                ),
            )
        await self.exec_as_agent(
            environment,
            command=(
                f"mkdir -p {evidence_dir} && "
                f"test ! -f {REMOTE_CODEX_MCP_PREFLIGHT} || "
                f"cp {REMOTE_CODEX_MCP_PREFLIGHT} {evidence_dir}/codex-mcp-preflight.json; "
                f"test ! -f {REMOTE_CLAUDE_MCP_PREFLIGHT} || "
                f"cp {REMOTE_CLAUDE_MCP_PREFLIGHT} {evidence_dir}/claude-mcp-preflight.txt; "
                f"test ! -f {REMOTE_DISPATCH_OPTIONS_PREFLIGHT} || "
                f"cp {REMOTE_DISPATCH_OPTIONS_PREFLIGHT} {evidence_dir}/dispatch-options-preflight.json; "
                f"if test -d {REMOTE_WORKSPACE}/.agent-layer/tmp/runs; then "
                f"find {REMOTE_WORKSPACE}/.agent-layer/tmp/runs -mindepth 2 -maxdepth 2 "
                "-name dispatch.json -exec sh -c '"
                f"d=$(dirname \"$1\"); id=$(basename \"$d\"); cp \"$1\" {evidence_dir}/\"$id\".json; "
                f"test ! -f \"$d/provider.stdout\" || cp \"$d/provider.stdout\" {evidence_dir}/\"$id\".stdout"
                "' sh {} \\;; fi"
            ),
        )
        await self.exec_as_agent(
            environment,
            command=(
                f"cd {REMOTE_WORKSPACE} && "
                f"for path in {PROJECTED_PATHS}; do "
                "key=$(printf '%s' \"$path\" | tr / _); rm -rf \"$path\"; "
                "if test ! -f \"/tmp/agent-layer-original/$key.absent\"; then "
                "mkdir -p \"$(dirname \"$path\")\" && "
                "cp -a \"/tmp/agent-layer-original/$key/value\" \"$path\"; fi; done"
            ),
        )
        await self.exec_as_agent(
            environment,
            command=(
                f"cd {REMOTE_WORKSPACE} && git add -A && "
                f"if test -s {PREEXISTING_UNTRACKED}; then "
                f"git reset -q --pathspec-from-file={PREEXISTING_UNTRACKED} "
                "--pathspec-file-nul --; fi && "
                "if ! git diff --cached --quiet; then "
                "git commit -m 'Complete benchmark task'; fi"
            ),
        )

    async def _write_spec_file(self, environment, instruction: str) -> None:
        """Persist the verbatim task text so workflow skills can cite it exactly."""
        encoded = base64.b64encode(instruction.encode("utf-8")).decode("ascii")
        await self.exec_as_agent(
            environment,
            command=(
                f"mkdir -p {REMOTE_WORKSPACE}/.agent-layer/tmp && "
                f"printf '%s' '{encoded}' | base64 -d > {REMOTE_SPEC_FILE}"
            ),
        )

    def _workflow_instruction(self, instruction: str) -> str:
        template = (self._treatment_bundle / "workflow-prompt.md").read_text(encoding="utf-8")
        if template.count("{{task}}") == 1:
            # Study-owned entry prompts are reproducibility inputs. Do not add
            # a workflow prefix/suffix or infer task placement.
            return template.replace("{{task}}", instruction)
        describe = lambda target: (
            f"{target['agent']} {target['model']} with "
            f"{target['reasoning_effort']} reasoning-effort"
        )
        plan_reviewers = self._dispatch_config["plan_reviewers"]
        return template.format(
            plan_reviewer_count=len(plan_reviewers),
            plan_reviewers="; ".join(describe(target) for target in plan_reviewers),
            implementer=describe(self._dispatch_config["implementer"]),
            code_reviewer=describe(self._dispatch_config["code_reviewer"]),
            task=instruction,
        )

    async def _prepare(self, instruction: str, environment) -> str:
        """Install the treatment and return the instruction the agent receives."""
        await self._install_treatment(environment)
        if self._treatment_mode != "instructions-and-skills":
            return instruction
        await self._write_spec_file(environment, instruction)
        return self._workflow_instruction(instruction)


class AgentLayerCodex(_AgentLayerTreatment, Codex):
    """Pier Codex adapter with one immutable Agent Layer treatment."""

    def _get_session_dir(self) -> Path | None:
        """Return Pier's coordinator session directory, excluding dispatch evidence."""
        sessions_dir = self.logs_dir / "sessions"
        if not sessions_dir.exists():
            return None

        # Codex stores coordinator sessions at YYYY/MM/DD. Dispatched sessions
        # are copied under agent-layer-dispatch/YYYY/MM/DD for cost accounting.
        # Pier 0.3.0 recursively selects the deepest directories, so that extra
        # prefix makes dispatch dates look like coordinator sessions and a run
        # crossing midnight UTC produces two candidates.
        session_dirs = sorted(
            {path.parent for path in sessions_dir.glob("*/*/*/*.jsonl")}
        )
        if not session_dirs:
            return None
        if len(session_dirs) != 1:
            raise ValueError(
                f"Expected exactly 1 coordinator session, found {len(session_dirs)}"
            )
        return session_dirs[0]

    async def run(self, instruction, environment, context):
        effective = await self._prepare(instruction, environment)
        if self._preflight_only:
            await self._collect_evidence(environment)
            return
        try:
            return await super().run(effective, environment, context)
        finally:
            await self._collect_evidence(environment)


class AgentLayerClaudeCode(_AgentLayerTreatment, ClaudeCode):
    """Pier Claude Code adapter with one immutable Agent Layer treatment."""

    async def run(self, instruction, environment, context):
        effective = await self._prepare(instruction, environment)
        if self._preflight_only:
            await self._collect_evidence(environment)
            return
        try:
            return await super().run(effective, environment, context)
        finally:
            await self._collect_evidence(environment)
