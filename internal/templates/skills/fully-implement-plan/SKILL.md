---
name: fully-implement-plan
description: >-
  Implement a supplied plan, iteratively review and fix until diminishing
  returns, and verify completion. Use
  when the caller provides plan, task, and context artifacts and wants the local
  work finished without opening or shipping a PR or running full-repository
  testing.
---

# fully-implement-plan

Orchestrate lower-level skills and keep one skill-level report. Do not do
delegated skill work yourself.

## Required inputs

Fail before side effects unless all are present:

- plan artifact path
- task artifact path
- context artifact path
- `implementer`: dispatch agent role for `/implement-plan`
- `fixer`: dispatch agent role for `/loop-clean-and-fix`
- `plan_reviewers`: one or more dispatch agent roles to pass to
  `/loop-clean-and-fix` and any `/plan-work` retry for verification gaps

If any required input is missing, ask for it before starting. Do not invent
defaults, implementers, fixers, plan reviewer lists, or auto-select
artifacts from `.agent-layer/tmp/`.

## Required artifacts

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`. Write:

- `.agent-layer/tmp/fully-implement-plan.<run-id>.report.md`

Create the report before writing. It is the skill-level ledger for all delegated skill
results.

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
- Treat delegated skill returns as intermediate until this workflow reaches its
  final report.
- If a direct repair dispatch, `/implement-plan`, `/loop-clean-and-fix`, or
  `/verify-work` fails, stops at its own checkpoint, emits unusable output, or
  cannot provide the report path or verdict this workflow needs,
  stop this workflow, record the stop reason, and surface it to the user.
- Do not accept an `incomplete` verification verdict as a shippable final
  status.
- Accept `complete-with-follow-up` only when every follow-up is clearly outside
  the supplied plan and task list.
- Do not stage, commit, discard, or destructively rewrite changes unless the
  user explicitly asks.
- Do not skip, disable, weaken, or lower thresholds for checks or tests.

## Workflow

1. Dispatch the implementer role with:

   ```text
   /implement-plan
   Plan artifacts:
   {relative path to plan artifact}
   {relative path to task artifact}
   {relative path to context artifact}
   ```

   Record its report path, deviations, task-local implementation checks, and
   remaining follow-up.
2. Dispatch the fixer role with:

   ```text
   /loop-clean-and-fix
   plan_reviewers are {agent 1, agent 2, ...}
   ```

   Record its review and planned artifact paths, round count, stop reason, issue
   ledger, `resolved_findings`, focused evidence, and any blocker or residual
   risk.
3. Run a built-in subagent with:

   ```text
   /verify-work
   Plan artifacts:
   {relative path to plan artifact}
   {relative path to task artifact}
   {relative path to context artifact}
   Supplemental obligations:
   {resolved cleanup findings from every cleanup round, or None}
   ```

   The original plan/task/context remain authoritative. Supplemental cleanup
   obligations may add item-by-item checks but must not weaken, replace, or
   reinterpret the original contract. Treat delegated skill reports as
   evidence, not contract artifacts. Record the verifier report path, verdict,
   findings, and recommended next step.
4. If the verification verdict is `incomplete`, enter the remaining-work retry
   loop:

   1. Apply the Remaining-Work Significance Gate to the verified gaps.
   2. For a concrete local gap, dispatch the implementer with the
      original plan/task/context paths, verification report, exact bounded gap,
      and focused check. Require it to write
      `.agent-layer/tmp/fully-implement-plan.<run-id>.direct-<attempt>.report.md`,
      but do not create a second plan artifact set solely for the local repair.
   3. For an exceptionally significant gap, run `/plan-work` with:

      ```text
      /plan-work
      {verification report plus original plan/task/context paths as source evidence}
      plan_reviewers are {agent 1, agent 2, ...}
      ```

      Record the remaining-work plan, task, context, and report paths.
   4. For an exceptionally significant gap only, dispatch the implementer role
      with:

      ```text
      /implement-plan
      Plan artifacts:
      {relative path to remaining-work plan artifact}
      {relative path to remaining-work task artifact}
      {relative path to remaining-work context artifact}
      ```

      Record the implementation report path. For a direct gap, skip steps 3-4
      and use the bounded repair report from step 2.
   5. Dispatch the fixer role with:

      ```text
      /loop-clean-and-fix
      plan_reviewers are {agent 1, agent 2, ...}
      ```

      Record the cleanup outcome, review and planned artifact paths,
      `resolved_findings`, and focused evidence.
   6. Run one built-in subagent with:

      ```text
      /verify-work
      Plan artifacts:
      {relative path to plan artifact}
      {relative path to task artifact}
      {relative path to context artifact}
      Supplemental obligations:
      {all resolved cleanup findings accumulated on the final tree, or None}
      ```

      Record the verification report path and verdict. If the verdict remains
      `incomplete`, repeat this remaining-work loop unless the same gap has
      recurred after two remaining-work implementation attempts.
5. Write the final report and prepare the final message for the user.

## Remaining-Work Significance Gate

Choose based on fix complexity and decision risk, not gap severity alone.

- `direct`: the verified gap and bounded repair are concrete, local,
  behaviorally clear, and safe to check with focused evidence before the one
  final contract verifier.
- `planned`: create a remaining-work plan only when the repair is exceptionally
  significant: cross-cutting, behavior-changing, architecture-sensitive,
  ambiguous, or unsafe to bound directly.

Record the classification and one-line reason for every gap or tightly coupled
gap group. A human checkpoint still wins over either path.

## Required master report structure

Write `.agent-layer/tmp/fully-implement-plan.<run-id>.report.md` with:

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

- The skill-level report exists and records every delegated report path, stop reason,
  issue ledger, and residual risk needed to understand the outcome.
- `/implement-plan` and `/loop-clean-and-fix` ran, or the report names the
  delegated-skill blocker.
- `/verify-work` reached `complete` or acceptable `complete-with-follow-up`, or
  the report names the verification blocker.
- Final verification covered the original contract plus every accepted cleanup
  obligation on the final tree.

## Final handoff

After the run, present the results to the user in chat so that implementation,
cleanup, verification, and any task-local implementation checks are clearly
attributed to the step that produced them.

Required chat output:

1. Echo the fully-implement-plan report path.
2. State total implementation attempts, cleanup rounds, verification verdict,
   stop reason, and final status.
3. Present a **Key fixes applied** table sorted by source, round, then
   severity. Example columns: `| Source | Round | Severity | Fix | Files |`.
   If no fixes were applied, still print the table with a single
   `No fixes applied` row.
4. List rejected, deferred, blocking, or repeated findings with their source and
   round numbers.
5. State which cleanup round stopped the loop.
6. Summarize resolved cleanup findings covered by final verification.
7. Name any blocker or residual risk.
