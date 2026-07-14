---
name: review-plan
description: >-
  Review and repair a plan/task/context artifact set with independent evidence,
  then report implementation readiness.
---

# review-plan

Review a plan independently; the owner synthesizes, edits, and decides
readiness.

## Required inputs

Require:

- exactly three `plan_reviewers` as self-contained dispatch target specifications
- plan, task, and context artifact paths
- an optional specification artifact path

Before dispatch, show every exact reviewer target to the user and ask for any
missing target; do not infer target specifications. Missing artifacts or a
reviewer count other than exactly three block review.

## Output artifact

Write `.agent-layer/tmp/review-plan.<run-id>.report.md` with run ID
`YYYYMMDD-HHMMSS-<short-rand>`. Preserve canonical reviewer results as evidence.

## Independence contract

Every reviewer receives complete, equivalent copies of
`assets/agent-review-prompt.md`, plan, task, context, and optional spec. Only
provider mechanics, target, and run identity may differ. Never share reviewer
outputs or synthesis between reviewers.

## Workflow

Read all artifacts and confirm objective/scope alignment. Build one shared
prompt; do not assign complementary coverage.

Run the three independent reviews concurrently through dispatch fanout.
Retry an unusable result only through its same supplied target; do not replace a
required reviewer with an unspecified or inferred target.

Validate candidates against artifacts and repository evidence. Merge duplicates
and retain material correctness, safety, scope, implementability, verification,
or maintainability gaps.

Apply accepted corrections and update direct dependencies. Escalate only under
the repository's human-escalation rules.

Report sources, accepted changes, unresolved decisions, and exactly one value:

- `implementation-ready`
- `blocked-for-user-decision`

Finish after evaluating all reports and applying accepted corrections. Return
evidence paths, changes, genuine user decisions, and readiness.
