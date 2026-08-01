---
name: review-uncommitted-code
description: >-
  Explicit-only.
  Report-only review of a concrete file, directory, diff, range, working tree,
  or proactive hotspot for material correctness, risk, architecture, test,
  documentation, performance, reliability, and maintainability findings.
---

# review-uncommitted-code

## Target and artifact

Use the explicit target, then requested hotspots, otherwise all working-tree
changes. Review the last commit only when asked. Choose hotspots from churn,
complexity, weak coverage, scaffolding, reliability boundaries, or contract
drift.

Write `.agent-layer/tmp/review-uncommitted-code.<run-id>.report.md` with run ID
`YYYYMMDD-HHMMSS-<short-rand>`. Apply
`references/finding-verdict-classification.md` during synthesis as the sole
verdict rubric.

## Workflow

1. Review the target and determine findings. Findings include title, severity,
   confidence, location, scope, verdict, evidence, and impact.
2. Merge duplicates
3. Apply the rubric to each finding, and add a recommendation.

Write:

1. `# Review Summary` — target, mode, and readiness
2. `## Recommended Accept`
3. `## Recommended Blocked`

Use `None` for empty groups and exactly one readiness verdict:
- `proceed`: no accepted fix or blocked finding remains
- `proceed-after-fixes`: accepted findings remain
- `revise-first`: a genuine decision or evidence gap blocks safe use

Return report path and readiness without editing the target.
