---
name: agent-dispatch
description: Use `al dispatch` only when the user names an external dispatch target or another skill explicitly requires dispatch. Treat generic subagent, second-agent, and fresh-context requests as built-in subagent work.
compatibility: Requires the Agent Layer CLI (`al`) from the project environment and at least one configured target for actual al dispatch runs.
allowed-tools: Bash(al:*) Bash(cat:*)
---

# al dispatch

## Required artifacts

- No artifact is required for ordinary `al dispatch` runs.
- If preparing prompt files or saving target output, put agent-only files under
  `.agent-layer/tmp/`.
- Use `.agent-layer/tmp/agent-dispatch.<run-id>.<type>.md` or
  `.agent-layer/tmp/agent-dispatch.<run-id>.<type>.json` for scratch prompt,
  output, or options files.
- Report every artifact path created.

## Global constraints

- Run `al dispatch --help` before the first `al dispatch` command in a session.
- Run relevant subcommand help before using non-obvious subcommands or flags.
- If `al`, a target CLI, authentication, source skill, or generated skill
  projection is missing, stop and report the missing requirement. Do not
  install, authenticate, sync, or change configuration unless the user asked
  for setup or repair.

## Workflow

1. Verify any requested skill exists in the current project's Agent Layer skill
   source. Do not invent a skill name.
2. Send the focused prompt to one target. Omit unrelated conversation
   history and information that could bias the target.
3. If a prompt file is needed, write it under `.agent-layer/tmp/`.
4. Run `al dispatch` and wait for the original process to finish, even if
   output is quiet or it appears stalled. Do not poll.
   Wait until it finishes to inspect dispatch artifacts.

## Guardrails

- Do not use `al dispatch` for ordinary local shell work, tests, web retrieval,
  browser automation, or API-only tasks.
- Do not use `al dispatch` to bypass the caller's restrictions, approvals, or
  sandbox expectations.
- Do not retry failed target launches by guessing flags, targets, models, or
  reasoning-effort values. Re-check help and options, then report the mismatch
  if it remains unresolved.

## Definition of done

- Current target metadata was inspected before any real run.
- Target selection and skill delivery if used were intentional and reported.
- Prompt and output artifacts were stored only under `.agent-layer/tmp/` and
  every created artifact path was reported.
- Errors were reported explicitly.
- Any side effects from target work were inspected and verified through the
  current task's normal checks.
