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
`plan_reviewers` target specifications. Resolve each through `/agent-dispatch`'s
live metadata; use an unambiguous match without confirmation. Missing task
input, an empty reviewer list, or an unresolved reviewer blocks planning;
create nothing.

## Workflow

1. Resolve material facts now rather than deferring investigation to
   implementation.
2. Follow `references/write-plan.md`. Correct evidence-backed gaps within this
   stage. Ask the user only when a substantive choice cannot be resolved under
   repository escalation rules.
3. Dispatch all supplied reviewers concurrently with `/review-plan`. Give each
   the complete artifacts and optional source/spec.
4. Merge duplicates, then assess each finding for correctness, relevance, and
   materiality. Accept or reject each; briefly justify rejections.
5. Address accepted findings in the artifacts. Escalate unresolved substantive
   decisions.
6. Write `.agent-layer/tmp/plan-work.<run-id>.report.md` using the plan artifact
   run ID. Include reviewer finding paths, dispositions, rejection reasons,
   unresolved decisions, and status.

## Completion

Return the plan, task, context, and report paths with one status:

- `implementation-ready`: accepted findings are addressed
- `blocked-for-user-decision`: name the unresolved substantive decision
