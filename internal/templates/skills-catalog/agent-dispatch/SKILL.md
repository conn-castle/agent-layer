---
name: agent-dispatch
description: Use `al dispatch` only when the user names an external dispatch target or another skill explicitly requires dispatch. Do not use for generic subagent, second-agent, and fresh-context requests; use the built-in subagent instead.
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
- Before resolving any role or making the first real dispatch in every session, run
  `al dispatch options --json` against the current project. Do not rely on
  remembered, cached, or prior-session options output.
- Resolve each requested role to one exact invocation: target plus optional
  full model and reasoning-effort values. Infer a target from a model only when
  the options output makes the mapping unique.
- Treat the selected target's non-empty `model.configured` value and exact
  `model.suggestions` entries in that live output as the complete allowlist for
  an explicit `--model` override. `allow_custom: true` never expands this
  allowlist. Never shorten or reinterpret a model name, guess an unsupported
  value, or silently substitute a fallback. Fail before launch when the
  request is ambiguous or its model is absent from that allowlist, and report
  the permitted model names.
- When no model is requested, omit `--model` and use the target's reported
  configured default; do not synthesize an override.
- If `al`, a target CLI, authentication, source skill, or generated skill
  projection is missing, stop and report the missing requirement. Do not
  install, authenticate, sync, or change configuration unless the user asked
  for setup or repair.

## Workflow

1. Normalize and validate the requested role under `Global constraints`.
2. Verify any requested skill exists in the current project's Agent Layer skill
   source. Do not invent a skill name.
3. Send the focused prompt to one target. Omit unrelated conversation
   history and information that could bias the target.
4. If a prompt file is needed, write it under `.agent-layer/tmp/`.
5. Run one foreground `al dispatch` call and let that original call finish,
   even when output is quiet or the target appears stalled.
   - Do not background the process or create a monitoring loop.
   - Do not poll the process, send empty-input status checks, inspect partial
     output, or read child artifacts while the call is running.
   - Do not narrate quiet progress from the child. The blocking dispatch call
     is the wait.
   - End the wait only when the original call returns, the user explicitly
     interrupts it, or the runtime reports a terminal process failure.
6. After the call returns, inspect the final result and completed artifacts
   once, then continue the caller's workflow.

## Guardrails

- Do not use `al dispatch` for ordinary local shell work, tests, web retrieval,
  browser automation, or API-only tasks.
- Do not use `al dispatch` to bypass the caller's restrictions, approvals, or
  sandbox expectations.
- Do not retry failed target launches by guessing flags, targets, models, or
  reasoning-effort values. Re-check help and options, then report the mismatch
  if it remains unresolved.
- When a caller launches an independent batch concurrently, apply this complete
  blocking contract to every invocation and collect only terminal results.

## Definition of done

- Current target metadata was inspected before any real run.
- Target selection and skill delivery if used were intentional and reported.
- Prompt and output artifacts were stored only under `.agent-layer/tmp/` and
  every created artifact path was reported.
- Errors were reported explicitly.
- The original dispatch call reached a terminal result without polling or
  partial-work inspection.
- Any side effects from target work were inspected and verified through the
  current task's normal checks.
