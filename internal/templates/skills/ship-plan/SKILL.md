---
name: ship-plan
description: >-
  Run a supplied implementation plan through local completion and PR shipping.
  Use when the caller provides plan, task, and context artifacts and wants
  `/fully-implement-plan` followed by `/ship-pr`.
---

# ship-plan

Orchestration-only skill. Manage inputs and delegate all work to
`/fully-implement-plan` and `/ship-pr`. Do not do delegated skill work yourself.

## Required inputs

Fail before side effects unless all are present:

- plan artifact path
- task artifact path
- context artifact path
- `implementer`: dispatch agent role to pass to both child skills
- `fixer`: dispatch agent role to pass to both child skills
- `plan_reviewers`: one or more dispatch agent roles to pass to both child skills

If any required input is missing, ask for it before starting. Do not invent
defaults, implementers, fixers, plan reviewer lists, or auto-select
artifacts from `.agent-layer/tmp/`.

## Context Discipline

You are the orchestrator for this skill. Preserve your context to validate
inputs, invoke the child skills, and relay their stop conditions. Treat child
skill returns as intermediate until this workflow reaches its final handoff.

## Rules

- Do not implement, review, clean up, verify, commit, push, monitor CI, reply to
  PR comments, or merge in this orchestration context.
- If `/fully-implement-plan` fails, stops at a checkpoint, reports a blocker, or
  cannot provide the report path and final status needed for handoff, stop this
  workflow and surface that child result.
- Run `/ship-pr` only after `/fully-implement-plan` reports a shippable final
  status: `complete` or acceptable `complete-with-follow-up`.
- Stop at any `/ship-pr` checkpoint, including merge authorization.

## Workflow

1. Run:

   ```text
   /fully-implement-plan
   Plan artifacts:
   {relative path to plan artifact}
   {relative path to task artifact}
   {relative path to context artifact}
   implementer is {implementer}
   fixer is {fixer}
   plan_reviewers are {agent 1, agent 2, ...}
   ```

   Record its report path, final status, stop reason, verification verdict, and
   residual risk.
2. Run:

   ```text
   /ship-pr
   implementer is {implementer}
   fixer is {fixer}
   plan_reviewers are {agent 1, agent 2, ...}
   ```

   Record its PR URL or checkpoint, comment ledger path if available, check
   status, and any merge authorization requirement.
3. Prepare the final message for the user.

## Definition of done

- `/fully-implement-plan` ran with the supplied plan, task, context,
  `implementer`, `fixer`, and `plan_reviewers`, or this workflow stopped on
  its child-skill blocker.
- `/ship-pr` ran after `/fully-implement-plan` completed with a shippable final
  status, or this workflow stopped before shipping with the reason.
- Final handoff reports the child skill report path, PR URL or checkpoint, and
  any blocker or required user decision.

## Final handoff

Report the `/fully-implement-plan` report path and final status. Report the
`/ship-pr` PR URL, current PR status, comment ledger path, and checkpoint when
available. State any blocker, residual risk, or user decision needed next.
