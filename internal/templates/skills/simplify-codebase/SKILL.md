---
name: simplify-codebase
description: >-
  Remove material internal complexity such as dead code, obsolete options,
  needless indirection, duplicate workflows, or misplaced boundaries while
  preserving external behavior.
---

# simplify-codebase

Remove a coherent source of internal complexity. Success deletes or collapses
a named concept rather than polishing syntax.

## Scope and useful-change gate

Accept paths, filters, dead-code inclusion, and assessment-only mode; otherwise
inspect repository source, excluding generated, vendored, and build output. Use
`/clean-and-fix-code` for current-diff cleanup. Write
`.agent-layer/tmp/simplify-codebase.<run-id>.report.md`.

A qualifying change removes dead code, an obsolete option, needless
indirection, duplication, or an incoherent boundary and materially reduces API
or state surface, confusion, defect risk, or maintenance burden. Renames,
syntax swaps, and tiny cleanups are report-only.

Preserve behavior, data shape, and stable external APIs. Update affected
callers, tests, documentation, and references. Reject candidates requiring an
unapproved contract, architecture, ownership, data, or migration decision.
Mixed liveness evidence from reflection, registration, string-based lookup, or
external references blocks deletion. Uncertain liveness requires more evidence
for that candidate, not a stop to the whole assessment.

## Workflow

1. Read COMMANDS.md and relevant contracts. Inspect enough evidence to select a
   material candidate or establish that none qualifies. Delegate broad discovery
   when useful and validate its evidence.
2. Record the named complexity, boundaries, simpler form, payoff, risk,
   verification path, and `apply` or `report-only` verdict.
3. Implement the strongest candidate or related set, preferring deletion,
   collapsed indirection, removed obsolete modes, and unified duplication. Do
   not move complexity into a new abstraction or widen scope.
4. Run proportionate verification and confirm the concept was removed, not
   moved. Repair in-scope failures and rerun invalidated evidence. If behavior
   preservation cannot be proven, revert only this skill's changes, record the
   rejection, and continue with another qualified candidate when available.

## Completion contract

Report `applied`, `report-only`, or `blocked-user-decision`, including scope,
candidate, concept removed, changes, verification, decisions, report path, and
residual risk. Applied work must preserve all unapproved external behavior.
