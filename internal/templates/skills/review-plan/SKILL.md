---
name: review-plan
description: >-
  Review and repair a plan/task/context artifact set through the requested plan
  reviewers, then report implementation readiness.
---

# review-plan

Run a bounded pre-implementation review. Dispatch the requested reviewers,
synthesize their evidence-backed findings, revise accepted material gaps, and
return a readiness verdict. The current agent owns synthesis, artifact changes,
and the final decision.

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
- Require one terminal report per requested reviewer for the reviewed artifact
  version. Replace a proven terminal infrastructure failure only when every
  descendant is terminal and evidence identifies a retryable cause. An
  ambiguous lifecycle or evidence-equivalent failure is a blocker; never retry
  to seek another opinion.
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

### 2. Dispatch reviewers

Give each reviewer `assets/agent-review-prompt.md`, the artifact paths, and a
unique child report path. Start all reviewer dispatches before waiting for their
results. Each dispatched reviewer owns the bounded fresh-context perspectives
required by that prompt and synthesizes them into its child report. Validate
that each completed report follows the reviewer contract.

### 3. Synthesize and revise

Evaluate each reported finding against the artifacts and repository evidence,
apply the materiality threshold, and merge duplicates. Do not use reviewer
consensus as a deciding factor.

Apply every accepted finding, then inspect the changed clauses and evidence they
invalidate, including direct dependents, for internal consistency. Correct
resulting gaps within this synthesis stage. Do not redispatch reviewers merely
because their accepted findings changed the artifacts; review a new artifact
version only when its contract or material scope changed independently.

If a genuine user-owned decision remains, record it in the final report with
`blocked-for-user-decision`, stop, and ask for the smallest choice that
unblocks the plan. After the answer, resume this same synthesis stage, apply the
decision to the affected artifacts and direct dependents, and continue without
redispatching reviewers.

### 4. Report readiness

Write or update the final report with:

- reviewed artifact paths and reviewer roles
- accepted changes
- unresolved user-owned decisions
- final readiness

Final readiness must be exactly one of:

- `implementation-ready`
- `blocked-for-user-decision`

Do not widen scope, weaken verification, or add reviewers to satisfy preference
or seek confidence. Return the final report path, accepted changes, any genuine
user decision, and the readiness verdict after every requested reviewer has a
terminal report and every material finding is resolved or blocked on that
decision.
