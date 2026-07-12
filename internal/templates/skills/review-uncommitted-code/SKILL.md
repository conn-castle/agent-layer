---
name: review-uncommitted-code
description: >-
  Report-only review of a concrete file, directory, diff, range, working tree,
  or proactive hotspot for material correctness, risk, architecture, test,
  documentation, performance, reliability, and maintainability findings.
---

# review-uncommitted-code

Review a concrete target and write a findings report without modifying reviewed
files.

## Target and artifact

Use the user's explicit target, then requested proactive hotspots, otherwise all
staged, unstaged, and untracked changes. Review the last commit only when asked.
For hotspots, select bounded targets using concrete signals such as churn,
complexity, weak coverage, scaffolding, reliability boundaries, or contract
drift. Stop for the smallest scope decision only when no credible target exists.

Write `.agent-layer/tmp/review-uncommitted-code.<run-id>.report.md`. During
synthesis read `assets/finding-verdict-classification.md`; it is the sole
verdict rubric, not another review stage.

## Review contract

- Review concrete code, tests, diffs, and observable contracts, not hypothetical
  alternatives.
- Give one reviewer the complete target and relevant concerns. If this context
  authored or materially changed the target, use one fresh built-in reviewer
  with the target and authoritative contract, not the author's rationale.
  Otherwise review directly.
- Do not split one diff for perspectives, consensus, or convergence.
- Treat reviewer output as candidates until validated against current evidence.
- Report only material correctness, safety, scope, reliability, performance,
  test-integrity, or maintainability findings. Omit style, speculation,
  unsupported claims, duplicates, and unrelated known issues.

## Workflow

### 1. Review

Record target, mode, and hotspot evidence. Read the minimum surrounding code,
tests, docs, and memory needed to establish intent, then cover relevant:

- correctness, error handling, boundary inputs, and failure modes
- ownership, interfaces, coupling, and unnecessary complexity
- tests, docs, performance, concurrency, data safety, and operability

### 2. Synthesize and report

Validate candidates against the current tree, merge duplicates, and apply the
verdict rubric. Each survivor includes title, severity, confidence,
location, scope, `Accept` or `Defer`, evidence, impact, and recommendation.

`Accept` is current, in scope, and actionable without a new user decision.
`Defer` requires a genuine user decision or unavailable evidence; a scope
boundary alone is not a user decision.

Write:

1. `# Review Summary` — target, mode, and readiness
2. `## Recommended Accept`
3. `## Recommended Defer`

Use `None` for empty groups and one readiness verdict:

- `proceed`: no accepted fix or blocking defer remains
- `proceed-after-fixes`: accepted findings remain
- `revise-first`: a genuine decision or evidence gap blocks safe use

Return the report and readiness after covering the target with current evidence.
Do not edit the target or add another reviewer for confidence.
