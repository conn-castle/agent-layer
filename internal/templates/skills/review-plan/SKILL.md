---
name: review-plan
description: >-
  Explicit-only.
  Review and repair a plan/task/context artifact set with independent evidence,
  then report implementation readiness.
---

# review-plan

## Required inputs

Require:

- one or more `plan_reviewers` as self-contained dispatch target specifications
- plan, task, and context artifact paths
- an optional specification artifact path

Resolve each supplied reviewer request through `/agent-dispatch`'s live
metadata. When it matches exactly one dispatchable target/model configuration,
use that match without asking for confirmation. Missing artifacts block review,
as does an empty reviewer list.

## Output artifact

Write `.agent-layer/tmp/review-plan.<run-id>.report.md` with run ID
`YYYYMMDD-HHMMSS-<short-rand>`. Preserve canonical reviewer results as evidence.

## Independence contract

Every reviewer receives complete, equivalent copies of
`references/agent-review-prompt.md`, plan, task, context, and optional spec.

## Workflow

Run all supplied independent reviews concurrently through dispatch fanout.

Validate findings against artifacts and repository evidence. Merge duplicates
and retain valid findings. Update the artifacts directly to fully address valid
findings.

Once done, report one of the following to the caller:
- `implementation-ready`
- `blocked-for-user-decision`
