---
name: review-plan
description: >-
  Review and repair a plan/task/context artifact set in one purposeful pass
  through the requested plan reviewers, then report implementation readiness.
---

# review-plan

Run one bounded pre-implementation review. Dispatch each requested reviewer
once, synthesize their evidence-backed findings, revise accepted material gaps,
and return a readiness verdict. The current agent owns synthesis, artifact
changes, and the final decision.

## Required inputs

- `plan_reviewers`: one or more dispatch agent roles
- plan artifact path
- task artifact path
- context artifact path

A spec artifact path is optional. When supplied, use it as the review contract;
otherwise use the plan's stated objective and scope.

Fail before dispatch if a required input is missing or unreadable.

## Output artifacts

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>` and write:

- one child report per reviewer at
  `.agent-layer/tmp/review-plan.<run-id>.<reviewer-index>-<role-slug>.report.md`
- the final report at `.agent-layer/tmp/review-plan.<run-id>.report.md`

Treat completed child reports as immutable evidence for synthesis.

## Rules

- Dispatch external reviewer roles through `/agent-dispatch`.
- Dispatch every requested reviewer once. Do not run another review round after
  revising the artifacts.
- Validate findings against the supplied artifacts and relevant repository
  evidence. Reviewer agreement does not strengthen or replace evidence.
- Keep only findings that materially affect correctness, safety, scope,
  implementability, verification, or meaningful maintainability.
- Classify material findings as `accepted` or `user-decision`. Unsupported,
  duplicate, stylistic, speculative, and immaterial suggestions remain in the
  immutable child reports but do not enter the final findings ledger.
- Resolve routine planning and verification details directly. Ask the user only
  when available evidence leaves a choice that materially affects behavior,
  architecture, scope, risk, or cost.

## Workflow

### 1. Preflight

Read the plan, task, context, and optional spec. Confirm that they describe the
same objective and scope before dispatching reviewers.

### 2. Dispatch reviewers once

Give each reviewer `assets/agent-review-prompt.md`, the artifact paths, and a
unique child report path. Start all reviewer dispatches before waiting for their
results. Validate that each completed report follows the reviewer contract.

### 3. Synthesize and revise

Evaluate each reported finding against the artifacts and repository evidence,
apply the materiality threshold, and merge duplicates. Do not use reviewer
consensus as a deciding factor.

Make one revision pass for accepted findings. Then inspect the changed clauses
and their direct dependents for internal consistency. This is part of applying
the findings, not a new review round.

If a genuine user-owned decision remains, stop and ask for the smallest choice
that unblocks the plan. Otherwise, continue without interruption.

### 4. Report readiness

Write the final report with:

- reviewed artifact paths and reviewer roles
- accepted changes
- unresolved user-owned decisions
- final readiness

Final readiness must be exactly one of:

- `implementation-ready`
- `blocked-for-user-decision`

## Guardrails

- Do not widen scope or weaken verification to satisfy reviewer preference.
- Do not let stylistic improvements or speculative edge cases block progress.
- Do not dispatch more reviewers to increase confidence in an evidence-settled
  conclusion.

## Definition of done

- Every requested reviewer was dispatched once and produced one child report.
- Every material finding was applied or identified as a user-owned decision.
- The final report records the artifact changes and readiness verdict.

## Final handoff

Return the final report path, accepted changes, any user-owned decision, and the
readiness verdict.
