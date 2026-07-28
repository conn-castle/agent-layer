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
# Named instructions.md, not task.md: "task" is already a plan artifact name in
# the workflow skills, and colliding with it invites the agent to treat the
# verbatim specification as just another derived artifact.
REMOTE_INSTRUCTIONS_FILE = f"{REMOTE_WORKSPACE}/.agent-layer/tmp/instructions.md"
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
        self._claude_credentials_path = Path(claude_credentials_path) if claude_credentials_path else None
        self._required_dispatch_roles = [role for role in required_dispatch_roles.split(",") if role]
        if not self._treatment_bundle.is_dir():
            raise RuntimeError("Agent Layer benchmark treatment bundle is missing")
        if treatment_mode not in {"instructions-only", "instructions-and-skills"}:
            raise RuntimeError(f"Unsupported Agent Layer benchmark treatment mode: {treatment_mode}")
        if treatment_agent not in {"claude", "codex"} or not treatment_model or not treatment_reasoning_effort:
            raise RuntimeError("Agent Layer benchmark workflow target is incomplete")
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
            constraints = base64.b64encode(
                json.dumps(
                    {
                        "agent": self._treatment_agent,
                        "model": self._treatment_model,
                        "reasoning_effort": self._treatment_reasoning_effort,
                    },
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

    async def _collect_evidence(self, environment):
        evidence_dir = EnvironmentPaths.agent_dir / "agent-layer-dispatch"
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
        await self.exec_as_agent(
            environment,
            command=(
                f"mkdir -p {evidence_dir} && "
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

    async def _write_instructions_file(self, environment, instruction: str) -> None:
        """Persist the verbatim task text so workflow skills can cite it exactly."""
        encoded = base64.b64encode(instruction.encode("utf-8")).decode("ascii")
        await self.exec_as_agent(
            environment,
            command=(
                f"mkdir -p {REMOTE_WORKSPACE}/.agent-layer/tmp && "
                f"printf '%s' '{encoded}' | base64 -d > {REMOTE_INSTRUCTIONS_FILE}"
            ),
        )

    def _workflow_instruction(self, instruction: str) -> str:
        template = (self._treatment_bundle / "workflow-prompt.md").read_text(encoding="utf-8")
        target = (
            f"{self._treatment_agent} {self._treatment_model} with "
            f"{self._treatment_reasoning_effort} reasoning-effort"
        )
        return template.format(
            plan_reviewer_count=1,
            plan_reviewers=target,
            implementer=target,
            code_reviewer=target,
            fixer=target,
            task=instruction,
        )

    async def _prepare(self, instruction: str, environment) -> str:
        """Install the treatment and return the instruction the agent receives."""
        await self._install_treatment(environment)
        if self._treatment_mode != "instructions-and-skills":
            return instruction
        await self._write_instructions_file(environment, instruction)
        return self._workflow_instruction(instruction)


class AgentLayerCodex(_AgentLayerTreatment, Codex):
    """Pier Codex adapter with one immutable Agent Layer treatment."""

    async def run(self, instruction, environment, context):
        effective = await self._prepare(instruction, environment)
        try:
            return await super().run(effective, environment, context)
        finally:
            await self._collect_evidence(environment)


class AgentLayerClaudeCode(_AgentLayerTreatment, ClaudeCode):
    """Pier Claude Code adapter with one immutable Agent Layer treatment."""

    async def run(self, instruction, environment, context):
        effective = await self._prepare(instruction, environment)
        try:
            return await super().run(effective, environment, context)
        finally:
            await self._collect_evidence(environment)
