---
name: auto-skill-loop
description: >-
  Explicit-only.
  Run a named autonomous mode through successive PR deliveries, preserving
  blocked work and centrally shipping ready deliveries.
---

# auto-skill-loop

Act as the orchestrator. Delegate all selected work; do not implement it in the
orchestrator context.

## Inputs

Require:

- a `mode` matching `references/modes/<mode>.md`
- standing merge authorization for deliveries that pass every gate below
- `planner`, `implementer`, `code_reviewer`, and `rote_worker` dispatch targets
- one or more `plan_reviewers` targets for plan-based modes
- any additional targets required by the selected mode

Read `references/mode-contract.md`, the selected mode file,
`references/blocker-classification.md`, and `references/merge-readiness.md`.
Validate the mode and all required targets before any side effect. Pass each
target unchanged through `/agent-dispatch`; do not infer or substitute targets.


## Context and isolation

When compacting, retain the caller's loop invocation and this skill text
verbatim in addition to what you would normally retain.

Write every fresh-dispatch prompt as a self-contained task. State what the
subagent must do, the authoritative files or links it should inspect, and any
required output format. Do not include internal role names or a narrative of
the parent agent's workflow. For follow-up dispatches, pass artifact and report
paths, changed files, and unresolved finding IDs.

## Initialize

Dispatch `planner` once to perform the selected mode's `Initialize` section.
Retain its state for each delivery selection.

## Loop

1. **Select.** Dispatch `planner` with the initialization state and current
   cursor to perform the mode's `Select` section. Require exactly one result:
   one selected delivery scope or proof that a complete current pass is
   exhausted. On exhaustion, terminate.

2. **Execute.** Prepare or reuse one workflow-owned delivery branch. Then
   perform the mode's `Execute` section through its required role dispatches.
   Isolate work only when the user requests it or the current checkout cannot
   safely hold the delivery.

3. **Prepare the PR.** Dispatch `rote_worker` to run `/ship-pr`, supplying the
   `implementer` target for `/fix-ci`. Continue only when it returns a
   merge-authorization request for an exact PR and head.

4. **Merge.** Resume the same `rote_worker`
   with single-use authorization for that exact PR and head, derived from the
   standing loop authorization. Continue to reconciliation after the shipping
   dispatch returns.

5. **Reconcile.** Perform the mode's `Reconcile` section against the actual
   merged, open, or preserved delivery. Then return
   to selection. With `stop_after=one-delivery`, stop after this reconciliation.

## Termination

For each blocked item, preserve useful branch or PR work and record what
condition must change.

Report why the loop ended, the smallest remaining user questions, and every
preserved branch or PR.
