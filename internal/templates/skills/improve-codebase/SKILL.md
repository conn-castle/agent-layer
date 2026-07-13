---
name: improve-codebase
description: >-
  Run one wide, evidence-led quality sweep over a repository or named scope,
  repair material local and cross-cutting findings, review the concrete work,
  and verify the result.
---

# improve-codebase

Cover the full declared scope once and fix every validated material finding not
blocked by a genuine user decision or concrete failure. Packages organize
repairs; they do not narrow discovery.

## Scope and artifacts

Accept paths, subsystems, audit lenses, exclusions, and report-only mode;
otherwise cover the whole repository. Exclude generated, vendored, and build
output unless included explicitly.

Examine relevant correctness, safety, security, concurrency, cancellation,
input, test, ownership, interface, dependency, duplication, error, docs, and
maintainability risks. Cover meaningful components and boundaries without
per-file ceremony or arbitrary finding quotas.

Write the master report to
`.agent-layer/tmp/improve-codebase.<run-id>.report.md` and delegated evidence
under the same prefix as `investigation-<index>-<slug>`, `cross-cutting`, and
`repair-<index>-<slug>` reports.

## Ownership

- One fresh scout maps the scope into coherent, non-overlapping investigation
  groups and cross-cutting questions.
- Fresh read-only investigators cover each group; run independent groups
  concurrently when useful. Do not duplicate artifacts or concerns for
  consensus.
- One fresh cross-cutting investigator examines the complete map and reports
  for relationships no isolated group can establish.
- The owning agent validates findings, maintains the ledger, orders all
  mutations, and may use fresh fixers for context-heavy repair packages.
- `/review-uncommitted-code` owns the single final code review. Keep mutations
  sequential and leave changes uncommitted and unpushed.

## Finding contract

Classify findings as `local`, `cross-boundary`, or `architectural`. Accept only
current, evidence-backed problems with material impact on correctness, safety,
reliability, performance, test integrity, architecture, or maintenance cost.
Merge duplicates under their shared root cause and omit stylistic, speculative,
unsupported, and immaterial candidates.

Fix every accepted in-scope finding, including required tests, docs, and memory.
Ask only when evidence leaves materially different behavior, architecture,
scope, risk, cost, migration, or external-contract choices. Difficulty or
breadth alone is not a user decision.

## Workflow

### 1. Map and investigate once

Read COMMANDS.md and relevant authoritative context. The scout records
components, ownership, interfaces, state and dependency boundaries, constraints,
groups, and cross-cutting questions. Validate that the map covers the declared
scope.

Give each investigator its group, constraints, and report path. Require exact
locations, affected behavior or boundary, impact, smallest repair shape, and
coverage accounted for. Then have the cross-cutting investigator examine the
complete evidence for ownership, dependency, data flow, protocol, lifecycle,
security, reliability, error, test, and documentation relationships.

### 2. Synthesize the master ledger

Validate candidates against the current tree and record for each survivor:

- stable ID, category, severity, scope, evidence, and impact
- root cause, repair boundary, required updates, and verification
- any genuine user decision or concrete blocker

Return `no-material-findings` after recording scope coverage when none survive;
in report-only mode return the ledger. Otherwise group every accepted finding
into dependency-ordered repair packages.

### 3. Repair all accepted findings

Execute packages sequentially against the latest tree, directly or through one
fresh fixer per context-heavy package. Each package repairs root causes, updates
required tests/docs/memory, runs focused checks, and records each assigned
finding as `fixed`, `invalid-with-evidence`, `blocked-user-decision`, or
`blocked-concrete-failure`. Continue independent packages when safe.

### 4. Review and verify once

When changes exist, run `/review-uncommitted-code` once over the combined work
and affected boundaries; validate and fix `Recommended Accept` findings with
focused evidence. Do not start another broad review.

Run one risk-proportional final verification over the final tree and repaired
ledger. A failed check may return to its responsible repair and rerun invalidated
evidence; confidence alone may not restart investigation or review.

## Completion contract

Report scope and boundary coverage, delegated evidence, the material ledger,
repairs, final review, verification, decisions, blockers, and residual risk.
Use one outcome: `improved`, `report-only`, `no-material-findings`,
`blocked-user-decision`, or `blocked-concrete-failure`.
