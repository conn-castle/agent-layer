"""Pinned Pier 0.3.0 treatment adapters for the Agent Layer benchmark.

This module is materialized only into a mode-0700 benchmark staging directory.
It intentionally uploads a secret-free bundle through Pier's Docker API; native
Pier agents continue to own provider authentication and result accounting.
"""

from __future__ import annotations

import base64
import importlib.metadata
import json
import os
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
            f"&& cp -a {REMOTE_BUNDLE}/AGENTS.md {REMOTE_WORKSPACE}/AGENTS.md "
            f"&& cp -a {REMOTE_BUNDLE}/docs/agent-layer {REMOTE_WORKSPACE}/docs/agent-layer "
            f"&& git -C {REMOTE_WORKSPACE} config user.email benchmark@local.invalid "
            f"&& git -C {REMOTE_WORKSPACE} config user.name 'Agent Layer Benchmark' "
            f"&& (git -C {REMOTE_WORKSPACE} show-ref --verify --quiet refs/heads/main || "
            f"git -C {REMOTE_WORKSPACE} branch main HEAD)"
        )
        if self._treatment_mode == "instructions-and-skills":
            install += (
                f" && cp -a {REMOTE_BUNDLE}/.agent-layer/. {REMOTE_WORKSPACE}/.agent-layer/ "
                f"&& cp -a {REMOTE_BUNDLE}/.agents {REMOTE_WORKSPACE}/.agents "
            )
        await self.exec_as_agent(
            environment,
            command=install,
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
