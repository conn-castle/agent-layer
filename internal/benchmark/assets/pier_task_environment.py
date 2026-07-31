"""Pinned Pier environment adapter for task-defined startup requirements."""

from __future__ import annotations

import shlex
from pathlib import Path

from pier.environments.docker.docker import DockerEnvironment


REMOTE_READINESS_SCRIPT = "/tmp/agent-layer-task-readiness.sh"
REMOTE_STARTUP_SCRIPT = "/tmp/agent-layer-task-startup.sh"


class CertifiedDockerEnvironment(DockerEnvironment):
    """Digest-pinned Docker environment certified before the agent starts."""

    def __init__(
        self,
        *args: object,
        readiness_script: str,
        pinned_image: str,
        startup_script: str | None = None,
        verifier_source_root: str,
        verifier_context: str,
        **kwargs: object,
    ) -> None:
        """Pin the task image and record its task-owned environment programs."""
        self._readiness_script = Path(readiness_script)
        if not self._readiness_script.is_file():
            raise RuntimeError("Task readiness program is missing")
        self._startup_script = Path(startup_script) if startup_script else None
        if self._startup_script is not None and not self._startup_script.is_file():
            raise RuntimeError("Task startup program is missing")
        task_env_config = kwargs.get("task_env_config")
        if task_env_config is None:
            raise RuntimeError("Task environment configuration is missing")
        environment_dir = Path(str(kwargs.get("environment_dir", ""))).resolve()
        verifier_source = Path(verifier_source_root).resolve()
        if environment_dir.is_relative_to(verifier_source):
            relative = environment_dir.relative_to(verifier_source)
            kwargs["environment_dir"] = Path(verifier_context) / relative
            task_env_config.docker_image = None
        else:
            task_env_config.docker_image = pinned_image
        super().__init__(*args, **kwargs)

    async def start(self, force_build: bool) -> None:
        """Start the pinned container and certify it before provider execution."""
        await super().start(force_build=force_build)
        await self.upload_file(self._readiness_script, REMOTE_READINESS_SCRIPT)
        result = await self.exec(
            f"bash {shlex.quote(REMOTE_READINESS_SCRIPT)}",
            user=0,
        )
        output = "\n".join(
            part.strip() for part in (result.stdout, result.stderr) if part and part.strip()
        )
        if result.return_code != 0:
            detail = f": {output}" if output else ""
            raise RuntimeError(
                f"Task environment readiness program exited with code {result.return_code}{detail}"
            )
        if output:
            self.logger.info("Task environment readiness output:\n%s", output)
        if self._startup_script is not None:
            await self.upload_file(self._startup_script, REMOTE_STARTUP_SCRIPT)
            result = await self.exec(
                f"bash {shlex.quote(REMOTE_STARTUP_SCRIPT)}",
                user=0,
            )
            output = "\n".join(
                part.strip()
                for part in (result.stdout, result.stderr)
                if part and part.strip()
            )
            if result.return_code != 0:
                detail = f": {output}" if output else ""
                raise RuntimeError(
                    f"Task startup program exited with code {result.return_code}{detail}"
                )
            if output:
                self.logger.info("Task startup output:\n%s", output)
