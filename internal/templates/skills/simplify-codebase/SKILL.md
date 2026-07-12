---
name: simplify-codebase
description: >-
  Remove material internal complexity such as dead code, obsolete options,
  needless indirection, duplicate workflows, or misplaced boundaries while
  preserving external behavior.
---

# simplify-codebase

Remove one coherent source of complexity. Success deletes or collapses a named
concept rather than polishing syntax.

## Scope and useful-change gate

Default to repository source excluding generated, vendored, and build output;
accept paths, filters, dead-code inclusion, and assessment-only mode. Use
`/clean-and-fix-code` for current-diff cleanup. Write
`.agent-layer/tmp/simplify-codebase.<run-id>.report.md`.

Apply only a change that removes a named concept—dead code, obsolete branch or
option, needless wrapper/abstraction, duplicate workflow, or incoherent
boundary—and materially reduces API/state surface, recurring confusion, defect
risk, or a coherent family of related burden. Local renames, syntax swaps,
isolated helper deletion, and tiny cleanup are report-only.

Preserve behavior, output, data shape, and stable external APIs. Update every
caller, test, document, and reference when changing an internal contract. Ask
before changing external contracts; choosing materially different architecture,
ownership, data, or migration paths; deleting uncertain test behavior; or
proceeding without credible verification. Mixed liveness evidence from
reflection, registration, strings, or external references blocks deletion.

## Workflow

### 1. Assess once

Read COMMANDS.md, identify contracts and a verification path, and create the
report. For broad/context-heavy scope, use one fresh scout to return candidates
with the named complexity, boundaries, behavior, payoff, risk, and verification;
assess a narrow scope directly.

Inspect only enough evidence to select the strongest material candidate or
prove none qualifies. Record its current complexity, concept removed, simpler
shape, payoff, risk, verification, and `apply` or `report-only` verdict. Consider
`keep`, `split`, or `merge` for a large file only when relevant. Return
report-only when nothing clears the gate.

### 2. Apply and verify

Implement the strongest candidate or tightly related set. Prefer deletion,
removing obsolete parameters/modes, collapsing indirection, and unifying true
duplication. Do not hide complexity behind a new abstraction, add internal
compatibility shims without an external need, or widen into features and
unrelated fixes.

Run one risk-proportional repository verification lane and confirm the concept
was removed rather than moved. Repair a concrete in-scope failure and rerun its
invalidated evidence. If credibility cannot be restored, undo only this skill's
changes and report the blocker.

## Completion contract

Report scope, candidate, changes, verification, decisions, and `applied`,
`report-only`, or `blocked-user-decision`. Applied work must remove a named
concept, update affected contracts, and preserve unapproved external behavior.
