#!/usr/bin/env python3
"""Restrict Agent Layer dispatch choices inside benchmark containers."""

from __future__ import annotations

import json
import os
import sys
from pathlib import Path

REAL_AL = "/usr/local/bin/al-real"
CONSTRAINTS = Path("/etc/agent-layer-benchmark-dispatch.json")


def _fail(message: str) -> None:
    """Exit with a benchmark dispatch constraint error."""
    print(f"benchmark dispatch constraint: {message}", file=sys.stderr)
    raise SystemExit(2)


def _flag_value(arguments: list[str], name: str) -> str:
    """Return a command-line flag value, accepting split and equals forms."""
    prefix = name + "="
    for index, argument in enumerate(arguments):
        if argument.startswith(prefix):
            return argument[len(prefix) :]
        if argument == name:
            if index + 1 >= len(arguments):
                _fail(f"{name} requires a value")
            return arguments[index + 1]
    return ""


def main() -> None:
    """Filter dispatch discovery and reject unavailable dispatch selections."""
    try:
        constraints = json.loads(CONSTRAINTS.read_text(encoding="utf-8"))
        agent = constraints["agent"]
        model = constraints["model"]
        effort = constraints["reasoning_effort"]
    except (OSError, KeyError, json.JSONDecodeError) as error:
        _fail(f"cannot load constraints: {error}")

    arguments = sys.argv[1:]
    if arguments[:2] == ["dispatch", "options"]:
        json.dump(
            {
                "agents": [
                    {
                        "agent": agent,
                        "available": True,
                        "model": {
                            "supported": True,
                            "configured": model,
                            "suggestions": [model],
                            "allow_custom": False,
                        },
                        "reasoning_effort": {
                            "supported": True,
                            "configured": effort,
                            "suggestions": [effort],
                            "allow_custom": False,
                        },
                    }
                ]
            },
            sys.stdout,
        )
        sys.stdout.write("\n")
        return

    if arguments[:2] == ["dispatch", "start"]:
        selected_agent = _flag_value(arguments, "--agent")
        selected_model = _flag_value(arguments, "--model")
        selected_effort = _flag_value(arguments, "--reasoning-effort")
        if selected_agent != agent:
            _fail(f"--agent must be {agent!r}, got {selected_agent!r}")
        if selected_model and selected_model != model:
            _fail(f"--model must be {model!r}, got {selected_model!r}")
        if selected_effort and selected_effort != effort:
            _fail(
                f"--reasoning-effort must be {effort!r}, got {selected_effort!r}"
            )
        if not selected_model:
            arguments.extend(["--model", model])
        if not selected_effort:
            arguments.extend(["--reasoning-effort", effort])

    os.execv(REAL_AL, [REAL_AL, *arguments])


if __name__ == "__main__":
    main()
