---
name: fully-implement-plan
description: >-
  Implement supplied plan/task/context artifacts, clean the working tree,
  verify the contract once, and repair material verification findings.
---

# fully-implement-plan

Coordinate implementation, one cleanup pass, one verification pass, and any
required repair. Do not open a PR or run an unrelated full-repository lane.

## Inputs and artifact

Require exact plan, task, and context artifact paths plus `implementer` and
`fixer` dispatch roles. Do not infer them. Write
`.agent-layer/tmp/fully-implement-plan.<run-id>.report.md`.

Dispatch external roles through `/agent-dispatch`. Treat the supplied artifacts
as the contract; delegated reports are evidence and cleanup findings are
supplemental obligations. Serialize mutations against the latest tree and stop
on a failed delegation or missing required report/verdict.

Continue through reversible in-scope repairs and non-destructive checks. Ask
only for external writes not already authorized, destructive actions,
substantive product/architecture choices, or material scope expansion. Do not
stage, commit, weaken checks, or destructively rewrite changes.

## Workflow

### 1. Implement and clean

Dispatch `implementer` with `/implement-plan` and the artifacts. Record its
report, deviations, checks, remaining work, and readiness.

Dispatch `fixer` with `/clean-and-fix-code` for one cleanup pass. Record its
report, resolved findings, focused evidence, and residual risk.

### 2. Verify once

Run `/verify-work` once in a fresh built-in subagent against the original
artifacts, passing cleanup findings as supplemental obligations. Record its
verdict, material findings, evidence, and next step. Treat
`complete-with-follow-up` as complete only when all follow-up is outside the
contract.

### 3. Repair verification findings

When incomplete, validate every material in-scope finding against the contract
and tree; discard only with repository evidence. If no genuine user decision
blocks repair, dispatch `implementer` once with the original artifacts,
verification report, confirmed findings, required updates/checks, and required
report path
`.agent-layer/tmp/fully-implement-plan.<run-id>.verification-repair.report.md`.

The implementer owns all repairs and records each finding as `resolved`,
`invalid-with-evidence`, or `blocked`, with focused checks and final diff
assessment. Do not replan, rerun implementation, cleanup, review, or verification
for confidence.

## Completion contract

Report inputs, implementation, cleanup, verification, repairs, final evidence,
and residual risk. Return:

- `complete`: verification passed or every in-scope finding was repaired
- `complete-with-follow-up`: only explicit out-of-contract work remains
- `blocked`: name the concrete failure or genuine user decision
