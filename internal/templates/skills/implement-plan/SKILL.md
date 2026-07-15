---
name: implement-plan
description: >-
  Explicit-only.
  Apply an explicit plan/task/context artifact set and report the resulting
  implementation, deviations, and remaining work.
---

# implement-plan

Implement the supplied artifact contract and return work ready for separate
verification, or a concrete blocker.

## Inputs and boundaries

Require exact plan, task, and context artifact paths. Do not discover or select
artifacts from `.agent-layer/tmp/`. Confirm each path before reporting it
missing. Write `.agent-layer/tmp/implement-plan.<run-id>.report.md`, using
`YYYYMMDD-HHMMSS-<short-rand>` for `run-id`.

Keep scope within the artifacts, including required documentation and memory
updates. Resolve routine details from repository evidence. Do not add unrelated
cleanup, another planning cycle, or a review layer.

## Workflow

1. Read the artifacts, confirm one objective and scope, and inspect named entry
   points. Resolve ambiguity from repository evidence; ask only for a genuine
   user decision.
2. Implement every task with localized changes, reordering or splitting work
   when needed for the same scope. Record plan adjustments as `equivalent` or
   `narrower`; get approval before materially broader work.
3. Run proportionate checks, using the documented full lane when risk or the
   contract warrants it. Address concrete in-scope failures.

Stop with `blocked` only when planned work cannot safely continue without
missing authoritative input, unsafe overlap, or a user-owned decision.

## Completion contract

Report the artifact paths, completed scope, checks and results, deviations,
remaining work, changed files, and readiness for verification or the concrete
blocker. Account for every plan item and required documentation or memory
update; avoid narrating details already clear from the diff.
