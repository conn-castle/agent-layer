---
name: plan-work
description: >-
  Produce and review an implementation-ready plan from a task source.
---

# plan-work

Create a reviewed, implementation-ready plan, task list, and context artifact.

## Inputs

Require a task source or user request and exactly three self-contained
`plan_reviewers` target specifications to pass to `/review-plan`. Before
creating artifacts, show the user every exact reviewer target and ask for any
missing target; do not infer target specifications. Without a task source or
the required reviewers, return a missing-input blocker and create nothing.

## Workflow

1. Read the source and the smallest repository context needed to understand the
   target. Resolve material facts now rather than deferring investigation to
   implementation. For broad cross-system work, bounded scouts may map entry
   points, contracts, dependencies, and unresolved facts; the planner validates
   consequential evidence and owns the result.
2. Follow [`assets/write-plan.md`](assets/write-plan.md) with the original source
   and gathered evidence. Correct evidence-backed gaps within this stage. Ask
   the user only when a substantive choice cannot be resolved under repository
   escalation rules.
3. Run `/review-plan` with the plan, task, context, optional source/spec, and
   `plan_reviewers`. Use the revised artifacts it returns. Reuse still-valid
   evidence and resolve routine review uncertainty autonomously.

Do not edit implementation code or invent missing facts.

## Completion

Return the plan, task, context, and review-report paths with one status:

- `implementation-ready`: material findings are incorporated
- `blocked-for-user-decision`: name the unresolved substantive decision
