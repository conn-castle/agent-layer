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

For a broad or cross-subsystem target whose investigation would consume
substantial planning context, use one fresh built-in scout subagent. Give it the
task source and bounded research questions. Require a compact evidence map of
entry points, contracts, dependencies, constraints, and unresolved facts with
exact repository locations. The planning agent validates consequential evidence
and owns the plan; a narrow target does not require a scout.

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

When `write-plan` returns `revise`, apply its cited corrections in one focused
revision and rerun only the artifact self-check. Resolve any concrete,
autonomously correctable material gap that self-check still exposes without
restarting research, drafting, or review. Escalate only when the remaining
choice materially affects behavior, architecture, scope, risk, or cost and
available evidence cannot settle it.

### 3. Review once

Call `/review-plan` once with the plan, task, context, optional source/spec, and
`plan_reviewers`. Use the revised artifacts it returns.

If review returns `blocked-for-user-decision`, ask the named question, then
resume the same review stage with the answer so it updates the affected
artifacts and final report. Do not redispatch reviewers or start another review
round merely because the artifacts changed to record that decision.

## Boundaries

- Do not edit implementation code.
- Do not invent missing inputs or unresolved facts.

## Completion contract

Return the plan, task, context, and review report paths with exactly one status:

- `implementation-ready`: the artifacts incorporate material review findings
- `blocked-for-user-decision`: name the unresolved decision
