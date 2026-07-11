---
name: simplify-codebase
description: >-
  Simplify a repository or named scope by removing material internal complexity
  such as dead code, obsolete options, needless indirection, duplicate
  workflows, or misplaced boundaries while preserving external behavior.
---

# simplify-codebase

Remove one coherent source of internal complexity. Successful work deletes or
collapses a named concept rather than polishing syntax.

## Scope and inputs

- Default scope is repository source code, excluding generated, vendored, and
  build output.
- Accept explicit paths or filters, dead-code inclusion, and assessment-only
  mode.
- Use `/clean-and-fix-code` for current-diff cleanup.
- Write `.agent-layer/tmp/simplify-codebase.<run-id>.report.md`, where `run-id`
  is `YYYYMMDD-HHMMSS-<short-rand>`.

## Useful-change contract

Apply a change only when it both:

1. removes a named concept, such as dead code, an obsolete option or branch, a
   needless wrapper or single-implementation abstraction, a duplicated
   workflow, or an incoherent module boundary; and
2. has material maintenance value through a smaller API or state surface,
   reduced recurring confusion, lower defect risk, or one coherent set of
   related removals.

Local renames, syntax translations, isolated helper deletion, branch inversion,
or tiny cleanup without recurring burden are report-only outcomes.

## Constraints and decisions

- Preserve user-facing behavior, observable output, data shape, and stable
  external APIs.
- Internal contracts may change only when every caller, test, document, and
  reference is updated in the same pass.
- Ask before changing an external contract; choosing among meaningful
  architecture, ownership, data-shape, or migration alternatives; deleting a
  test with uncertain behavioral value; or making a broad change without a
  credible verification path.
- Mixed liveness evidence from reflection, registration, string dispatch, or
  external references requires a user decision before deletion.

## Workflow

### 1. Establish scope and verification

Read COMMANDS.md before selecting checks. Identify public contracts, available
dead-code or complexity evidence, and a credible verification path. Create the
report.

### 2. Assess candidates once

Inspect enough of the declared scope to identify the strongest material
simplification or establish that none exists. Stop gathering candidates once
the evidence is sufficient to choose safely; do not require an arbitrary count
or expand scope merely to manufacture a change.

For serious candidates, record:

- current code-created complexity
- exact concept removed and intended simpler shape
- maintenance payoff, risk, and verification path
- `apply` or `report-only` verdict

For a materially large file, record `keep`, `split`, or `merge` only when that
decision is relevant to a candidate. `Keep` is valid when one responsibility is
clear and a split would fragment understanding.

If no candidate clears the useful-change contract, write a report-only outcome
and yield.

### 3. Apply one coherent simplification

Implement the strongest credible candidate or tightly related candidate set.
Prefer deletion, removal of obsolete parameters or modes, collapsing needless
indirection, and unifying genuinely duplicated workflows when every original
site becomes simpler.

Do not hide complexity behind a new abstraction, leave internal compatibility
shims without an external requirement, or widen into feature work and unrelated
bug fixing.

### 4. Verify once

Run one risk-proportional repository-defined verification lane and confirm the
named concept disappeared rather than moved. If it exposes a concrete in-scope
mistake, repair that failure directly and rerun only the failed evidence. If
the simplification cannot be made credible, undo only this skill's changes and
report the blocker.

### 5. Report and yield

The report contains:

1. `# Simplification Summary`
2. `## Scope`
3. `## Candidate Assessment`
4. `## Changes Applied`
5. `## Verification`
6. `## Decisions Needed`

Record relevant dead-code evidence in `Candidate Assessment`; do not create an
empty dedicated section when dead code was not part of the selected candidate.
The summary includes the terminal verdict: `applied`, `report-only`, or
`blocked-user-decision`.

## Definition of done

- The scope received one sufficient candidate assessment.
- Applied work removed at least one named concept and updated all affected
  contracts in the same pass.
- External behavior remained unchanged unless explicitly approved.
- Verification evidence and the terminal verdict are recorded, then the skill
  yields.
