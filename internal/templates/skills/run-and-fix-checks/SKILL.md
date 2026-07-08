---
name: run-and-fix-checks
description: >-
  Run the repository's full documented check lane, including all tests and CI,
  then plan, implement, verify, and repeat fixes until the lane passes or a
  real blocker remains.
---

# run-and-fix-checks

Parent workflow for making the repo-defined full check lane green. Use the
documented commands as the source of truth, preserve failed command evidence,
route code fixes through planning and verification, then rerun the lane.

## Required inputs

- `review_agents`: one or more dispatch agent roles to pass through to
  `/plan-work`

If `review_agents` is missing, ask for it before starting. Do not invent a
default review agent list.

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

- `.agent-layer/tmp/run-and-fix-checks.<run-id>.state.md`
- `.agent-layer/tmp/run-and-fix-checks.<run-id>.report.md`

Create both before writing. Store failed command output or distilled failure
evidence under `.agent-layer/tmp/` with the same prefix and record each path in
the state file.

## Context Discipline

You are the orchestrator for this skill. Do not do work that belongs to
subagents or delegated skills in the orchestration context. Preserve your
context to make strategic decisions, enforce gates, reconcile returned outputs,
and continue this skill's workflow after every delegation returns.

## Workflow

For every subagent step, use a built-in subagent with fresh context.

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
6. Run a subagent with `/plan-work`.
   - Use the failure artifact as the task source.
   - Pass the required `review_agents`.
   - Plan a root-cause fix for the observed failure.
   - Do not require a separate spec when the command output is concrete enough
     to plan from.
7. Run `write-code` by invoking `/implement-plan` with the plan, task, and
   context paths produced by `/plan-work`.
8. Run a subagent with `/verify-work`.
   - Verify against the plan, task, and context paths produced by `/plan-work`.
   - Treat an `incomplete` verdict as active failure evidence for the next
     repair cycle.
9. Go back to step 2.

## Pass Gate

- Finish only when the full check lane passes after the latest changes.
- If the latest `/verify-work` verdict for the active repair is `incomplete`,
  do not finish even if the check lane passes. Plan the verification gaps as the
  next repair task.
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

- Do not do delegated workflow work yourself. Let `/plan-work` own planning,
  `/implement-plan` own code edits, and `/verify-work` own contract
  verification.
- Do not skip, disable, weaken, or lower thresholds for checks or tests.
- Do not silently downgrade from the full lane to a fast lane.
- Do not stage, commit, discard, or destructively rewrite changes unless the
  user explicitly asks.
- Keep fixes tied to observed check failures or active verification gaps.
- Treat delegated skill returns as intermediate until this workflow reaches the
  pass gate or a failure gate.

## Required report structure

Write `.agent-layer/tmp/run-and-fix-checks.<run-id>.report.md` with:

1. `# Run and Fix Checks Summary`
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
- plan, task, context, implementation report, and verification report paths for
  each repair cycle
- final stop reason: `checks-passed`, `blocked`, `delegated-checkpoint`, or
  `repeated-failure`
- any blocker or residual risk

## Definition of done

- `COMMANDS.md` was read before selecting the check lane.
- The full documented check lane, including all tests and CI, ran at least once.
- Every failed check round either produced a repair cycle or a named blocker.
- Every repair cycle ran `/plan-work`, `/implement-plan`, and `/verify-work`.
- The workflow stopped only through the pass gate or a failure gate.

## Final handoff

When the skill finishes, report:

- run-and-fix-checks report path
- check lane commands used
- check round count and stop reason
- repair cycle count
- final passing command evidence, or the blocker that prevented a clean finish
- plan, implementation, and verification report paths for repair cycles
