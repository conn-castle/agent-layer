---
name: improve-codebase
description: >-
  Run one wide, evidence-led quality sweep over a repository or named scope:
  investigate local and cross-cutting risks, directly repair material findings
  from small defects through architectural problems, review the concrete work,
  and verify the result.
---

# improve-codebase

Survey the entire declared scope once, identify material problems within and
across its parts, and directly fix every accepted in-scope finding that is not
blocked by a genuinely user-owned decision or concrete failure. Work packages
organize mutations; they do not narrow discovery or limit the kinds of problems
this skill may address.

## Scope and inputs

Accept explicit paths, subsystems, audit lenses, exclusions, and report-only
mode. Otherwise the declared scope is the whole repository.

- Account for the full declared scope at the subsystem, component, and boundary
  level. A wide sweep does not require ceremonial per-file commentary or an
  exhaustive search for every conceivable defect.
- Exclude generated, vendored, and build output unless explicitly included.
- Examine correctness, data safety, security, concurrency, cancellation, input
  robustness, test integrity, ownership, interfaces, dependency direction,
  duplicated workflows, error handling, documentation truth, and material
  maintainability where relevant.
- Do not use arbitrary chunk counts or finding caps to stop a productive sweep.
  Stop discovery when the scope map is covered and the evidence is sufficient
  to synthesize material findings.
- Existing issue tracking is evidence and deduplication context, not a reason to
  leave an otherwise accepted in-scope problem unfixed.

Write `.agent-layer/tmp/improve-codebase.<run-id>.report.md`, where `run-id` is
`YYYYMMDD-HHMMSS-<short-rand>`. Use it as the master ledger and handoff. Write
delegated evidence to:

- `.agent-layer/tmp/improve-codebase.<run-id>.investigation-<index>-<slug>.report.md`
- `.agent-layer/tmp/improve-codebase.<run-id>.cross-cutting.report.md`
- `.agent-layer/tmp/improve-codebase.<run-id>.repair-<index>-<slug>.report.md`

## Required agent boundaries

- Use one fresh built-in scout subagent to map the full declared scope into
  coherent subsystem, component, and interface-boundary investigation groups.
- Use enough coherent, non-overlapping investigators to give every mapped
  subsystem and boundary credible independent coverage without overloading any
  one context. Do not minimize agent count at the expense of distinct
  perspectives, and do not split groups merely to increase fan-out. Each
  investigator is fresh and read-only. Run substantial independent groups
  concurrently when the wall-clock benefit warrants the extra agent cost;
  otherwise run them sequentially.
- After those reports return, use one fresh built-in cross-cutting investigator
  to examine the complete scope map, all investigator reports, and the cited
  boundary evidence. Its responsibility is to find material relationships and
  architectural problems that no isolated investigation could establish.
- Use fresh built-in fixer subagents for context-heavy repair packages when
  useful. Keep all working-tree mutations sequential against the latest tree.
- Review the combined concrete work through `/review-uncommitted-code` once so
  its fresh-context, complementary reviewers remain authoritative.

The owning agent controls scope coverage, validates findings, maintains the
master ledger, orders mutations, resolves routine decisions, and produces the
terminal result. Investigators and reviewers return evidence, findings, or a
blocker; they do not create another orchestration layer.

Do not assign multiple agents to the same artifact or concern for consensus,
and do not create parallelism whose only result is duplicated repository
reading. Comprehensive means every meaningful area and relationship is covered,
not that every available agent slot is used.

## Finding and repair contract

- Classify findings as `local`, `cross-boundary`, or `architectural`. Breadth is
  not severity, and architectural scope is not automatically a user decision.
- Validate every candidate against current code, tests, specifications, and
  documented contracts. Evidence outranks agreement between investigators.
- Accept a finding only when it materially affects correctness, safety,
  reliability, performance, test integrity, architectural coherence, or
  meaningful maintenance cost.
- Omit unsupported, duplicate, stylistic, speculative, and immaterial
  candidates. Small issues remain valid when their concrete impact clears the
  same materiality threshold.
- Directly fix every accepted finding within the declared scope, whether it is
  a small defect, repeated local problem, cross-component inconsistency, or
  holistic architectural issue.
- Ask the user only when available evidence leaves multiple viable choices that
  materially differ in behavior, architecture, scope, risk, cost, migration, or
  external contract. Continue independent repairs while that decision is
  pending when safe.
- Apply directly required tests, documentation, and memory updates with each
  repair. Do not route findings into planning, coverage, simplification,
  issue-fixing, or verification sub-workflows.

## Workflow

### 1. Map the entire scope once

Read COMMANDS.md and the minimum memory and repository context needed to
identify authoritative constraints. Give the scout the declared scope, lenses,
exclusions, known issues, and report path.

The scout returns:

- every in-scope subsystem and significant component
- ownership, interface, state-flow, and dependency boundaries
- coherent investigation groups that together cover the declared scope
- cross-cutting questions that span those groups
- known constraints, exclusions, and evidence locations

The owning agent checks that the map accounts for the declared scope and
consolidates groups where one context can examine them coherently. Do not
silently shrink an explicitly requested scope because it is large.

### 2. Investigate the full scope once

Give each fresh investigator its distinct group, the relevant scope map and
constraints, and its investigation report path. Follow the agent boundary above
for grouping and concurrency.

Each investigator reports only evidence-backed material candidates with exact
locations, affected behavior or boundary, impact, and the smallest credible
repair shape. It also records which assigned components and boundaries were
examined so scope coverage can be reconciled without an unaffected-file
inventory.

After all group reports return, give the fresh cross-cutting investigator:

- the complete scope and boundary map
- every investigator report
- cited code, tests, contracts, and documentation needed to inspect the
  relationships between groups
- the cross-cutting report path

Have it examine ownership and dependency direction, state and data flow,
protocol consistency, duplicated workflows, lifecycle and cancellation,
security and reliability boundaries, error semantics, and documentation or
test contracts across the declared scope. It reports only material findings
that require a multi-part view.

### 3. Synthesize one master finding ledger

Validate candidates against their cited current-tree evidence, merge
duplicates, and reconcile local symptoms with any shared cross-cutting root
cause. Preserve the strongest root-cause finding instead of scheduling several
surface repairs.

For every material finding record:

- stable identifier, category, severity, and affected scope
- concrete evidence and impact
- root cause and repair boundary
- directly required tests, documentation, memory, and verification
- any user-owned decision or concrete blocker

If no material finding survives validation, return `no-material-findings` after
the full scope and cross-cutting coverage are recorded. In report-only mode,
return the validated ledger without editing.

Group all accepted findings into coherent, dependency-ordered repair packages.
Packages are execution boundaries only: every accepted finding must appear in
one package or have an explicit blocker.

### 4. Repair every accepted finding

Execute packages sequentially against the latest tree. Handle a narrow package
directly or give a context-heavy package to one fresh built-in fixer subagent
with its finding identifiers, evidence, repair boundary, and repair report path.

Require each package to:

- repair the shared root cause rather than each visible symptom
- update directly affected tests, documentation, and memory
- run the narrowest credible checks for the changed behavior
- account for every assigned finding as `fixed`, `invalid-with-evidence`,
  `blocked-user-decision`, or `blocked-concrete-failure`

Do not stop after small local fixes while accepted cross-boundary or
architectural findings remain. If a user decision blocks one package, continue
independent packages when their outcome cannot prejudice that decision.

### 5. Review the combined concrete work once

When changes were made, run `/review-uncommitted-code` once over the combined
changes and their directly affected boundaries. Pass the master finding ledger
as contract evidence. Validate and directly address every `Recommended Accept`
finding with focused evidence.

Do not run another broad review after those repairs. A user-owned decision or
explicit scope boundary may remain `Recommended Defer`; difficulty or breadth
alone may not.

### 6. Verify the final result and yield

Run one combined, risk-proportional repository-defined verification stage that
covers the final tree and repaired finding set. Reuse focused evidence only when
its command, result, and covered tree are still current. A concrete failed check
may return to its responsible repair and rerun the invalidated evidence; a
desire for more confidence may not restart surveying or review.

Write the report with:

1. `# Codebase Improvement Summary` — declared scope and terminal outcome
2. `## Scope and Boundary Coverage`
3. `## Investigation Reports` — group assignments and evidence coverage
4. `## Material Finding Ledger` — local, cross-boundary, and architectural
5. `## Repairs and Focused Evidence`
6. `## Concrete-Work Review`
7. `## Final Verification`
8. `## User Decisions, Blockers, and Residual Risk`

Use one outcome: `improved`, `report-only`, `no-material-findings`,
`blocked-user-decision`, or `blocked-concrete-failure`.

## Completion contract

- The scope map accounts for the entire declared scope at meaningful subsystem,
  component, and boundary granularity.
- Distinct investigators examined every mapped group once, and the cross-cutting
  investigator examined their relationships once.
- Every validated material finding has a terminal repair, user-decision, or
  concrete-failure outcome; work packages did not silently narrow the ledger.
- The combined implementation received one fresh-context concrete-work review
  and one final verification stage.
- The skill returns its report, scope coverage, findings, repairs, evidence,
  decisions, blockers, and residual risk, then yields.
