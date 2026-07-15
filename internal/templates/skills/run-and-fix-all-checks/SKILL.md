---
name: run-and-fix-all-checks
description: >-
  Explicit-only.
  Run the repository's full documented check lane, directly repair observed
  failures, and repeat only until the lane passes or a concrete blocker remains.
---

# run-and-fix-all-checks

Run the repository's complete required check lane to green through observed,
root-cause repairs. Do not create another planning or verification workflow.

## Scope and artifacts

Read COMMANDS.md and use the documented continuous-integration or all-checks
lane, including every required check type. Prefer its canonical command;
otherwise run the documented sequence. If no lane is explicit, resolve and
record the union required by repository tooling and continuous integration.

Write `.agent-layer/tmp/run-and-fix-all-checks.<run-id>.report.md`, with useful
failure artifacts under the same prefix.

## Workflow

1. Run the lane and preserve commands, exit statuses, relevant output, and the
   covered tree. Return `checks-passed` when green.
2. Diagnose each failure to its root cause. Repair it, including required tests,
   documentation, or memory, and run a credible affected check. Serialize
   mutations.
3. Rerun the full lane. Further failures drive another diagnosis and repair.
   For recurring equivalent failures, revise the causal model, instrument, or
   consult authoritative documentation instead of repeating a strategy.
   Continue only when new evidence supports a safe repair; otherwise stop with
   `repeated-failure`.

Never skip, weaken, or narrow the lane or lower thresholds. Do not stage,
commit, discard, or destructively rewrite changes without authorization.
Continue independent work when an external gate blocks part of the lane.

Return `blocked` only when no safe diagnostic or repair path remains because of
an external gate, missing authoritative input, unsafe user-state overlap, a
required destructive or schema change, or a genuine user decision.

## Completion contract

Report `checks-passed`, `repeated-failure`, or `blocked`, with the resolved
lane, commands and rounds, failure evidence, repairs, focused checks, and
either final passing evidence or the named gate.
