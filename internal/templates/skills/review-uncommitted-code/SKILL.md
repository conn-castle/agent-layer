---
name: review-uncommitted-code
description: >-
  Explicit-only.
  Report-only review of a concrete file, directory, diff, range, working tree,
  or proactive hotspot for material correctness, risk, architecture, test,
  documentation, performance, reliability, and maintainability findings.
---

# review-uncommitted-code

Review a target and report material findings without edits.

## Target and artifact

Use the explicit target, then requested hotspots, otherwise all working-tree
changes. Review the last commit only when asked. Choose hotspots from churn,
complexity, weak coverage, scaffolding, reliability boundaries, or contract
drift; if none is credible, return `no-findings` with evidence.

Write `.agent-layer/tmp/review-uncommitted-code.<run-id>.report.md` with run ID
`YYYYMMDD-HHMMSS-<short-rand>`. Apply
`references/finding-verdict-classification.md` during synthesis as the sole verdict
rubric.

## Review contract

- If this context authored or materially changed the target, give one fresh
  built-in reviewer the complete target and contract. Otherwise review directly.
  Recover or replace unusable results, then review directly if needed.
- Do not split one diff for perspectives or consensus.
- Validate candidates against current code, tests, diffs, and contracts. Report
  only material findings.

## Workflow

Record target, mode, and hotspot evidence. Read enough surrounding code, tests,
docs, and memory to establish intent, then cover relevant:

- correctness, error handling, boundary inputs, and failure modes
- ownership, interfaces, coupling, and unnecessary complexity
- tests, docs, performance, concurrency, data safety, and operability

Merge duplicates and apply the rubric. Findings include title, severity,
confidence, location, scope, verdict, evidence, impact, and recommendation.

Write:

1. `# Review Summary` — target, mode, and readiness
2. `## Recommended Accept`
3. `## Recommended Defer`

Use `None` for empty groups and exactly one readiness verdict:

- `proceed`: no accepted fix or blocking defer remains
- `proceed-after-fixes`: accepted findings remain
- `revise-first`: a genuine decision or evidence gap blocks safe use

Finish after covering the target with current evidence. Return report path and
readiness without editing the target.
