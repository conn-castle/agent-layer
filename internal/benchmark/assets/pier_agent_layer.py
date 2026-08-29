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
import shlex
import subprocess
import tempfile
import urllib.error
import urllib.request
import uuid
from datetime import datetime, timezone
from pathlib import Path

from pier.agents.base import BaseAgent
from pier.agents.installed.claude_code import ClaudeCode
from pier.agents.installed.codex import Codex
from pier.agents.installed.base import BaseInstalledAgent
from pier.models.agent.install import AgentInstallSpec, InstallStep
from pier.models.agent.network import NetworkAllowlist
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
REMOTE_ANTIGRAVITY_MCP_PREFLIGHT = "/tmp/agent-layer-antigravity-mcp-preflight.json"
REMOTE_GROK_MCP_PREFLIGHT = "/tmp/agent-layer-grok-mcp-preflight.json"
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
    ".agy .claude .claude-config .grok .grok-config .vscode/mcp.json .vscode/settings.json "
    "docs/agent-layer"
)

# These URLs are immutable, versioned Linux/amd64 artifacts.  The Antigravity
# digest is published in the vendor manifest for 1.1.21.  xAI's installer
# publishes the immutable Grok artifact URL but no SHA-256 sidecar; the pinned
# digest below is the SHA-256 recorded from that vendor object on 2026-08-26.
# Do not replace either with a channel/latest installer.
ANTIGRAVITY_LINUX_AMD64_URL = (
    "https://storage.googleapis.com/antigravity-public/antigravity-cli/"
    "1.1.21-6424454201475072/linux-x64/cli_linux_x64.tar.gz"
)
ANTIGRAVITY_LINUX_AMD64_SHA512 = (
    "3de7552ef089c136c0f570cdc9c04652278e02c1d41ed3911ad5f9f1b8c3bd"
    "567643aa401a1916060ea32a1b17fbaf90cbb417071db33f467880fcd848868d92"
)
GROK_LINUX_AMD64_URL = "https://storage.googleapis.com/grok-build-public-artifacts/cli/grok-1.0.5-linux-x86_64"
GROK_LINUX_AMD64_SHA256 = "9ba87444e1819e8f6104adbbf4676a870c204380aa5c3e1c38a926c4ea677238"
STREAM_BYTE_CAP = 16 * 1024 * 1024
ANTIGRAVITY_PROMPT_BYTE_CAP = 100 * 1024


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
        treatment_bundle: str = "",
        treatment_mode: str = "bare",
        treatment_agent: str,
        treatment_model: str,
        treatment_reasoning_effort: str,
        required_dispatch_roles: str = "",
        credential_names: str = "",
        preflight_only: bool = False,
        codex_credentials_path: str | None = None,
        claude_credentials_path: str | None = None,
        grok_credentials_path: str | None = None,
        antigravity_credentials_path: str | None = None,
        **kwargs,
    ):
        try:
            version = importlib.metadata.version("datacurve-pier")
        except importlib.metadata.PackageNotFoundError as error:
            raise RuntimeError("Pier package metadata is unavailable") from error
        if version != EXPECTED_PIER_VERSION:
            raise RuntimeError(f"Agent Layer benchmark adapter requires Pier {EXPECTED_PIER_VERSION}, got {version}")
        self._treatment_bundle = Path(treatment_bundle) if treatment_bundle else None
        self._treatment_mode = treatment_mode
        self._treatment_agent = treatment_agent
        self._treatment_model = treatment_model
        self._treatment_reasoning_effort = treatment_reasoning_effort
        self._preflight_only = preflight_only
        self._codex_credentials_path = Path(codex_credentials_path) if codex_credentials_path else None
        self._claude_credentials_path = Path(claude_credentials_path) if claude_credentials_path else None
        self._grok_credentials_path = Path(grok_credentials_path) if grok_credentials_path else None
        self._antigravity_credentials_path = (
            Path(antigravity_credentials_path) if antigravity_credentials_path else None
        )
        self._required_dispatch_roles = [role for role in required_dispatch_roles.split(",") if role]
        self._credential_names = [name for name in credential_names.split(",") if name]
        self._credential_env = {}
        for name in self._credential_names:
            value = os.environ.get(name)
            if not value:
                raise RuntimeError(f"Configured MCP credential {name} is unavailable")
            self._credential_env[name] = value
        if treatment_mode == "bare":
            if treatment_bundle or self._required_dispatch_roles or self._credential_names:
                raise RuntimeError("Bare benchmark adapter received treatment state")
            super().__init__(*args, **kwargs)
            return
        if self._treatment_bundle is None or not self._treatment_bundle.is_dir():
            raise RuntimeError("Agent Layer benchmark treatment bundle is missing")
        if treatment_mode not in {"instructions-only", "instructions-and-skills"}:
            raise RuntimeError(f"Unsupported Agent Layer benchmark treatment mode: {treatment_mode}")
        if treatment_agent not in {"claude", "codex", "antigravity", "grok"} or not treatment_model or not treatment_reasoning_effort:
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

    def _record_provider_checkpoint(self, context) -> None:
        """Publish agent completion before Pier starts pre-artifacts/verification."""
        if not getattr(self, "_provider_completed", False) or self._preflight_only:
            return
        self.logs_dir.mkdir(parents=True, exist_ok=True)
        path = self.logs_dir / "provider-checkpoint.json"
        temporary = path.with_suffix(".tmp")
        temporary.write_text(
            json.dumps(
                {
                    "schema": "agent-layer-provider-checkpoint-v1",
                    "completed_at": datetime.now(timezone.utc).isoformat(),
                    "agent_result": context.model_dump(mode="json"),
                },
                sort_keys=True,
            )
            + "\n",
            encoding="utf-8",
        )
        temporary.replace(path)

    async def _stage_treatment(self, environment):
        if self._treatment_mode == "bare":
            return
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
        install = (
            f"mkdir -p {REMOTE_WORKSPACE}/docs {REMOTE_WORKSPACE}/.agent-layer/tmp "
            f"&& test ! -f {REMOTE_BUNDLE}/AGENTS.md || cp -a {REMOTE_BUNDLE}/AGENTS.md {REMOTE_WORKSPACE}/AGENTS.md "
            f"&& test ! -d {REMOTE_BUNDLE}/docs/agent-layer || cp -a {REMOTE_BUNDLE}/docs/agent-layer {REMOTE_WORKSPACE}/docs/agent-layer "
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

    async def _snapshot_workspace(self, environment):
        """Preserve adapter-owned paths and the task image's untracked baseline."""
        preserve = (
            "rm -rf /tmp/agent-layer-original && mkdir -p /tmp/agent-layer-original && "
            f"cd {REMOTE_WORKSPACE} && "
            f"git ls-files --others -z > {PREEXISTING_UNTRACKED} && "
            "git config user.email benchmark@local.invalid && "
            "git config user.name 'Agent Layer Benchmark' && "
            f"for path in {PROJECTED_PATHS}; do "
            "key=$(printf '%s' \"$path\" | tr / _); "
            "if test -e \"$path\" || test -L \"$path\"; then "
            "mkdir -p \"/tmp/agent-layer-original/$key\" && cp -a \"$path\" \"/tmp/agent-layer-original/$key/value\"; "
            "else : > \"/tmp/agent-layer-original/$key.absent\"; fi; done"
        )
        await self.exec_as_agent(environment, command=preserve)
        if self._treatment_mode == "bare":
            return
        await self._stage_treatment(environment)
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
                        f"jq -e '.enabled == true and .transport.type == \"stdio\" and "
                        "((.transport.command == \"al\" and .transport.args == [\"dispatch\", \"mcp-server\"]) or "
                        "(.transport.command == \"/bin/sh\" and .transport.args[0] == \"-c\" and "
                        "(.transport.args[1] | contains(\"exec al dispatch mcp-server\")) and "
                        ".transport.args[2] == \"agent-layer-mcp\" and (.transport.args | length) == 4))' "
                        f"{REMOTE_CODEX_MCP_PREFLIGHT} >/dev/null || "
                        f"{{ echo 'Codex Agent Dispatch MCP preflight failed' >&2; "
                        f"test ! -f {REMOTE_CODEX_MCP_PREFLIGHT} || "
                        f"cat {REMOTE_CODEX_MCP_PREFLIGHT} >&2; exit 1; }}"
                    ),
                    env=self.build_process_env(
                        {"CODEX_HOME": REMOTE_CODEX_HOME}
                    ),
                )
            elif self._treatment_agent == "claude":
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
            elif self._treatment_agent == "antigravity":
                # agy 1.1.21's `mcp list` ignores --gemini_dir and therefore
                # cannot inspect the contained home. Validate the exact file
                # that agy migrates on first launch; the protocol preflight
                # below separately starts the configured server.
                await self.exec_as_agent(
                    environment,
                    command=(
                        f"cd {REMOTE_WORKSPACE} && "
                        f"cp .agy/antigravity-cli/mcp_config.json {REMOTE_ANTIGRAVITY_MCP_PREFLIGHT} && "
                        "jq -e '.mcpServers[\"agent-layer\"] as $s | "
                        "(($s.command == \"al\" and $s.args == [\"dispatch\", \"mcp-server\"]) or "
                        "($s.command == \"/bin/sh\" and $s.args[0] == \"-c\" and "
                        "($s.args[1] | contains(\"exec al dispatch mcp-server\")) and "
                        "$s.args[2] == \"agent-layer-mcp\" and ($s.args | length) == 4))' "
                        f"{REMOTE_ANTIGRAVITY_MCP_PREFLIGHT} >/dev/null || "
                        f"{{ echo 'Antigravity Agent Dispatch MCP preflight failed' >&2; "
                        f"test ! -f {REMOTE_ANTIGRAVITY_MCP_PREFLIGHT} || "
                        f"cat {REMOTE_ANTIGRAVITY_MCP_PREFLIGHT} >&2; exit 1; }}"
                    ),
                )
            elif self._treatment_agent == "grok":
                await self.exec_as_agent(
                    environment,
                    command=(
                        f"cd {REMOTE_WORKSPACE} && grok mcp list --json "
                        f"> {REMOTE_GROK_MCP_PREFLIGHT} && "
                        "jq -e 'any(.[]?; (.name // .id) == \"agent-layer\" and "
                        "(((.command // .transport.command // \"\") == \"al\" and "
                        "(.args // .transport.args) == [\"dispatch\", \"mcp-server\"]) or "
                        "((.command // .transport.command // \"\") == \"/bin/sh\" and "
                        "(.args // .transport.args)[0] == \"-c\" and "
                        "((.args // .transport.args)[1] | contains(\"exec al dispatch mcp-server\")) and "
                        "(.args // .transport.args)[2] == \"agent-layer-mcp\" and "
                        "((.args // .transport.args) | length) == 4)))' "
                        f"{REMOTE_GROK_MCP_PREFLIGHT} >/dev/null || "
                        f"{{ echo 'Grok Agent Dispatch MCP preflight failed' >&2; "
                        f"test ! -f {REMOTE_GROK_MCP_PREFLIGHT} || "
                        f"cat {REMOTE_GROK_MCP_PREFLIGHT} >&2; exit 1; }}"
                    ),
                    env=self.build_process_env({"GROK_HOME": f"{REMOTE_WORKSPACE}/.grok-config"}),
                )
            else:
                raise RuntimeError(f"unsupported treatment MCP provider {self._treatment_agent!r}")
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
                f"test ! -f {REMOTE_ANTIGRAVITY_MCP_PREFLIGHT} || "
                f"cp {REMOTE_ANTIGRAVITY_MCP_PREFLIGHT} {evidence_dir}/antigravity-mcp-preflight.json; "
                f"test ! -f {REMOTE_GROK_MCP_PREFLIGHT} || "
                f"cp {REMOTE_GROK_MCP_PREFLIGHT} {evidence_dir}/grok-mcp-preflight.json; "
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
        await self._snapshot_workspace(environment)
        if self._treatment_mode != "instructions-and-skills":
            return instruction
        await self._write_spec_file(environment, instruction)
        return self._workflow_instruction(instruction)


def _bounded_json_lines(payload: str, *, label: str, byte_cap: int = STREAM_BYTE_CAP):
    """Decode one provider stream without accepting unbounded/malformed output."""
    if len(payload.encode("utf-8")) > byte_cap:
        raise RuntimeError(f"{label} stream exceeds {byte_cap} byte limit")
    for line in payload.splitlines():
        if not line.strip():
            continue
        try:
            value = json.loads(line)
        except json.JSONDecodeError as error:
            raise RuntimeError(f"{label} stream contains malformed JSON") from error
        if not isinstance(value, dict):
            raise RuntimeError(f"{label} stream event is not an object")
        yield value


def parse_antigravity_stream(payload: str):
    """Return terminal API-equivalent usage from exactly one successful result."""
    terminal = None
    for event in _bounded_json_lines(payload, label="Antigravity"):
        if event.get("event") == "result":
            if terminal is not None:
                raise RuntimeError("Antigravity stream has multiple terminal result events")
            terminal = event.get("result")
            if not isinstance(terminal, dict):
                raise RuntimeError("Antigravity terminal result is not an object")
    if terminal is None:
        raise RuntimeError("Antigravity stream has no terminal result event")
    if terminal.get("status") != "SUCCESS":
        detail = terminal.get("error")
        suffix = f": {detail}" if isinstance(detail, str) and detail else ""
        raise RuntimeError(f"Antigravity terminal result was unsuccessful{suffix}")
    usage = terminal.get("usage")
    if not isinstance(usage, dict):
        raise RuntimeError("Antigravity terminal result has no usage object")
    return terminal, usage


def parse_grok_stream(payload: str, session_id: str):
    """Require Grok's caller session, successful end, and request usage events."""
    terminal = None
    usage = []
    for event in _bounded_json_lines(payload, label="Grok"):
        if terminal is not None:
            raise RuntimeError("Grok stream has records after the terminal end event")
        kind = event.get("type")
        if kind == "error":
            raise RuntimeError("Grok reported a provider error")
        if kind == "tool_call_update" and event.get("status") == "failed":
            text = json.dumps(event, sort_keys=True)
            if "Denied by permission policy" in text:
                raise RuntimeError("Grok denied a tool call")
        if kind == "usage":
            usage.append(event)
        if kind == "end":
            terminal = event
    if terminal is None:
        raise RuntimeError("Grok stream has no terminal end event")
    if terminal.get("sessionId") != session_id:
        raise RuntimeError("Grok terminal event did not return the caller-assigned session ID")
    if terminal.get("stopReason") != "end_turn":
        raise RuntimeError("Grok terminal event was unsuccessful")
    if not usage:
        raise RuntimeError("Grok stream has no usage evidence")
    return terminal, usage


class _AgentLayerStreamAgent(_AgentLayerTreatment, BaseInstalledAgent):
    """Shared deterministic headless runner for the non-native Pier agents."""

    BINARY = ""
    PINNED_VERSION = ""
    OUTPUT_NAME = ""

    @staticmethod
    def name() -> str:
        raise NotImplementedError

    def get_version_command(self) -> str | None:
        return f"{self.BINARY} --version"

    def network_allowlist(self) -> NetworkAllowlist:
        """Declare both the immutable installer host and provider API boundary."""
        if self.BINARY == "agy":
            # The contained Gemini API path can contact regional Google API
            # endpoints. A suffix is necessary; a single endpoint is not a
            # stable contract for all supported Gemini requests.
            return NetworkAllowlist(domains=["storage.googleapis.com", ".googleapis.com"])
        if self.BINARY == "grok":
            return NetworkAllowlist(domains=["storage.googleapis.com", "api.x.ai", "auth.x.ai"])
        raise RuntimeError(f"unsupported pinned stream agent binary {self.BINARY!r}")

    def install_spec(self) -> AgentInstallSpec:
        if self.BINARY == "agy":
            url, digest, verify, install = (
                ANTIGRAVITY_LINUX_AMD64_URL,
                ANTIGRAVITY_LINUX_AMD64_SHA512,
                "sha512sum",
                "tar -xzf \"$payload\" -C \"$stage\" antigravity && install -m 0755 \"$stage/antigravity\" /usr/local/bin/agy",
            )
            checksum_algorithm = "sha512"
            checksum_provenance = "vendor linux_amd64 manifest"
        elif self.BINARY == "grok":
            url, digest, verify, install = (
                GROK_LINUX_AMD64_URL,
                GROK_LINUX_AMD64_SHA256,
                "sha256sum",
                "install -m 0755 \"$payload\" /usr/local/bin/grok",
            )
            checksum_algorithm = "sha256"
            checksum_provenance = "sha256 recorded from immutable xAI versioned artifact on 2026-08-26"
        else:
            raise RuntimeError(f"unsupported pinned stream agent binary {self.BINARY!r}")
        # The install step reaches only the one static storage.googleapis.com
        # object declared in metadata.  It never calls a channel, updater, or
        # vendor bootstrap script, and it verifies before replacing the binary.
        command = (
            "set -eu; stage=$(mktemp -d); trap 'rm -rf \"$stage\"' EXIT; "
            "payload=\"$stage/payload\"; "
            # Python is provided by Pier itself. It avoids a mutable package
            # manager/bootstrap dependency merely to fetch one pinned object.
            "python3 -c 'import sys, urllib.request; urllib.request.urlretrieve(sys.argv[1], sys.argv[2])' "
            f"{shlex.quote(url)} \"$payload\"; "
            f"printf '%s  %s\\n' {shlex.quote(digest)} \"$payload\" | {verify} -c -; "
            f"{install}; "
            f"/usr/local/bin/{self.BINARY} --version | grep -F {shlex.quote(self.PINNED_VERSION)} >/dev/null"
        )
        return AgentInstallSpec(
            agent_name=self.name(), version=self.PINNED_VERSION,
            steps=[InstallStep(user="root", run=command)],
            verification_command=self.get_version_command(),
            metadata={
                "pin": self.PINNED_VERSION,
                "linux_amd64_artifact": url,
                "checksum_algorithm": checksum_algorithm,
                "checksum": digest,
                "checksum_provenance": checksum_provenance,
                "network_allowlist": ["storage.googleapis.com"],
            },
        )

    def populate_context_post_run(self, context) -> None:
        # Custom agents do not have a LiteLLM model mapping.  The adapter has
        # already retained and validated its terminal usage/session evidence;
        # cost is reconstructed once by the host normalizer.
        for name, value in (("model_name", self.model_name), ("provider", self.name())):
            if hasattr(context, name):
                setattr(context, name, value)
        self._record_provider_checkpoint(context)

    async def _run_command(self, environment, command: str, env: dict[str, str]):
        await self.exec_as_agent(environment, command=command, env=env)

    def _bounded_provider_capture(self, provider_command: str, stream_path: str, diagnostics_path: str) -> str:
        """Capture each provider stream under the evidence byte ceiling.

        Stdout is the structured stream consumed by the normalizer; stderr is
        diagnostic evidence.  Keeping both bounded prevents a noisy provider
        failure from bypassing Pier's evidence-size contract.
        """
        limiter = (
            "import sys; cap=" + str(STREAM_BYTE_CAP) + "; total=0; out=sys.stdout.buffer; "
            "\nfor chunk in iter(lambda: sys.stdin.buffer.read(65536), b''):\n"
            " total += len(chunk)\n"
            " if total > cap: raise SystemExit('provider stream exceeds byte limit')\n"
            " out.write(chunk); out.flush()\n"
        )
        capture = (
            "set -eu; capture=$(mktemp -d); trap 'rm -rf \"$capture\"' EXIT; "
            "mkfifo \"$capture/stdout\" \"$capture/stderr\"; "
            f"python3 -c {shlex.quote(limiter)} < \"$capture/stdout\" > {shlex.quote(stream_path)} & stdout_pid=$!; "
            f"python3 -c {shlex.quote(limiter)} < \"$capture/stderr\" > {shlex.quote(diagnostics_path)} & stderr_pid=$!; "
            "set +e; "
            f"( {provider_command} ) > \"$capture/stdout\" 2> \"$capture/stderr\"; provider_status=$?; "
            "wait \"$stdout_pid\"; stdout_status=$?; wait \"$stderr_pid\"; stderr_status=$?; set -e; "
            "test \"$provider_status\" -eq 0 && test \"$stdout_status\" -eq 0 && test \"$stderr_status\" -eq 0"
        )
        return f"bash -c {shlex.quote(capture)}"

    async def _validate_retained_stream(self, environment, remote_path: str, provider: str, session_id: str = "") -> None:
        parser = parse_antigravity_stream if provider == "antigravity" else parse_grok_stream
        source = inspect.getsource(_bounded_json_lines) + inspect.getsource(parser)
        invocation = "parse_antigravity_stream(payload)" if provider == "antigravity" else f"parse_grok_stream(payload, {session_id!r})"
        script = (
            f"import json\nSTREAM_BYTE_CAP = {STREAM_BYTE_CAP}\n"
            + source
            + f"\npayload = open({remote_path!r}, encoding='utf-8').read()\n{invocation}\n"
        )
        encoded = base64.b64encode(script.encode("utf-8")).decode("ascii")
        await self.exec_as_agent(
            environment,
            command=f"printf '%s' '{encoded}' | base64 -d | python3",
        )

    async def _preflight_retained_stream_validator(self, environment, provider: str) -> None:
        """Execute the exact post-inference validator before a paid provider call."""
        session_id = "11111111-1111-4111-8111-111111111111"
        if provider == "antigravity":
            payload = (
                '{"event":"result","result":{"status":"SUCCESS",'
                '"conversation_id":"preflight","usage":{"input_tokens":1,"output_tokens":1}}}\n'
            )
        elif provider == "grok":
            payload = (
                '{"type":"usage","usage":{"input_tokens":1,"output_tokens":1}}\n'
                f'{{"type":"end","sessionId":"{session_id}","stopReason":"end_turn"}}\n'
            )
        else:
            raise RuntimeError(f"unsupported retained stream validator provider {provider!r}")
        remote_path = f"/tmp/agent-layer-{provider}-stream-validator-preflight.jsonl"
        encoded = base64.b64encode(payload.encode("utf-8")).decode("ascii")
        try:
            await self.exec_as_agent(
                environment,
                command=f"printf '%s' '{encoded}' | base64 -d > {remote_path}",
            )
            await self._validate_retained_stream(environment, remote_path, provider, session_id)
        finally:
            await self.exec_as_agent(environment, command=f"rm -f {remote_path}")


class AgentLayerAntigravity(_AgentLayerStreamAgent):
    BINARY = "agy"
    PINNED_VERSION = "1.1.21"
    OUTPUT_NAME = "antigravity.jsonl"

    @staticmethod
    def name() -> str:
        return "antigravity"

    async def run(self, instruction, environment, context):
        remote_credential = f"{REMOTE_WORKSPACE}/.agy/antigravity-cli/antigravity-oauth-token"
        effective = await self._prepare(self.render_instruction(instruction), environment)
        try:
            if len(effective.encode("utf-8")) > ANTIGRAVITY_PROMPT_BYTE_CAP:
                raise RuntimeError(
                    f"Antigravity benchmark prompt exceeds {ANTIGRAVITY_PROMPT_BYTE_CAP} byte limit"
                )
            if not self._antigravity_credentials_path or not self._antigravity_credentials_path.is_file():
                raise RuntimeError("Antigravity benchmark requires the Agent Layer OAuth profile")
            settings_path = f"{REMOTE_WORKSPACE}/.agy/antigravity-cli/settings.json"
            settings_script = f'''import json, os
path = {settings_path!r}
os.makedirs(os.path.dirname(path), exist_ok=True)
try:
    with open(path, encoding="utf-8") as stream:
        value = json.load(stream)
except FileNotFoundError:
    value = {{}}
if not isinstance(value, dict):
    raise RuntimeError("Antigravity settings are not a JSON object")
value.pop("modelProvider", None)
with open(path, "w", encoding="utf-8") as stream:
    json.dump(value, stream, indent=2, sort_keys=True)
    stream.write("\\n")
'''
            encoded_settings = base64.b64encode(settings_script.encode("utf-8")).decode("ascii")
            await self.exec_as_agent(
                environment,
                command=f"printf '%s' '{encoded_settings}' | base64 -d | python3",
            )
            await environment.upload_file(self._antigravity_credentials_path, remote_credential)
            await self.exec_as_agent(environment, command=f"chmod 0600 {remote_credential}")
            if self._preflight_only:
                models_path = "/tmp/agent-layer-antigravity-models.txt"
                await self.exec_as_agent(
                    environment,
                    command=(
                        f"AGY_CLI_DISABLE_AUTO_UPDATE=1 agy --gemini_dir={REMOTE_WORKSPACE}/.agy models "
                        f"> {models_path} && awk -v expected={shlex.quote(self.model_name or '')} "
                        f"{shlex.quote('$1 == expected { found=1 } END { exit !found }')} {models_path} || "
                        f"{{ echo 'Antigravity benchmark model is unavailable' >&2; cat {models_path} >&2; exit 1; }}"
                    ),
                )
                await self._preflight_retained_stream_validator(environment, "antigravity")
                return
            env = self.build_process_env({"AGY_CLI_DISABLE_AUTO_UPDATE": "1"})
            stream_path = str(EnvironmentPaths.agent_dir / self.OUTPUT_NAME)
            diagnostics_path = str(EnvironmentPaths.agent_dir / "antigravity.stderr")
            # Pier owns the hard execution deadline. The long client deadline
            # prevents agy's five-minute default from preempting that contract.
            provider_command = (
                f"agy --gemini_dir={REMOTE_WORKSPACE}/.agy --model {shlex.quote(self.model_name or '')} "
                "--output-format stream-json "
                "--dangerously-skip-permissions --print-timeout 24h "
                f"--print {shlex.quote(effective)}"
            )
            command = self._bounded_provider_capture(provider_command, stream_path, diagnostics_path)
            await self._run_command(environment, command, env)
            await self._validate_retained_stream(environment, stream_path, "antigravity")
            self._provider_completed = True
        finally:
            try:
                await self.exec_as_agent(environment, command=f"rm -f {remote_credential}")
            except Exception:
                pass
            await self._collect_evidence(environment)


class AgentLayerGrok(_AgentLayerStreamAgent):
    BINARY = "grok"
    PINNED_VERSION = "1.0.5"
    OUTPUT_NAME = "grok.jsonl"
    # Pier already runs the agent in a disposable task container. Grok's
    # built-in devbox profile is designed for that boundary and, unlike the
    # workspace profile's protected global-hook paths, does not require bwrap.
    SANDBOX_PROFILE = "devbox"

    @staticmethod
    def name() -> str:
        return "grok"

    async def run(self, instruction, environment, context):
        effective = await self._prepare(self.render_instruction(instruction), environment)
        try:
            if not self._grok_credentials_path or not self._grok_credentials_path.is_file():
                raise RuntimeError("Grok benchmark credential file is missing")
            session = str(uuid.uuid4())
            prompt_path = "/tmp/agent-layer-grok-prompt.txt"
            grok_home = f"{REMOTE_WORKSPACE}/.grok-config"
            with tempfile.NamedTemporaryFile("w", encoding="utf-8", delete=False) as stream:
                stream.write(effective)
                local_prompt = stream.name
            try:
                await self.exec_as_agent(
                    environment,
                    command=f"mkdir -p {grok_home} && chmod 0700 {grok_home}",
                )
                await environment.upload_file(Path(local_prompt), prompt_path)
                await environment.upload_file(self._grok_credentials_path, f"{grok_home}/auth.json")
                await self.exec_as_agent(
                    environment,
                    command=f"chmod 0600 {grok_home}/auth.json",
                )
            finally:
                Path(local_prompt).unlink(missing_ok=True)
            env = self.build_process_env({"GROK_HOME": grok_home, "GROK_MEMORY": "0", "GROK_CLAUDE_AGENTS_ENABLED": "false"})
            if self._preflight_only:
                models_path = "/tmp/agent-layer-grok-models.txt"
                await self.exec_as_agent(
                    environment,
                    command=(
                        f"grok --no-auto-update --sandbox {self.SANDBOX_PROFILE} models > {models_path} && "
                        f"awk -v expected={shlex.quote(self.model_name or '')} "
                        f"{shlex.quote('$1 ~ /^[-*]$/ && $2 == expected { found=1 } END { exit !found }')} "
                        f"{models_path} || "
                        f"{{ echo 'Grok benchmark model is unavailable' >&2; cat {models_path} >&2; exit 1; }}"
                    ),
                    env=env,
                )
                await self._preflight_retained_stream_validator(environment, "grok")
                return
            stream_path = str(EnvironmentPaths.agent_dir / self.OUTPUT_NAME)
            diagnostics_path = str(EnvironmentPaths.agent_dir / "grok.stderr")
            provider_command = (
                f"grok --no-auto-update --prompt-file {prompt_path} --output-format streaming-json "
                f"--session-id {session} --model {shlex.quote(self.model_name or '')} "
                f"--reasoning-effort {shlex.quote(self._treatment_reasoning_effort)} --no-memory "
                f"--trust --sandbox {self.SANDBOX_PROFILE} --permission-mode bypassPermissions --always-approve "
                ""
            )
            command = self._bounded_provider_capture(provider_command, stream_path, diagnostics_path)
            await self._run_command(environment, command, env)
            await self._validate_retained_stream(environment, stream_path, "grok", session)
            self._provider_completed = True
        finally:
            # The credential is private process setup state, never run evidence.
            try:
                await self.exec_as_agent(environment, command=f"rm -f {REMOTE_WORKSPACE}/.grok-config/auth.json")
            except Exception:
                pass
            await self._collect_evidence(environment)


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
            result = await super().run(effective, environment, context)
            self._provider_completed = True
            return result
        finally:
            await self._collect_evidence(environment)

    def populate_context_post_run(self, context) -> None:
        super().populate_context_post_run(context)
        self._record_provider_checkpoint(context)


class AgentLayerClaudeCode(_AgentLayerTreatment, ClaudeCode):
    """Pier Claude Code adapter with one immutable Agent Layer treatment."""

    async def run(self, instruction, environment, context):
        effective = await self._prepare(instruction, environment)
        if self._preflight_only:
            await self._collect_evidence(environment)
            return
        try:
            result = await super().run(effective, environment, context)
            self._provider_completed = True
            return result
        finally:
            await self._collect_evidence(environment)

    def populate_context_post_run(self, context) -> None:
        super().populate_context_post_run(context)
        self._record_provider_checkpoint(context)


class AgentLayerPatchReplay(BaseAgent):
    """Apply one retained provider patch, then let Pier run its normal verifier."""

    def __init__(self, *args, replay_patch: str, **kwargs):
        if importlib.metadata.version("datacurve-pier") != EXPECTED_PIER_VERSION:
            raise RuntimeError("Agent Layer verifier replay requires the pinned Pier version")
        self._replay_patch = Path(replay_patch)
        if not self._replay_patch.is_file():
            raise RuntimeError("Agent Layer verifier replay patch is missing")
        super().__init__(*args, **kwargs)

    @staticmethod
    def name() -> str:
        return "agent-layer-patch-replay"

    def version(self) -> str:
        return "1"

    async def setup(self, environment) -> None:
        pass

    async def run(self, instruction, environment, context) -> None:
        if self._replay_patch.stat().st_size == 0:
            # The provider completed without committing changes. Pier's
            # pre-artifacts export yields the same empty patch, and git
            # rejects empty input, so the verifier runs on the base tree.
            return
        remote_patch = "/tmp/agent-layer-provider.patch"
        await environment.upload_file(self._replay_patch, remote_patch)
        result = await environment.exec(
            command=(
                f"git -C {REMOTE_WORKSPACE} config user.email benchmark@local.invalid && "
                f"git -C {REMOTE_WORKSPACE} config user.name 'Agent Layer Verifier Replay' && "
                f"git -C {REMOTE_WORKSPACE} apply --binary --index {remote_patch} && "
                f"git -C {REMOTE_WORKSPACE} commit -m 'Replay retained provider patch'"
            )
        )
        if result.return_code != 0:
            detail = result.stderr or result.stdout or "no output"
            raise RuntimeError(f"apply retained provider patch: {detail}")
