---
name: implement-plan
description: >-
  Explicit-only.
  Apply an explicit plan/task/context artifact set and report the resulting
  implementation, deviations, and remaining work.
---

# implement-plan

Implement the supplied artifact contract or return a concrete blocker.

## Inputs and boundaries

Require exact plan, task, and context artifact paths. Write the output
report `.agent-layer/tmp/implement-plan.<run-id>.report.md`, using
`YYYYMMDD-HHMMSS-<short-rand>` for `run-id`.

## Workflow

1. Read the artifacts. Resolve ambiguity from repository evidence; ask only
   for a genuine user decision.
2. Implement every task, reordering or splitting work when needed. Record
   plan adjustments as `equivalent` or `narrower`; get approval before
   materially broader work.
3. Run proportionate checks, using the documented full lane when risk or the
   contract warrants it. Address concrete in-scope failures.
4. Confirm there is no remaining work to be done.

Stop with `blocked` only when planned work cannot safely continue or
requires a user-owned decision.

## Completion contract

Return the relative path to the output report, a summary, and any additional
information that should be shared with a reviewer.
