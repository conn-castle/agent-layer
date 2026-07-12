---
name: ship-plan
description: >-
  Run a supplied implementation plan through local completion and PR shipping.
  Use when the caller provides plan, task, and context artifacts and wants
  `/fully-implement-plan` followed by `/ship-pr`.
---

# ship-plan

Orchestration-only skill. Manage inputs and delegate all work to
`/fully-implement-plan` and `/ship-pr`. Do not do delegated skill work yourself.

## Required inputs

Fail before side effects unless all are present:

- plan artifact path
- task artifact path
- context artifact path
- `implementer`: dispatch agent role for `/fully-implement-plan`
- `fixer`: dispatch agent role for `/fully-implement-plan`

If any required input is missing, ask for it before starting. Do not invent
defaults, implementers, fixers, or auto-select artifacts from
`.agent-layer/tmp/`.

An exact prior checkpoint path is an optional resume input; never discover one
implicitly. Write `.agent-layer/tmp/ship-plan.<run-id>.checkpoint.md` with the
phase, contract hashes, roles, child reports/verdicts, shipping obligations,
tree fingerprint (`HEAD` plus staged, unstaged, and untracked content),
PR/head/ledger state, and next step.

Resuming a user gate also requires the user's exact answer. Treat it as
single-use input, not checkpoint state or authorization inferred from the phase.

## Context preservation

You are the orchestrator for this skill. Preserve your context to validate
inputs, invoke the child skills, and relay their stop conditions. Treat child
skill returns as intermediate until this workflow reaches its final handoff.

## Compaction guidance

When compaction is needed, retain this skill and the checkpoint path. Reconcile
the checkpoint before acting again.

## Rules

- Do not implement, review, clean up, verify, commit, push, monitor CI, reply to
  PR comments, or merge in this orchestration context.
- If `/fully-implement-plan` fails, stops at a checkpoint, reports a blocker, or
  cannot provide the report path and final status needed for handoff, stop this
  workflow and surface that child result.
- Run `/ship-pr` only after `/fully-implement-plan` reports `complete`, or
  `complete-with-follow-up` with every follow-up explicitly outside the
  supplied contract.
- A shippable `/fully-implement-plan` result is intermediate, not terminal.
  Invoke `/ship-pr`; do not replace it with a question or completion summary.
- Stop at any `/ship-pr` checkpoint, including merge authorization.

## Workflow

1. Create or reconcile the checkpoint using phases `implementation`, `shipping`,
   `merge-authorization`, and `done`. Continue at its next step when contract
   hashes and covered tree still match. Restart implementation only for a
   contract change or unexplained pre-shipping tree change; let `/ship-pr`
   reconcile changes made during shipping.
2. When the phase is `implementation`, run:

   ```text
   /fully-implement-plan
   Plan artifacts:
   {relative path to plan artifact}
   {relative path to task artifact}
   {relative path to context artifact}
   implementer is {implementer}
   fixer is {fixer}
   ```

   On a shippable result, checkpoint its report, verdict, tree fingerprint, and
   shipping obligations; set phase `shipping` and next step `/ship-pr`.
3. When the phase is `shipping` or `merge-authorization`, run:

   ```text
   /ship-pr
   ```

   Pass checkpointed shipping obligations and PR/head/ledger state plus any
   exact gate answer. At merge authorization, checkpoint that phase with next
   step `/ship-pr`; mark `done` only on a terminal result.
4. Prepare the final message for the user.

## Definition of done

- `/fully-implement-plan` completed with a shippable result and `/ship-pr` ran,
  or a child blocker was surfaced.
- A resumed workflow reconciled its explicit checkpoint and skipped every
  completed phase whose contract and covered tree remained valid.

## Final handoff

Report the `/fully-implement-plan` report path and final status. Report the
`/ship-pr` PR URL, current PR status, comment ledger path, and checkpoint when
available. Always report the `ship-plan` checkpoint path. State any blocker,
residual risk, or user decision needed next.
