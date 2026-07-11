---
name: full-workflow
description: >-
  Align a feature specification, produce a reviewed plan, complete the local
  work, and ship the pull request.
---

# full-workflow

Own one end-to-end feature workflow from specification alignment through
`/plan-work` and `/ship-plan`.

## Required inputs

- the user's requested work
- `planner`: dispatch agent role for `/plan-work`
- `shipper`: dispatch agent role for `/ship-plan`
- `implementer`: dispatch agent role
- `fixer`: dispatch agent role
- `plan_reviewers`: one or more dispatch agent roles

Require every input before side effects. Do not infer roles or reviewer lists.

## Output artifact

Write `.agent-layer/tmp/full-workflow.<run-id>.spec.md` using
`run-id = YYYYMMDD-HHMMSS-<short-rand>`.

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

Read the focused repository context needed to distinguish facts from choices.
Write a concise spec that clearly captures the objective, scope and non-goals,
confirmed material decisions, constraints, acceptance criteria, shipping
expectations, and any unresolved user-owned decisions. Organize it for the work
at hand; these contents do not require ceremonial empty sections.

Ask only the questions required to resolve user-owned decisions. Record each
answer in the spec and resolve routine gaps directly from evidence.

Present one concise alignment summary covering scope, non-goals, acceptance
criteria, and material decisions. Continue to planning when repository evidence
settles the contract and no user-owned decision remains. Wait for approval only
when the user or caller explicitly requested an approval checkpoint, or when a
material choice actually belongs to the user. Apply corrections directly and
do not repeat alignment merely for confidence.

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
