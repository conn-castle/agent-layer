---
name: auto-skill-loop
description: >-
  Run a named autonomous mode until no substantive autonomous work remains or
  the user stops it, preserving blocked work and centrally shipping ready
  deliveries.
---

# auto-skill-loop

You are the orchestrator, not a worker. Preserve your context so the loop can
keep running: choose the next transition, handle human escalation, and keep
dispatched agents on track. Delegate bounded work and retain only what you need
to direct it.

## Inputs

Require a `mode` matching one file under `references/modes/`, standing merge
authorization for deliveries that pass this workflow's gates, and these
caller-supplied dispatch targets:

- all modes: `planner`, `implementer`, `code_reviewer`, and `rote_worker`
- plan-based modes: exactly three `plan_reviewers`
- any additional roles declared by a custom mode

A target is the caller's exact self-contained agent, model, and reasoning
specification. Before any side effect, validate and show every required target.
Pass targets unchanged; never infer or substitute them. Start each role in a
fresh context. The three plan-reviewer slots are separate dispatches but may use
the same caller-chosen target.

Accept source filters, item IDs, and `stop_after=one-delivery`. Read
`references/mode-contract.md`, the selected mode,
`references/blocker-classification.md`, and `references/merge-readiness.md`.
Reject malformed modes and unsafe paths. Repository-added named mode files are
additive and cannot weaken the central rules.

## Context and isolation

Git, pull requests, and source systems are authoritative. Keep brief loop notes
under `.agent-layer/tmp/` only for what they do not show: progress through the
current source pass, work already covered or set aside in that pass, what would
unblock it, pending reconciliation, the current branch or PR, and the next
transition. Revalidate the notes against authoritative sources when resuming.

Before compaction, retain the caller's loop invocation verbatim, the current
transition, active or preserved branches and PRs, pending reconciliation,
unresolved human gates, and the next action. After compaction, reread this skill,
reconstruct authoritative state, and continue.

Write every fresh-dispatch prompt as a self-contained task. State what the
subagent must do, the authoritative files
or links it should inspect, and any required output format. Do not include
internal role names or a narrative of the parent agent's workflow. Include prior
results only when they are necessary task inputs.

## Loop

1. Reconstruct any interrupted delivery or pending reconciliation from
   authoritative sources and the loop notes. Dispatch `rote_worker` once to
   recover delivery state and initialize source bookkeeping unless the selected
   mode assigns substantive initialization to `planner`.
2. Dispatch `planner` fresh to select the next coherent work.
3. Dispatch `planner` fresh with the selected work and the complete
   `references/blocker-classification.md` contract to classify any decision.
   A single safe answer remains agent-owned. Record a genuinely human-owned
   decision under the blocker rules and immediately select independent work.
4. Dispatch `rote_worker` to prepare or reuse one workflow-owned delivery
   branch, then execute the mode. If work is blocked, preserve useful changes on
   that branch or PR and note what must change before retrying it. Continue
   compatible independent work on the same delivery branch. Use a separate
   branch or worktree only when evidence shows unsafe overlap or incompatible
   delivery topology.
5. Accumulate mutually compatible, completed work on that branch before
   opening a PR. Never open one until the delivery contains at least three
   resolved source items, 500 added-plus-deleted changed lines, or 10 changed
   files. If the source is exhausted below all three thresholds, preserve the
   branch and report it without opening a PR.
6. Only after a threshold is met, dispatch `rote_worker` to run `/ship-pr`,
   passing instructions to use the caller's exact unchanged `implementer`
   target for any `/fix-ci` work. Keep `/ship-pr` entirely inside that dispatch.
   It returns its normal exact-PR/head merge-authorization request to this
   orchestrator.
7. Before merge, dispatch `code_reviewer` fresh for the exact PR and head. Route
   simple in-scope repair findings to a fresh `implementer`, resume the same
   `rote_worker` to publish and re-establish all gates, then dispatch a fresh
   `code_reviewer` merge review for the new exact head. A genuine manual or
   external gate preserves the PR and returns the loop to independent
   selection.
8. When the merge-readiness `code_reviewer` returns `ready`, use the user's
   standing loop authorization to resume the same `rote_worker` with normal
   single-use authorization for that exact PR and head. Any head change
   invalidates it.
9. Dispatch `rote_worker` fresh to reconcile the actual merged, open, or
   preserved result with its authoritative source, then select again. With
   `stop_after=one-delivery`, stop only after reconciliation.

Do not impose iteration, time, source-size, or batch-count limits. Continue
until a complete current pass finds no substantive autonomous work, the user
interrupts, or the blocker contract requires human input and no independent
work remains.
A blocker ends only that work; select independent work instead. Ask accumulated
human questions only after a full pass finds no independent work. Report why
the loop ended, the smallest remaining questions, and any preserved branch or
PR. Never infer exhaustion from a partial pass or abandon preserved work.
