---
name: auto-skill-loop
description: >-
  Explicit-only.
  Run a named autonomous mode, repeatedly selecting, implementing, and shipping
  coherent work while preserving anything blocked.
---

# auto-skill-loop

Act as the orchestrator. Delegate all selected work; do not implement it in the
orchestrator context.

## Inputs

Require:

- a `mode` matching `references/modes/<mode>.md`
- standing authorization to merge work that passes every gate below
- `planner`, `implementer`, `fixer`, `code_reviewer`, and `rote_worker` dispatch
  targets
- one or more `plan_reviewers` targets when the mode always plans; otherwise
  they are optional until the selected work needs planning
- any additional targets required by the selected mode

Read the selected mode file and `references/merge-authorization.md`.
Validate the mode and all required targets before any side effect. Pass each
target unchanged through `/agent-dispatch`; do not infer or substitute targets.

A mode may add instructions to any stage of the core loop. Every section is
optional; an omitted section uses the core behavior unchanged.

## Context and isolation

When compacting, retain the caller's loop invocation and this skill text
verbatim in addition to what you would normally retain.

Write every fresh-dispatch prompt as a self-contained task. State what the
subagent must do, the authoritative files or links it should inspect, and any
required output format. Do not include internal role names or a narrative of
the parent agent's workflow. For follow-up dispatches, pass artifact and report
paths, changed files, and unresolved finding IDs.

## Initialize

Dispatch `planner` once to initialize progress, applying any mode-specific
initialization instructions. Its output must record what was examined and what
remains eligible.

## Loop

1. **Select.** Prompt a fresh `planner` with the selected mode, current progress,
   latest reconciliation result, and `plan_reviewers`. Require it to select as
   much eligible work as can be implemented and reviewed coherently together,
   update progress, and run `/plan-work` when substantive design is needed.
   Include any mode-specific `Execute` instructions in the plan. Terminate only
   when it proves a complete pass is exhausted under any mode-specific criteria.
   If planning is required, do not proceed without a non-empty
   `plan_reviewers` list and an `implementation-ready` result.

2. **Execute.** Prepare or reuse a branch for the selected work. For planned
   work, run `/fully-implement-plan` with the plan and context artifacts and the
   `implementer`, `fixer`, and `code_reviewer` targets. Otherwise, dispatch
   `implementer` with the selected work and any mode-specific `Execute`
   instructions. Then dispatch `code_reviewer` with
   `/review-uncommitted-code`, return material findings to `implementer`, and
   rerun affected checks.

3. **Prepare the PR.** Dispatch `rote_worker` to run `/ship-pr`. Continue only
   when it returns a merge-authorization request for an exact PR and head.

4. **Authorize.** Follow `references/merge-authorization.md` for the exact PR
   and head. On `changes-required`, dispatch `implementer` fresh with the
   findings, rerun affected checks, and return to PR preparation. Preserve a
   `blocked` PR and continue independent work. Continue to merge only on
   `authorize`.

5. **Merge.** Resume the same `rote_worker`
   with authorization for that exact PR and head, derived from the
   standing loop authorization. Continue to reconciliation after the shipping
   dispatch returns.

6. **Reconcile.** Reconcile the actual merged, open, or preserved PR with
   its source, applying the mode's `Reconcile` section when present. Then return
   to selection. With `stop_after=one-pr`, stop after this reconciliation.

## Termination

Continue whenever evidence supports a safe, in-scope next step. Preserve blocked
work and keep selecting independent work. Ask the user only after a complete
pass finds no independent work and further progress requires authority only the
user can provide or a consequential choice the available evidence cannot
resolve.

Report why the loop ended, the smallest remaining user questions, and every
preserved branch or PR.
