---
name: plan-work
description: >-
  Explicit-only.
  Produce and review an implementation-ready plan from a task source.
---

# plan-work

Create a reviewed, implementation-ready plan, task list, and context artifact.

## Inputs

Require a task source or user request and one or more self-contained
`plan_reviewers` target specifications to pass to `/review-plan`. Without a
task source or at least one valid reviewer, return a missing-input blocker and
create nothing.

## Workflow

1. Resolve material facts now rather than deferring investigation to
   implementation.
2. Follow `references/write-plan.md`. Correct evidence-backed gaps within this
   stage. Ask the user only when a substantive choice cannot be resolved under
   repository escalation rules.
3. Run `/review-plan` with the artifacts, optional source/spec, and
   `plan_reviewers`.

## Completion

Return the plan, task, context, and review-report paths with one status:
- `implementation-ready`: material findings are incorporated
- `blocked-for-user-decision`: name the unresolved substantive decision
