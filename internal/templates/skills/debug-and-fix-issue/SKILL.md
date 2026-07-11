---
name: debug-and-fix-issue
description: >-
  Reproduce an unexplained bug, prove its root cause, capture a failing test or
  diagnostic blocker, and dispatch one proportional repair and verification.
---

# debug-and-fix-issue

Turn an unexplained symptom into evidence, then repair the proven root cause.
Investigation is complete when it produces a failing test and causal diagnosis,
or an explicit diagnostic blocker—not when every hypothesis has been explored.

## Inputs and artifacts

Require a symptom specific enough to support a testable hypothesis.

For fix mode, require `implementer` and `fixer` dispatch roles before repair.
Require `plan_reviewers` only if the proven repair is significant enough to use
the planned path. Diagnosis-only mode does not require agent roles.

Accept reproduction steps, expected and actual behavior, errors or traces,
suspect paths, a regression range, and diagnosis-only mode.

Write `.agent-layer/tmp/debug-and-fix-issue.<run-id>.report.md`, where `run-id`
is `YYYYMMDD-HHMMSS-<short-rand>`. A direct repair also writes
`.agent-layer/tmp/debug-and-fix-issue.<run-id>.direct-repair.report.md`.

## Rules

- Dispatch external roles through `/agent-dispatch`; do not implement the repair
  in the investigation context.
- Base the diagnosis on observed reproduction, code paths, experiments, logs,
  and authoritative dependency behavior.
- Repair the root cause before defensive hardening. Do not accept retries,
  sleeps, timeout inflation, broad catches, error silencing, ignored validation,
  or weaker assertions unless evidence proves that behavior is the contract.
- Do not change a test expectation unless repository evidence proves the
  expectation wrong.
- If the root cause remains unknown, the issue is not fixed. Return the exact
  instrumentation or evidence needed to distinguish the remaining hypotheses.
- Do not stage, commit, discard, or destructively rewrite changes.

## Workflow

### 1. Reproduce

Read COMMANDS.md and relevant issue and decision context. Follow supplied
reproduction steps first; otherwise build the smallest credible reproducer.
Capture expected behavior, observed behavior, command, environment facts, and
output.

If targeted attempts cannot reproduce the symptom, stop with the evidence and
the smallest missing input. Do not speculate a repair.

### 2. Narrow and diagnose

Trace the failing path and test only hypotheses that distinguish plausible
causes. Use history, binary search, focused experiments, dependency research,
or temporary instrumentation when the evidence warrants them.

Record:

- the defective condition and exact location
- the causal chain from input to symptom
- experiments or evidence that exclude competing causes
- the introducing change when established and useful
- symptom-masking fixes rejected by the evidence

If multiple causes remain plausible, add only the diagnostic instrumentation
needed to separate them, record `diagnosed-only`, and stop.

### 3. Capture the failing test

Refine an automated reproducer or write one focused behavioral test. Confirm it
fails because of the diagnosed defect. Skip this only when the root cause is
unknown or the behavior cannot be automated; record the concrete reason and
alternative evidence.

In diagnosis-only mode, update ISSUES.md when repository policy requires a
durable entry, run `/finish-task` only when no broader orchestrator owns
closeout, and yield.

### 4. Choose one repair path

Use `direct` when the root cause, desired behavior, boundary, and verification
are concrete. Use `planned` only when the repair materially changes several
subsystems, architecture, behavior, migration, or risk and therefore benefits
from an explicit reviewed plan. Severity alone does not require planning.

Ask the user before either path only when evidence leaves a user-owned behavior,
architecture, scope, risk, or cost choice.

### 5A. Dispatch a direct repair

Dispatch `implementer` with the debug report, original request, bounded root
cause, failing test, and direct-repair report path. Require the root-cause fix,
the failing test to turn green, directly required updates, and focused affected
checks.

Dispatch `fixer` once with `/clean-and-fix-code`. Then run `/verify-work` once
in a fresh built-in subagent against the original request, passing the debug and
repair reports as evidence and cleanup `resolved_findings` as supplemental
obligations.

If verification is incomplete, validate its material in-scope findings and
dispatch `implementer` once to address them directly with focused evidence.
Do not rerun cleanup or verification merely to gain confidence. Return a
blocker only for a remaining concrete failure or user-owned decision.

### 5B. Dispatch a significant planned repair

Run `/plan-work` once from the debug report with `plan_reviewers`, then run
`/fully-implement-plan` once with the returned artifacts, `implementer`, and
`fixer`. Do not plan a behavior fix when the report says the root cause is
unknown; plan only the diagnostic instrumentation needed to obtain it.

### 6. Report and yield

The report contains:

1. `# Debug Summary` — `fixed`, `diagnosed-only`, or `blocked`
2. `## Reproduction`
3. `## Investigation and Root Cause`
4. `## Failing Test or Diagnostic Evidence`
5. `## Repair Path and Artifacts`
6. `## Verification`
7. `## Residual Risk and Follow-up`

Return the report path, symptom, root cause or missing fact, failing test,
repair artifacts, verification verdict, and terminal outcome. State explicitly
when the issue is not fixed.
