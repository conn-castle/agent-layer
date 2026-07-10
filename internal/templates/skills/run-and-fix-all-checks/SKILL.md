---
name: run-and-fix-all-checks
description: >-
  Run the repository's full documented check lane from COMMANDS.md, including
  every required test and check, then repair failures with a proportional
  direct or planned workflow until the lane passes or a real blocker remains.
---

# run-and-fix-all-checks

Parent workflow for making the repo-defined full check lane green. Use the
documented commands as the source of truth, preserve failed command evidence,
route code fixes through proportional repair and verification, then rerun the
lane.

## Required inputs

- `implementer`: dispatch agent role for `/implement-plan`
- `plan_reviewers`: one or more dispatch agent roles to pass through to
  `/plan-work`

If `implementer` or `plan_reviewers` is missing, ask for it before
starting. Do not invent a default implementer or plan reviewer list.

Dispatch agent roles may be terse (`codex high`, `claude opus xhigh`,
`antigravity`).

## Scope

Default target is the whole repository's documented full check lane:

- all tests
- full CI or the closest documented local CI lane
- required format, lint, typecheck, coverage, build, docs, or pre-commit checks
  included by that lane

Read `COMMANDS.md` before choosing commands. Prefer one documented CI or
all-checks command when it exists and includes all tests. Otherwise run the
documented all-tests command plus the documented CI or full-check commands in
the order `COMMANDS.md` specifies.

If `COMMANDS.md` does not identify a full lane clearly, inspect repo tooling
only enough to resolve the lane. If ambiguity remains, stop and ask.

## Required artifacts

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`. Write:

- `.agent-layer/tmp/run-and-fix-all-checks.<run-id>.state.md`
- `.agent-layer/tmp/run-and-fix-all-checks.<run-id>.report.md`

Create both before writing. Store failed command output or distilled failure
evidence under `.agent-layer/tmp/` with the same prefix and record each path in
the state file.

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

## Workflow

For every subagent step, use a built-in subagent with fresh context. Dispatch
the requested `implementer` role for a bounded direct repair or
`/implement-plan`, as selected by the significance gate.
Dispatch external roles through `/agent-dispatch`.

1. Resolve the full check lane from `COMMANDS.md` and record the commands in
   state.
2. Run the full check lane.
3. If every command passes and no active verification gap remains, write the
   final report and finish.
4. If every command passes but the latest `/verify-work` verdict is
   `incomplete`, save the verification report and gap summary as the next
   failure artifact.
5. If a command fails, save the failing command, exit code, relevant output,
   suspected failing package/file/test when available, and any minimal
   reproduction notes as a failure artifact.
6. Apply the Repair Significance Gate.
7. For a direct repair, dispatch the implementer with the failure
   artifact, exact bounded repair target, root-cause-first standard, and focused
   check command. Require it to write
   `.agent-layer/tmp/run-and-fix-all-checks.<run-id>.repair-<cycle>.report.md`,
   but do not create plan/task/context artifacts solely for this local repair.
8. For an exceptionally significant repair, run a subagent to plan a root-cause
   fix. Do not require a separate spec when the command output is concrete enough
   to plan from:

   ```text
   /plan-work
   {relative path to failure artifact}
   plan_reviewers are {agent 1, agent 2, ...}
   ```

9. For a planned repair only, dispatch the implementer role with:

   ```text
   /implement-plan
   Plan artifacts:
   {relative path to plan artifact}
   {relative path to task artifact}
   {relative path to context artifact}
   ```

10. For a planned repair only, run a built-in subagent to verify against the
    plan, task, and context paths produced by `/plan-work`:

   ```text
   /verify-work
   Plan artifacts:
   {relative path to plan artifact}
   {relative path to task artifact}
   {relative path to context artifact}
   ```

    Treat an `incomplete` verdict as active failure evidence for the next repair
    cycle. For a direct repair, skip steps 8-10: the focused check plus the next
    full check-lane run are the verification evidence; do not create a synthetic
    plan merely to invoke `/verify-work`.
11. Go back to step 2.

## Repair Significance Gate

Choose based on fix complexity and decision risk, not failure severity alone.

- `direct`: the failure, root cause, and bounded repair are concrete, local,
  behaviorally clear, and safely checked with focused evidence plus the next
  full-lane run.
- `planned`: use `/plan-work` + `/implement-plan` + `/verify-work` only when the
  repair is exceptionally significant: large, cross-cutting,
  behavior-changing, architecture-sensitive, ambiguous, or unsafe to bound.

Record the classification and reason in the repair-cycle ledger. Preserve every
Failure Gate checkpoint.

## Pass Gate

- Finish only when the full check lane passes after the latest changes.
- If the latest `/verify-work` verdict for the active repair is `incomplete`,
  do not finish even if the check lane passes. Treat the verification gaps as
  the next repair task and apply the Repair Significance Gate.
- `complete-with-follow-up` is acceptable only when every follow-up is clearly
  outside the check failure repair.

## Failure Gate

- If a failure is caused by missing tooling, credentials, network access, or an
  external service, stop and report the blocker instead of inventing a code
  fix.
- If the same failure recurs after two completed repair cycles, stop and ask
  unless new evidence identifies a different root cause.
- If a fix would require destructive action, production access, schema changes,
  or a substantive user-facing behavior decision, stop at the human checkpoint.
- If `/plan-work`, `/implement-plan`, or `/verify-work` stops at its own
  checkpoint, stop this workflow and surface that checkpoint.

## Guardrails

- Do not do repair implementation yourself. Let the dispatched implementer own
  direct repairs or `/implement-plan` own planned edits; let `/plan-work` and
  `/verify-work` own their contracts when the significance gate selects them.
- Do not skip, disable, weaken, or lower thresholds for checks or tests.
- Do not silently downgrade from the full lane to a fast lane.
- Do not stage, commit, discard, or destructively rewrite changes unless the
  user explicitly asks.
- Keep fixes tied to observed check failures or active verification gaps.
- Keep check/repair rounds sequential because each full-lane rerun must observe
  the tree produced by the preceding repair.
- Treat delegated skill returns as intermediate until this workflow reaches the
  pass gate or a failure gate.

## Required report structure

Write `.agent-layer/tmp/run-and-fix-all-checks.<run-id>.report.md` with:

1. `# Run All Checks Summary`
2. `## Inputs`
3. `## Check Lane`
4. `## Check Rounds`
5. `## Repair Cycles`
6. `## Verification Results`
7. `## Stop Reason`
8. `## Residual Risk`

Include:

- commands selected from `COMMANDS.md`
- check round count and pass/fail result for each round
- failure artifact path for each failed round
- significance classification and reason for each repair cycle
- direct-repair report path, or plan, task, context, implementation, and
  verification report paths, as applicable
- final stop reason: `checks-passed`, `blocked`, `delegated-checkpoint`, or
  `repeated-failure`
- any blocker or residual risk

## Definition of done

- `COMMANDS.md` was read before selecting the check lane.
- The full documented check lane, including all tests and CI, ran at least once.
- Every failed check round either produced a repair cycle or a named blocker.
- Every repair cycle recorded a significance decision and ran either a bounded
  direct repair or `/plan-work`, `/implement-plan`, and `/verify-work`.
- The workflow stopped only through the pass gate or a failure gate.

## Final handoff

When the skill finishes, report:

- run-and-fix-all-checks report path
- check lane commands used
- check round count and stop reason
- repair cycle count
- final passing command evidence, or the blocker that prevented a clean finish
- direct-repair report paths or plan, implementation, and verification report
  paths for repair cycles, as applicable
