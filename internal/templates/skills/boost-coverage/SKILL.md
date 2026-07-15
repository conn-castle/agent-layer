---
name: boost-coverage
description: >-
  Explicit-only.
  Raise coverage to an explicit or repository-required target with coherent
  behavior-focused tests and authoritative coverage evidence. Do not use for
  full-suite cleanup or pruning tests added in the current diff.
---

# boost-coverage

Raise trustworthy coverage to a declared target with behavior-focused tests.

## Inputs and boundaries

Accept a target percentage and optional file, component, or domain. Otherwise
use the repository's enforced minimum from continuous integration, coverage
configuration, or DECISIONS.md. Do not invent a target or make an explicitly
named scope stricter than the repository requires.

Prefer test-only changes. A small behavior-preserving production seam is
allowed only when necessary to test an established contract. Never change
production behavior, exclusions, thresholds, or the coverage command to improve
the result. Named files must be eligible under the repository's coverage rules.

Write `.agent-layer/tmp/boost-coverage.<run-id>.report.md`, using
`YYYYMMDD-HHMMSS-<short-rand>` for `run-id`.

## Workflow

1. Read COMMANDS.md and coverage configuration. Identify the authoritative
   command, working directory, threshold, exclusions, and useful per-file
   output. Delegate discovery when it materially reduces context, but validate
   the returned evidence.
2. Run the command once for a baseline. If the target is already met, report
   `already-satisfied` without editing. If the contract cannot be established,
   report the missing evidence rather than guessing.
3. Select a coherent batch of under-covered behavior that can credibly close
   the gap. Favor meaningful branches, boundaries, error paths, and component
   interactions. Every test must be capable of detecting a real production
   defect; exclude implementation-restatement and statically enforced tests.
4. Implement the batch and rerun the authoritative command. Use the new
   coverage evidence to address actionable misses while valuable in-scope
   behavior remains. Do not add low-value tests merely to reach a number.

Return `blocked` only when the target cannot be reached credibly without
missing tooling, changing production behavior, writing weak tests, or resolving
a user-owned decision.

## Completion contract

Structure the report as:

1. `# Coverage Summary`
2. `## Contract and Baseline`
3. `## Behavior-Focused Tests`
4. `## Final Coverage Evidence`
5. `## Remaining Shortfall or Blocker`

Report `target-met`, `already-satisfied`, or `blocked`, including the target and
its source, command, baseline and final result, behavior tested, changed files,
and any remaining blocker. The measured final result must cover the resulting
tree.
