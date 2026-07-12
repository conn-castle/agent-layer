---
name: agent-dispatch
description: Use `al dispatch` only when the user names an external dispatch target or another skill explicitly requires dispatch. Do not use it for generic subagent, second-agent, or fresh-context requests; use the built-in subagent instead.
compatibility: Requires the project Agent Layer CLI (`al`) and a provider whose exact installed version is reported as fresh-capable by `al dispatch options --json`.
allowed-tools: Bash(al:*) Bash(cat:*)
---

# al dispatch

## Required artifacts

- No artifact is required for an ordinary dispatch.
- Put caller-owned prompt or output files under `.agent-layer/tmp/` and report
  each created path.
- Agent Dispatch itself owns isolated evidence under
  `.agent-layer/tmp/runs/<run-id>/`; do not inspect it merely to wait.

## Global constraints

- Run `al dispatch --help` before the first real dispatch in a session and run
  subcommand help before using an unfamiliar command or flag.
- Before choosing a target, run `al dispatch options --json` against the current
  project. Use its separate `fresh`, `resume`, and `inspect` capability facts;
  never rely on a former `dispatch_capable` or streaming field.
- Start ordinary work with `al dispatch --agent <target> ...`. Ordinary calls
  always create a fresh provider conversation.
- Continue work only with `al dispatch resume <name> ...`, where `<name>` is
  the exact identity printed by the original successful dispatch. Never infer
  reuse from a target, prompt, role, artifact, or prior output.
- Standard output is the final answer only. Identity lines, capability changes,
  and failures are on standard error. Do not parse provider events or expect
  partial-answer progress.
- If a target, authentication, source skill, generated skill projection, or
  required capability is missing, stop and report it. Do not install,
  authenticate, sync, or change configuration unless the user asked for setup
  or repair.

## Workflow

1. Validate the requested target and optional skill from current metadata.
2. Build a focused prompt without unrelated history or biasing context.
3. Launch one fresh `al dispatch` invocation in a background-capable terminal
   and yield. Preserve its printed identity name with the task state.
4. Wait for its terminal notification. Do not poll, send empty input, inspect
   partial output, or infer failure from silence.
5. Inspect final output and required artifacts once after terminal completion.
   Use `al dispatch inspect <name>` only for a deliberate diagnosis; it reports
   transport facts and never makes a process healthy or retry-safe.
6. If explicit continuation is required, use the exact name with `resume`.
   A failure, missing report, or silence is not permission to launch a fresh
   replacement. Preserve ambiguous work and surface the blocker.

## Review-plan fanout

- Start the requested external reviewer dispatches before waiting for any one.
- Each reviewer starts exactly the three built-in perspectives from its review
  prompt before waiting, synthesizes them, and writes its required report.
- Replace a reviewer only after proven terminal infrastructure failure, all of
  its descendants terminal, and evidence that the retry is safe. Missing
  output or an ambiguous lifecycle blocks replacement.

## Guardrails

- Do not use Agent Dispatch for ordinary local shell work, tests, web
  retrieval, browser automation, or API-only tasks.
- Do not use it to bypass caller restrictions, approvals, or sandbox policy.
- Do not guess flags, target versions, models, effort values, provider session
  IDs, or a fallback conversation. Re-check help and options, then report the
  mismatch.
- `delete` removes only the Agent Layer mapping; it never deletes a
  provider-owned conversation or transcript.

## Definition of done

- Current capability metadata informed intentional target selection.
- The original invocation reached a terminal result without polling or
  partial-work inspection.
- Any explicit resume used its exact durable name.
- Created caller artifacts are reported, required output is verified, and
  failures or unresolved ambiguity are reported explicitly.
