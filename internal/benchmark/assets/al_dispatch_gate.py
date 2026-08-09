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
        targets = constraints["targets"]
        if not isinstance(targets, list) or not targets:
            raise ValueError("targets must be a non-empty list")
        for target in targets:
            if not all(target.get(key) for key in ("agent", "model", "reasoning_effort")):
                raise ValueError("every target must be complete")
    except (OSError, KeyError, ValueError, json.JSONDecodeError) as error:
        _fail(f"cannot load constraints: {error}")

    arguments = sys.argv[1:]
    if arguments[:2] == ["dispatch", "options"]:
        agents = []
        for agent in dict.fromkeys(target["agent"] for target in targets):
            allowed = [target for target in targets if target["agent"] == agent]
            models = list(dict.fromkeys(target["model"] for target in allowed))
            efforts = list(dict.fromkeys(target["reasoning_effort"] for target in allowed))
            agents.append(
                {
                    "agent": agent,
                    "available": True,
                    "model": {
                        "supported": True,
                        "configured": models[0],
                        "suggestions": models,
                        "allow_custom": False,
                    },
                    "reasoning_effort": {
                        "supported": True,
                        "configured": efforts[0],
                        "suggestions": efforts,
                        "allow_custom": False,
                    },
                }
            )
        json.dump(
            {"agents": agents},
            sys.stdout,
        )
        sys.stdout.write("\n")
        return

    if arguments[:2] == ["dispatch", "mcp-server"]:
        # The MCP server accepts tool calls, not command-line flags, so this
        # shim cannot inspect its selections. Hand the same root-owned policy to
        # the Go server, which enforces it inside dispatch_options and
        # dispatch_start.
        os.environ["AL_BENCHMARK_DISPATCH_TARGETS"] = json.dumps(targets, sort_keys=True)
        os.execv(REAL_AL, [REAL_AL, *arguments])

    if arguments[:2] == ["dispatch", "start"]:
        selected_agent = _flag_value(arguments, "--agent")
        selected_model = _flag_value(arguments, "--model")
        selected_effort = _flag_value(arguments, "--reasoning-effort")
        matches = [
            target
            for target in targets
            if target["agent"] == selected_agent
            and (not selected_model or target["model"] == selected_model)
            and (not selected_effort or target["reasoning_effort"] == selected_effort)
        ]
        if len(matches) != 1:
            _fail("dispatch selection must resolve to exactly one configured target")
        target = matches[0]
        if not selected_model:
            arguments.extend(["--model", target["model"]])
        if not selected_effort:
            arguments.extend(["--reasoning-effort", target["reasoning_effort"]])

    os.execv(REAL_AL, [REAL_AL, *arguments])


if __name__ == "__main__":
    main()
