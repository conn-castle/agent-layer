---
name: loop-clean-and-fix
description: >-
  Run one to four cleanup rounds over the uncommitted working tree, continuing
  only while each round demonstrates material, non-repeating progress.
---

# loop-clean-and-fix

Run `/clean-and-fix-code` at least once and at most four times. The limit is a
safety ceiling, not a target: stop as soon as another round is not justified by
concrete progress from the previous round.

## Scope

The target is the combined staged, unstaged, and untracked working tree. Do not
ask the user to confirm files before starting. If the target is empty, write the
report with `resolved_findings: []` and stop.

## Output artifact

Write `.agent-layer/tmp/loop-clean-and-fix.<run-id>.report.md` using
`run-id = YYYYMMDD-HHMMSS-<short-rand>`.

## Rules

- Run cleanup rounds sequentially against the latest working tree.
- Capture enough before-and-after working-tree state to prove whether each round
  materially changed the target and to detect oscillation.
- Treat rejected, deferred, and already-resolved findings as report data, not
  reasons to continue.
- Stop when delegated output is missing the outcome, finding disposition, or
  evidence needed to make the repeat decision. Do not infer progress from prose.
- Do not stage, commit, discard, or destructively rewrite changes unless the
  user explicitly asks.
- Do not modify unrelated code or repeat a repair that already failed without
  new evidence.

## Workflow

### 1. Run a cleanup round

Run `/clean-and-fix-code`. Record:

- outcome and report path
- cleanup pre-pass results
- accepted, rejected, deferred, and already-resolved counts
- `resolved_findings`
- changed files and material before/after diff
- focused checks and evidence
- blockers and residual risk

### 2. Apply the repeat gate

Round one always runs. After each successful round, continue only when all of
these are true:

- fewer than four rounds have run
- the cleanup outcome is `completed`, not `no-findings`
- the round materially changed the working tree
- the change resolved at least one accepted finding or applied a material
  cleanup from a pre-pass
- no evidence-equivalent finding recurred from the previous round
- the resulting working-tree state does not match an earlier recorded state
- no concrete blocker, destructive action, or user-owned decision prevents the
  next round

Stop immediately when any condition fails. In particular:

- `no-findings` or no material change means the work reached diminishing returns
- a recurring finding means the attempted repair did not resolve its root cause,
  regardless of its reported disposition
- a repeated working-tree state means the loop is oscillating
- round four ends the loop even when it made progress

Do not run a confirmation round after the gate closes.

### 3. Report

Write:

1. `# Loop Clean and Fix Summary`
2. `## Cleanup Rounds`
3. `## Issue Ledger`
4. `## Resolved Findings`
5. `## Stop Reason`
6. `## Residual Risk`

The issue ledger must include every reported issue with its round, severity,
classification, location, outcome, and source. Record one stop reason:

- `empty-target`
- `no-findings`
- `no-material-change`
- `repeated-finding`
- `oscillation`
- `blocked`
- `iteration-cap`

## Completion contract

Return the report path, round count, stop reason, aggregate
`resolved_findings`, focused evidence, issue ledger, and residual risk. A run is
successful when it stops because the target is empty, no findings or material
change remain, or the fourth-round ceiling is reached without an unresolved
blocker.
