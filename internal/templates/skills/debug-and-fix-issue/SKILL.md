---
name: debug-and-fix-issue
description: >-
  Reproduce an unexplained bug, prove its root cause, capture a failing test or
  diagnostic blocker, and dispatch one proportional repair and verification.
---

# debug-and-fix-issue

Turn a symptom into a failing test and causal diagnosis, or an explicit
diagnostic blocker, then repair the proven root cause.

## Inputs and boundaries

Require a testable symptom. Fix mode also requires `implementer` and `fixer`
dispatch roles; require `plan_reviewers` and `code_reviewer` only for the
planned repair path.
Accept reproduction evidence, suspect paths, regression range, and
diagnosis-only mode.

Before mutation, record the current head plus staged, unstaged, and untracked
state. Use it to isolate the repair diff from pre-existing work with
path-specific boundaries; stop only when an attempted isolation still leaves
repair and pre-existing changes in the same files, and name those paths.

Write `.agent-layer/tmp/debug-and-fix-issue.<run-id>.report.md`; direct repair
also writes the same prefix with `.direct-repair.report.md`.

- Dispatch external roles through `/agent-dispatch`; the investigation context
  does not implement the repair.
- Use one fresh investigator for reproduction, diagnosis, and failing test
  unless live non-transferable state requires the current context. Give it the
  original symptom, evidence, constraints, and report path.
- Require observed reproduction, code paths, experiments, logs, or
  authoritative dependency behavior. Unknown root cause means not fixed.
- Reject symptom-masking retries, sleeps, timeout inflation, broad catches,
  silenced errors, ignored validation, and weakened assertions unless evidence
  proves they are the contract.
- Do not change a test expectation without evidence or stage, commit, discard,
  or destructively rewrite changes.

## Workflow

### 1. Reproduce and diagnose

Read COMMANDS.md and relevant issue/decision context. Follow supplied steps or
build the smallest reproducer; capture expected and observed behavior, command,
environment, and output. If it cannot reproduce, return the evidence and
smallest missing input.

Trace the failing path and run only experiments that distinguish plausible
causes. Record the defective condition, causal chain, excluded alternatives,
useful introducing change, and rejected masking fixes. If causes remain
plausible, add only discriminating instrumentation and return `diagnosed-only`.

### 2. Capture the failing test

Create or refine one behavioral reproducer and confirm it fails for the
diagnosed defect. When automation is impossible, record why and the alternative
evidence. In diagnosis-only mode, update ISSUES.md when required, use
the repository's memory format, avoid duplicate entries, and yield without a
separate closeout or verification stage.

### 3. Choose and run one repair path

Validate the investigator's causal claim. Use `direct` for a concrete root
cause, desired behavior, boundary, and verification. Use `planned` only for
material multi-system, architecture, behavior, migration, or risk changes. Ask
only for a genuine material choice.

For `direct`, dispatch `implementer` with the report, request, root cause,
failing test, and repair-report path. Require the root-cause fix, green test,
required updates, and focused checks. Dispatch `fixer` once with
`/clean-and-fix-code`, the exact repair diff boundary, and the pre-mutation
inventory. Continue only on `completed` or `no-findings`; stop on `blocked`.
Then run `/verify-work` once in a fresh subagent against the original request.
If incomplete, validate and dispatch `implementer` once for material in-scope
findings. When that dispatch mutates the tree, run one final targeted
`/verify-work` against the original request and accepted findings; do not rerun
cleanup or review. Return `fixed` only for a final `complete` verdict or
`complete-with-follow-up` whose follow-up is outside the contract.

For `planned`, run `/plan-work` once from the debug report and then
`/fully-implement-plan` once with its artifacts and roles. When root cause is
unknown, plan only diagnostic instrumentation.

## Completion contract

Report `fixed`, `diagnosed-only`, or `blocked`; include the symptom, root cause
or missing fact, failing test or diagnostic evidence, repair artifacts,
verification, residual risk, and report path. State explicitly when unfixed.
