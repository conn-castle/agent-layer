---
name: full-workflow
description: >-
  Align a feature specification, produce a reviewed plan, complete the local
  work, and ship the pull request.
---

# full-workflow

Align one feature specification, plan it, and ship it.

## Required inputs

- the user's requested work
- `planner`: dispatch agent role for `/plan-work`
- `shipper`: dispatch agent role for `/ship-plan`
- `implementer`: dispatch agent role
- `fixer`: dispatch agent role
- `plan_reviewers`: one or more dispatch agent roles

Require every input before side effects. Do not infer roles or reviewer lists.

Write `.agent-layer/tmp/full-workflow.<run-id>.spec.md`.

## Rules

- Dispatch external roles through `/agent-dispatch`.
- Keep user questions, summaries, and approvals in chat; artifacts must not be
  the only presentation of a user-owned decision.
- Resolve repository facts and routine implementation details without asking
  the user.
- Ask only when multiple viable choices materially affect behavior,
  architecture, scope, risk, cost, or shipping.
- Treat the aligned spec as the workflow contract. Do not widen it silently.
- If a child skill fails or omits its required artifact or verdict, stop and
  report the concrete blocker.

## Workflow

### 1. Align the specification

Read enough repository context to distinguish facts from choices. Write a
concise spec with objective, scope/non-goals, material decisions, constraints,
acceptance criteria, shipping expectations, and unresolved decisions; omit
ceremonial empty sections.

When the request is to complete a roadmap phase, read ROADMAP.md first. Use the
named phase or the first incomplete phase, and include every unchecked task,
exit criterion, and direct prerequisite. Include necessary ROADMAP.md, memory,
and documentation closeout in the contract. If repository evidence shows the
phase is already complete, update only stale project truth and return the
evidence instead of planning or creating an empty pull request.

Ask only the questions required to resolve user-owned decisions. Record each
answer in the spec and resolve routine gaps directly from evidence.

Present one alignment summary and continue when evidence settles the contract.
Wait only for a requested checkpoint or genuine material choice; apply routine
corrections without repeating alignment.

### 2. Plan once

Dispatch `planner` with `/plan-work`, the aligned spec path, and
`plan_reviewers`. Require the plan, task, context, and review report paths with
`implementation-ready` status.

If planning uncovers a user-owned decision, relay the exact question, record the
answer in the spec, and let the planning stage update the affected artifacts.
Do not restart spec alignment or plan review unless the changed contract
invalidates that completed work.

### 3. Implement and ship

Dispatch `shipper` with `/ship-plan`, the reviewed artifact paths,
`implementer`, and `fixer`. Relay any explicit child checkpoint, including
shipping or merge authorization, and resume the same stage after the user
answers.

### 4. Report

Return the spec, plan, task, context, and review report paths together with the
`/ship-plan` final handoff.

## Completion contract

The workflow is complete when the aligned spec is represented by an
implementation-ready plan and `/ship-plan` returns its terminal result. Report
all artifact paths, pull request status when available, and any blocker or
remaining user decision.
