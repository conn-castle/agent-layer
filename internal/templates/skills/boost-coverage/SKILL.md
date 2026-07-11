---
name: boost-coverage
description: >-
  Raise coverage to an explicit or repository-required target with one
  behavior-focused test batch and one final coverage measurement. Do not use
  for full-suite cleanup or pruning tests added in the current diff.
---

# boost-coverage

Raise trustworthy coverage to a declared target. Establish the coverage
contract once, implement one coherent batch of behavior-focused tests, measure
the final result once, and yield.

## Inputs and target

Accept an explicit target percentage and an optional file, component, or domain
that narrows where the target is pursued. Otherwise use the minimum threshold
defined by continuous integration, coverage configuration, or DECISIONS.md. A
named scope does not create an unstated higher target.

- Do not invent a target or treat an already-satisfied threshold as permission
  for open-ended improvement.
- If the repository threshold is already satisfied and the user supplied no
  higher target, return `already-satisfied` without editing.
- A user-named file must be eligible under the repository's coverage rules.
- Prefer test-only changes. Allow a small behavior-preserving seam only when it
  is necessary to test an established contract.

Write `.agent-layer/tmp/boost-coverage.<run-id>.report.md`, where `run-id` is
`YYYYMMDD-HHMMSS-<short-rand>`.

## Rules

- Read COMMANDS.md and repository tooling before choosing commands, thresholds,
  scope, or exclusions.
- Do not add tautological, self-confirming, type-system, schema, or
  implementation-restatement tests.
- Test behavior, logic, integration, and meaningful runtime failure modes.
- Do not change production behavior, weaken coverage configuration, or swap to
  a narrower command to improve the number.
- Ask only when no authoritative target exists, the coverage contract cannot be
  established, required tooling must be installed, or a testability change
  requires a user-owned design decision.

## Workflow

### 1. Establish the coverage contract

Identify the documented coverage command, working directory, threshold source,
exclusions, and trustworthy per-file output. Use a fresh built-in scout
subagent when this discovery is context-heavy and can return a compact evidence
map; handle a compact contract directly. Run the command once to capture the
baseline and eligible shortfalls.

If the contract or target cannot be established, return the smallest blocking
decision. If the target is already satisfied, write the report and yield.

### 2. Select one coherent test batch

Choose one coherent set of under-covered behaviors credibly sized to reach the
declared target. Prefer high-value error paths, boundaries, branching logic, and
component interactions over mechanically selecting the lowest percentage or
touching one file at a time. If reaching the target would require unrelated or
low-value test work, report that concrete target blocker before editing.

Record the behavior to prove, its contract, baseline coverage, expected target
contribution, and why the tests can detect a real defect.

### 3. Implement and measure once

Implement the coherent test batch directly or use one fresh built-in subagent
when doing so meaningfully protects the owning context or provides useful
independence. Do not split one coherent batch across agents merely to create
parallel work. Then run the authoritative coverage command once on the final
tree.

If the final result misses the declared target, directly address a concrete,
actionable miss in the selected behavior and rerun only the affected evidence.
Do not start a new target-selection loop. Stop when the remaining shortfall
requires a different scope, low-value tests, missing tooling, or a user-owned
decision.

### 4. Report and yield

The report contains:

1. `# Coverage Summary` — scope, target, and terminal outcome
2. `## Contract and Baseline` — command, threshold source, exclusions, and
   observed baseline
3. `## Behavior-Focused Tests` — behavior, files, and defects the tests detect
4. `## Final Coverage Evidence` — observed final result and covered tree
5. `## Remaining Shortfall or Blocker`

Use one outcome:

- `target-met`
- `already-satisfied`
- `blocked`

## Completion contract

- The target and coverage command come from an explicit or repository-defined
  source.
- The skill performed one baseline, one coherent implementation batch, and one
  final coverage measurement by default.
- Every added test can fail because of a real implementation defect.
- The report names the observed before/after result, any concrete repair of the
  selected batch, and any remaining blocker, then yields.
