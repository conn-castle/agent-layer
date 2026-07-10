---
name: fully-implement-plan
description: >-
  Implement a supplied plan, clean and fix until diminishing returns, repair
  verified gaps proportionally, and verify completion. Use when the caller
  provides plan, task, and context artifacts and wants the local work finished
  without opening or shipping a PR or running full-repository testing.
---

# fully-implement-plan

Coordinate lower-level skills and maintain one run ledger.

## Required inputs

Require all of these before side effects:

- plan artifact path
- task artifact path
- context artifact path
- `implementer`: dispatch agent role for `/implement-plan`
- `fixer`: dispatch agent role for `/loop-clean-and-fix`
- `plan_reviewers`: one or more dispatch agent roles to pass to
  `/loop-clean-and-fix` and any `/plan-work` retry for verification gaps

If any input is missing, ask for it. Do not invent roles, reviewer lists, or
artifact paths, and do not auto-select artifacts from `.agent-layer/tmp/`.

## Required artifacts

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`. Create the run ledger before
writing at `.agent-layer/tmp/fully-implement-plan.<run-id>.report.md`.

## Context preservation

You are the orchestrator for this skill. Do not do work that belongs to
subagents or delegated skills in the orchestration context. Preserve your
context to make strategic decisions, enforce gates, reconcile returned outputs,
and continue this skill's workflow after every delegation returns.

## Compaction guidance

When compaction is needed, retain this entire skill verbatim. Also preserve the
current workflow step or phase, active artifact paths, selected scope, pending
gate verdicts, delegated skills and subagents already run and their outcomes,
unresolved blockers or user checkpoints, and the next exact step.

## Rules

- Dispatch external roles through `/agent-dispatch`.
- Do not ask the user to confirm target files before starting; required
  artifact paths, `implementer`, `fixer`, and `plan_reviewers` are enough.
- The supplied plan/task/context artifacts remain the authoritative contract.
  Treat delegated reports as evidence and resolved cleanup findings as additive
  verification obligations; neither may weaken or replace the contract.
- If any delegated call fails, returns blocked or checkpointed, or omits a
  required artifact or verdict, stop, record the blocker, and surface it in the
  final handoff.
- Run all implementation, cleanup, and repair mutations sequentially against the
  latest working tree.
- Do not stage, commit, discard, or destructively rewrite changes unless the
  user explicitly asks.
- Do not skip, disable, weaken, or lower thresholds for checks or tests.

## Workflow

### Phase 1: Implement the supplied plan

Dispatch the implementer role with:

```text
/implement-plan
Plan artifacts:
{relative path to plan artifact}
{relative path to task artifact}
{relative path to context artifact}
```

Record its report path, deviations, task-local implementation checks, and
remaining follow-up.

### Phase 2: Clean and fix the working tree

Dispatch the fixer role with:

```text
/loop-clean-and-fix
plan_reviewers are {agent 1, agent 2, ...}
```

Record the cleanup outcome, review and planned artifact paths, round count, stop
reason, issue ledger, `resolved_findings`, focused evidence, and residual risk.
Accumulate `resolved_findings` across every Phase 2 run.

### Phase 3: Verify the original contract

Run one fresh built-in subagent with:

```text
/verify-work
Plan artifacts:
{relative path to original plan artifact}
{relative path to original task artifact}
{relative path to original context artifact}
Supplemental obligations:
{all accumulated resolved cleanup findings, or None}
```

Record the verifier report path, verdict, findings, and recommended next step.

- `complete`: continue to Phase 5.
- `complete-with-follow-up`: continue to Phase 5 only when every follow-up is
  outside the supplied plan and task list; otherwise treat it as `incomplete`.
- `incomplete`: continue to Phase 4.

### Phase 4: Repair verified gaps

Use the latest verification report. Group tightly coupled gaps, then classify
and repair each group based on complexity and decision risk, not severity alone:

- `direct`: use when the gap and bounded repair are concrete, local,
  behaviorally clear, and safely checked with focused evidence. Dispatch the
  implementer with the original artifact paths, latest verification report,
  exact gap, and focused check. Require it to repair the gap, run that check, and
  write
  `.agent-layer/tmp/fully-implement-plan.<run-id>.direct-<attempt>.report.md`;
  do not create another plan artifact set.
- `planned`: use only when the repair is exceptionally significant:
  cross-cutting, behavior-changing, architecture-sensitive, ambiguous, or
  unsafe to bound directly. Run:

  ```text
  /plan-work
  {latest verification report plus original artifact paths as source evidence}
  plan_reviewers are {agent 1, agent 2, ...}
  ```

  Record the new plan/task/context and planning report paths, then dispatch the
  implementer with:

  ```text
  /implement-plan
  Plan artifacts:
  {relative path to remaining-work plan artifact}
  {relative path to remaining-work task artifact}
  {relative path to remaining-work context artifact}
  ```

  Record the implementation report path.

Record each classification and one-line reason. A human checkpoint overrides
either path. After all repair groups finish, repeat Phases 2 and 3. One repair
attempt is one Phase 4 pass followed by cleanup and verification; stop with a
blocker if the same verified gap remains after two attempts.

### Phase 5: Report

Write the run ledger with:

1. `# Fully Implement Plan Summary`
2. `## Inputs`
3. `## Implementation Attempts`
4. `## Cleanup Rounds`
5. `## Issue Ledger`
6. `## Resolved Findings`
7. `## Verification Results`
8. `## Stop Reason`
9. `## Residual Risk`

In `## Issue Ledger`, include one Markdown table row for every issue reported by
`/loop-clean-and-fix` repair cycles when available.

Required columns:

`| Source | Round | Severity | Classification | Issue | Location | Outcome |`

Use the delegated skill's classification when available, such as accepted,
resolved, rejected, deferred, already-resolved, blocker, or unclassified. If no
issues were reported, include a single `No issues reported` row.

## Definition of done

- The run ledger contains every delegated artifact path and the final issue,
  repair, cleanup, and verification state.
- The latest verification covers the original contract plus all accumulated
  `resolved_findings` and is `complete` or acceptable
  `complete-with-follow-up`; otherwise the ledger names the blocker.

## Final handoff

Present:

1. Echo the fully-implement-plan report path.
2. State total implementation attempts, cleanup rounds, verification verdict
   and scope, stop reason, final status, and residual risk. Verification scope
   must say whether it covered the original contract and all accumulated
   `resolved_findings`.
3. Present a **Key fixes applied** table sorted by source, round, then
   severity. Example columns: `| Source | Round | Severity | Fix | Files |`.
   If no fixes were applied, still print the table with a single
   `No fixes applied` row.
4. List rejected, deferred, blocking, or repeated findings with their source and
   round numbers.
