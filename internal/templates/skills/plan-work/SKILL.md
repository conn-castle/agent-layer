---
name: plan-work
description: >-
  Produce and review an implementation-ready plan from a task source.
---

# plan-work

Create one implementation-ready artifact set, run one plan review, and return
the reviewed artifacts or a user-owned blocker.

## Required inputs

- task source or user request
- `plan_reviewers` to pass to `/review-plan`

Ask for a missing required input before creating artifacts.

## Workflow

### 1. Establish the planning contract

Read the task source and the smallest amount of repository context needed to
resolve material facts. Do not defer factual investigation into the plan.

Resolve routine planning choices from repository evidence. Ask the user only
when multiple viable choices would materially change behavior, architecture,
scope, risk, or cost.

### 2. Write the artifacts

Load and follow `assets/write-plan.md` in the current agent context. Give it the
original task source and the evidence gathered during preflight. It must return
plan, task, and context artifact paths.

If the drafting result exposes a correctable material gap, fix it within this
planning stage. If it exposes a user-owned decision, stop and ask for that
decision. Do not delegate planning or start repeated drafting passes for greater
confidence.

When `write-plan` returns `revise`, apply its cited correction once and rerun
only the artifact self-check. Do not involve the user. Escalate only when the
remaining choice materially affects behavior, architecture, scope, risk, or
cost and available evidence cannot settle it.

### 3. Review once

Call `/review-plan` once with the plan, task, context, optional source/spec, and
`plan_reviewers`. Use the revised artifacts it returns.

If review returns `blocked-for-user-decision`, ask the named question and update
the affected artifacts after the answer. Do not rerun the review merely because
the artifacts changed to record that decision.

## Boundaries

- Do not edit implementation code.
- Do not invent missing inputs or unresolved facts.

## Completion contract

Return the plan, task, context, and review report paths with exactly one status:

- `implementation-ready`: the artifacts incorporate material review findings
- `blocked-for-user-decision`: name the unresolved decision
