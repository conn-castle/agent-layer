---
name: simplify-codebase
description: >-
  Codebase-wide simplification for removing real internal complexity: dead
  code, obsolete options, needless indirection, duplicate workflows, and
  misplaced module boundaries across explicit paths or the whole repo. Use
  `simplify-new-code` instead when the target is only the current uncommitted
  diff.
---

# simplify-codebase

Simplify by removing concepts, not polishing syntax. A successful run leaves
fewer moving parts for future readers while preserving user-facing behavior and
external APIs.

## Defaults and inputs

- Default scope: repository source tree, excluding generated files, vendor
  directories, and build artifacts.
- Accepted inputs: explicit paths/filters, dead-code on/off, assessment-only.
- Do not operate only on the current diff; that is `simplify-new-code`.
- Required report: `.agent-layer/tmp/simplify-codebase.<run-id>.report.md`.
  Use `run-id = YYYYMMDD-HHMMSS-<short-rand>` and create it with `touch`.

## Useful-change bar

Apply edits only when a candidate satisfies both:

1. It removes a named concept: dead file/symbol, config key, option, parameter,
   branch, mode, wrapper, adapter, facade, single-implementation abstraction,
   duplicated workflow across three or more sites, or incoherent module
   boundary.
2. It has a material maintenance payoff: high-traffic area, recurring confusion,
   known defect source, stale docs/test churn, smaller API surface, or several
   related removals that form one coherent simplification.

Report-only when the best candidate is local cleanup, one isolated helper
deletion, style rename, branch inversion, equivalent translation (`if` chain to
map, switch to regex, loop to recursion, etc.), or a tiny deletion with no
recurring burden. Do not create a small "something changed" diff.

## Hard constraints

- Preserve user-facing behavior, observable output, data shape, and stable
  external APIs.
- Internal contracts may change when all callers, tests, docs, and references
  are updated in the same pass.
- Do not install tools without approval.
- Local cleanups may accompany a larger simplification; they are never the
  reason for the change.

## Human checkpoints

Ask before:

- removing dead code when reflection, registration, string dispatch, or
  external references make liveness evidence mixed
- a non-trivial change with no credible verification path
- a significant refactor across many files or subsystems
- changing an external API or documented stable contract
- architecture, ownership, data-shape, migration, or scope alternatives with
  real tradeoffs
- deleting a test that may protect subtle behavior

## Workflow

### 0. Preflight

- Run `git status --porcelain` and `git diff --stat`.
- Read `COMMANDS.md` before selecting checks.
- Identify scope, languages, public/external API surface, and available
  dead-code or complexity tooling.
- Check `git log -30 --format='%h %s%n%b'` for recent simplification/refactor
  work. Avoid repeatedly revisiting the same subtree if another area offers a
  larger concept removal.
- Create the report file.

### 1. Find candidates

Inspect enough of the scope to find the strongest simplification, not the
easiest edit. Unless the user gave a tiny strict scope, record at least three
candidates before choosing. If the initial area only yields local cleanups,
expand once to adjacent owner modules before deciding report-only.

Lead with:

- trusted dead-code tooling or reference searches for internal unreachable code
- always-one-value options, flags, parameters, and branches
- wrappers, adapters, facades, factories, or registries with fixed consumers
- duplicated workflows or configuration across three or more sites
- large files, or small files whose split/merge no longer matches
  responsibility

For large files, record a keep/split/merge verdict. `Keep` is valid when one
responsibility is clear and splitting would fragment understanding.

For each candidate, record:

- current complexity and whether it is inherent or code-created
- exact concept removed
- intended simpler shape
- maintenance payoff
- risk and verification path
- apply vs report-only verdict

If no dead code is removed, record the symbols, files, or areas inspected under
`Dead Code Evidence`.

If all candidates fail the useful-change bar, write the report and stop.

### 2. Apply one coherent simplification

Implement the largest credible candidate or one coherent candidate set. Prefer:

- deleting dead internal code with evidence
- removing obsolete parameters, options, modes, or branches and updating every
  call site
- collapsing single-use or single-implementation indirection
- merging premature file splits or splitting genuinely mixed responsibilities
- unifying duplicate logic only when every original site shrinks and semantics
  stay visible

Do not:

- extract helpers just to shorten code
- rewrite clear linear code
- translate one structure to equivalent weight
- hide complexity behind a new abstraction
- leave compatibility shims for internal callers unless an external contract
  requires them

### 3. Verify

Run the fastest credible repo-defined checks. Re-assess the changed area and
confirm the named concept disappeared rather than moved. If verification fails,
fix the underlying issue or revert the simplification.

### 4. Report

Write the report with:

1. `# Simplification Summary`
2. `## Scope`
3. `## Candidate Assessment`
4. `## Large File Decisions`
5. `## Changes Applied`
6. `## Dead Code Evidence`
7. `## Verification`
8. `## Useful-Change Verdict`
9. `## Deferred Opportunities`

## Guardrails

- Do not use simplification as cover for feature work, bug fixes, or architecture
  expansion.
- Do not rewrite stable code because it looks old or scores high.
- Do not split when the new layout reads worse; do not merge when the result has
  multiple unrelated concerns.
- Do not undertake ambitious simplification when verification is weak; do not
  avoid it when verification is credible.

## Definition of done

- Report exists at the required path and names the scope, candidates, and
  useful-change verdict.
- Applied edits remove at least one named concept and update all callers, tests,
  docs, and references.
- User-facing behavior and external APIs are unchanged unless approved.
- Verification commands ran with observed results recorded in the report.

## Final handoff

State first: `applied` or `report-only`, and why. Then give the report path,
concepts removed, files restructured or deleted, verification commands, and
deferred opportunities.
