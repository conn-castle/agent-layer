---
name: improve-codebase
description: >-
  Run one wide, evidence-led quality sweep over a repository or named scope,
  repair material local and cross-cutting findings, review the concrete work,
  and verify the result.
---

# improve-codebase

Cover the declared scope once, repair every validated material finding that is
not genuinely blocked, and verify the combined result.

## Scope and artifacts

Accept paths, audit lenses, exclusions, and report-only mode; otherwise cover
the repository. Exclude generated, vendored, and build output. Consider
correctness, safety, security, lifecycle, concurrency, tests, ownership,
interfaces, dependencies, errors, documentation, and maintainability as
relevant.

Write `.agent-layer/tmp/improve-codebase.<run-id>.report.md`, with delegated
evidence under the same prefix. Do not stage, commit, or push.

## Finding standard

Accept only current, evidence-backed problems with material impact on behavior,
safety, reliability, performance, test integrity, architecture, or maintenance
cost. Classify them as `local`, `cross-boundary`, or `architectural`; merge
duplicates under their root cause. Omit stylistic, speculative, unsupported,
and immaterial candidates.

## Workflow

1. Read COMMANDS.md and authoritative context. Map meaningful components,
   boundaries, ownership, state, dependencies, and cross-cutting questions.
   Delegate non-overlapping investigations when useful; validate their coverage
   and evidence.
2. Examine the complete map for cross-cutting relationships — ownership,
   dependency, data flow, protocol, lifecycle, security, reliability, error
   handling, tests, documentation — that no isolated investigation can
   establish. Build a ledger of each accepted finding's evidence, impact, root
   cause, repair boundary, required updates, and verification. Return
   `no-material-findings` if empty, or the ledger without mutation in
   report-only mode.
3. Group findings into dependency-aware packages and apply them sequentially.
   Repair root causes and required tests, documentation, and memory; run focused
   checks and mark each `fixed`, `invalid-with-evidence`, or blocked by a named
   constraint. Continue independent work when safe.
4. Run `/review-uncommitted-code` once over the delivery and affected
   boundaries, repairing accepted findings. Run risk-proportional verification
   over the final tree and ledger. Concrete failures return to repair; confidence
   alone does not restart discovery or review.

Delegated-agent failure is recoverable through local evidence or replacement.
Escalate only for external or authoritative-contract constraints, unsafe user
state overlap, or genuine user decisions. Difficulty or breadth alone is not a
user decision.

## Completion contract

Report `improved`, `report-only`, `no-material-findings`,
`blocked-user-decision`, or `blocked-concrete-failure`. Include scope and
boundary coverage, evidence sources, the finding ledger, repairs, review,
verification, blockers, report path, and residual risk.
