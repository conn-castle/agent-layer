---
name: implement-plan
description: >-
  Apply an explicit plan/task/context artifact set and report the resulting
  implementation, deviations, and remaining work.
---

# implement-plan

Implement the supplied artifact contract once and return completed work or a
concrete blocker. Final verification is separate.

## Required inputs

- plan artifact path
- task artifact path
- context artifact path

Require exact paths. Do not discover or select artifacts from
`.agent-layer/tmp/`. Stop if an artifact is missing or unreadable.

Write `.agent-layer/tmp/implement-plan.<run-id>.report.md` using
`run-id = YYYYMMDD-HHMMSS-<short-rand>`.

## Rules

- Keep scope within the plan and task list.
- Resolve routine implementation details directly from the artifacts and
  repository evidence.
- Include planned documentation and memory updates. Do not defer them silently.
- Use narrow task-local checks when they help implement or debug the change.
  Do not turn this stage into broad or final verification.
- Do not add unrelated cleanup, a new planning cycle, or a review layer.

## Workflow

### 1. Preflight

Read the context, plan, and task artifacts. Confirm they describe the same
objective and scope, then inspect the named implementation entry points.

If a material ambiguity cannot be settled from repository evidence, identify
the user-owned decision before editing. Otherwise, proceed without another
readiness ceremony.

### 2. Implement

Execute the task list with localized, explainable changes. Split or reorder a
task when needed to implement the same scope safely, and record the adjustment.

When new information invalidates part of the plan, choose the smallest safe
response:

- continue and record an `equivalent` or `narrower` deviation
- stop for a user decision before a `broader` deviation
- report a concrete implementation blocker when work cannot continue

### 3. Report

Write a concise report with:

1. `## Status`
   - state whether the work is ready for verification or name the concrete
     blocker
2. `## Deviations`
   - record `equivalent`, `narrower`, and approved `broader` deviations with
     brief reasons; use `None` when there were none
3. `## Implementation Checks`
   - list task-local checks and results; use `None` when none ran
4. `## Remaining Work`
   - list incomplete planned work or deferred required updates with reasons;
     use `None` when nothing remains

## Completion contract

Return the report path, completed scope, checks, deviations, remaining work,
and readiness or a blocker after accounting for every plan item and required
docs/memory update. Do not narrate changes already clear from the diff.
