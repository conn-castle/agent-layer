---
name: plan-work
description: >-
  Produce an implementation-ready plan from a task source.
---

# plan-work

## Required inputs

- task source or user request
- `plan_review_agents` to pass to `/review-plan`

If either is missing, ask for it before writing artifacts or running review.

## Workflow

1. Resolve missing material facts with the smallest read-only investigation
   needed for planning. Then write plan artifacts. If a user-owned decision or
   larger investigation is required, stop and surface the blocker. Never defer
   investigation into the plan.
2. Load and follow `assets/write-plan.md` in the current `/plan-work` run, using
   the original task source plus the already-completed investigation findings
   that shape the plan. Do not delegate this to a subagent.
3. Continue only after the loaded planning prompt returns plan, task, and
   context artifact paths. If its verdict is `escalate`, stop and surface the
   checkpoint.
4. Run:

   ```text
   /review-plan
   Plan artifacts:
   {relative path to plan artifact}
   {relative path to task artifact}
   {relative path to context artifact}
   {relative path to source/spec artifact, if supplied}
   plan_review_agents are {agent 1, agent 2, ...}
   ```
5. If review changes artifacts, use the revised artifacts. If review blocks on a
   user decision, ask and rerun the smallest necessary step.

## Guardrails

- Do not edit implementation code.
- Do not invent missing required inputs.

## Definition of done

- Success: `/review-plan` final readiness is `implementation-ready`.
- Blocked: `/review-plan` final readiness is `blocked-for-user-decision` and the
  handoff names the decision.
- Final handoff includes plan, task, context, and review report paths when they
  exist.
