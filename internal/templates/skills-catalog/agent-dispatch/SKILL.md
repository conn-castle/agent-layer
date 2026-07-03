---
name: agent-dispatch
description: Use `al dispatch` only for human-requested focused headless second-agent work, or when another active skill's written workflow explicitly requires subagent/fresh-context delegation. Do not use for discretionary/background second opinions, ordinary shell commands, local test runs, web search, browser automation, or multi-agent orchestration.
compatibility: Requires the Agent Layer CLI (`al`) from the project environment and at least one configured target for actual al dispatch runs.
allowed-tools: Bash(al:*) Bash(cat:*)
---

# al dispatch

Use `al dispatch` as Agent Layer's headless second-agent control surface. This
skill provides routing, safety, and workflow rules; installed CLI help provides
command syntax and runtime contract details.

## Defaults

- Keep the target prompt focused on the second agent's job.
- Create no durable artifact unless the task needs one.

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
- Treat installed CLI help as the source of truth for commands, arguments,
  flags, defaults, and runtime contract details.
- If `al`, a target CLI, authentication, source skill, or generated skill
  projection is missing, stop and report the missing requirement. Do not
  install, authenticate, sync, or change configuration unless the user asked
  for setup or repair.

## Command routing

Use `al dispatch` only when the human explicitly asks for dispatch, subagent,
second-agent, or fresh-context reviewer work, or when another active skill's
written workflow explicitly requires delegation to a subagent or fresh-context
reviewer. Do not use it for discretionary/background second opinions or for
work the current agent can perform directly with local shell commands.

## Workflow

1. Inspect `al dispatch --help`.
2. Follow live help to inspect target metadata before a real run.
3. Choose the smallest target path that matches the user's intent.
4. Before starting a real dispatch, choose an execution mode that will not kill
   the command while the target agent is still working. Use a persistent or
   long-running shell/tool session, and avoid command wrappers or tool settings
   that enforce a hard timeout.
5. Verify any requested skill exists in the current project's Agent Layer skill
   source, or let `al dispatch` validation fail clearly. Do not invent a skill name.
6. Send the focused prompt to one target. Omit unrelated conversation
   history and information that could bias the target.
7. If a prompt file is needed, write it under `.agent-layer/tmp/`.

## Guardrails

- Do not use `al dispatch` for ordinary local shell work, tests, web retrieval,
  browser automation, or API-only tasks.
- Do not use `al dispatch` just because a second opinion might be nice.
- Do not use `al dispatch` to bypass the caller's restrictions, approvals, or
  sandbox expectations.
- After starting a dispatch, let the running `al dispatch` command finish.
  If the host tool separates output wait/yield duration from process lifetime,
  only the output wait/yield duration may be bounded; keep the same dispatch
  session alive until the original process exits. Use progress and the final
  result from that process, and do not inspect dispatch artifacts with separate
  commands while it is active.
- Do not retry failed target launches by guessing flags, targets, models, or
  reasoning-effort values. Re-check help and options, then report the mismatch
  if it remains unresolved.

## Definition of done

- Live help was checked for each command shape used.
- Current target metadata was inspected before any real run.
- Target selection and skill delivery if used were intentional and reported.
- Prompt and output artifacts were stored only under `.agent-layer/tmp/` and
  every created artifact path was reported.
- Missing targets, authentication, skill projection, or unsupported options were
  reported explicitly.
- Any side effects from target work were inspected and verified through the
  current task's normal checks.

## Final handoff

State the target selected, whether a skill was invoked, how the prompt was
provided, the useful result from the target, any artifacts created, and any
verification or `al dispatch` limits that could not be resolved.
