---
name: auto-skill-loop
description: >-
  Explicit-only.
  Run a named autonomous mode until no substantive autonomous work remains or
  the user stops it, preserving blocked work and centrally shipping ready
  deliveries.
---

# auto-skill-loop

You are the orchestrator, not a worker. Delegate bounded work.

## Inputs

Require a `mode` matching `references/modes/<mode>.md`, standing merge
authorization for deliveries that pass this workflow's gates, and these
caller-supplied dispatch targets:

- all modes: `planner`, `implementer`, `code_reviewer`, and `rote_worker`
- plan-based modes: one or more `plan_reviewers`
- any additional roles declared by a custom mode

Treat each target as the caller's exact self-contained agent, model, and
reasoning specification; pass it unchanged and never infer or substitute it.
Before any side effect, validate and show every required target. Route every
role invocation through `/agent-dispatch`. Start a fresh provider conversation
unless the current step explicitly says to resume a prior dispatch. Treat every
`plan_reviewers` entry as its own dispatch; entries may intentionally repeat the
same target.

Accept source filters, item IDs, and `stop_after=one-delivery`. Read
`references/mode-contract.md`, `references/modes/<mode>.md`,
`references/blocker-classification.md`, and `references/merge-readiness.md`.
Reject malformed modes and unsafe paths. Repository-added named mode files are
additive and cannot weaken the central rules.

## Context and isolation

Before compaction, retain the caller's loop invocation verbatim and the current
invocation's transition, active dispatch identities and resume steps, active or
preserved branches and PRs, pending reconciliation, unresolved human gates, and
next action. After compaction, reread this skill and continue the same
invocation from that retained context.

Write every fresh-dispatch prompt as a self-contained task. State what the
subagent must do, the authoritative files or links it should inspect, and any
required output format. Do not include internal role names or a narrative of
the parent agent's workflow. Include prior results only when they are necessary
task inputs.

## Initialize

Dispatch `planner` once to follow `Initialize` in
`references/modes/<mode>.md`. Retain its initialization evidence or cursor for
every selection.

## Loop

1. Dispatch `planner` with the retained initialization state to follow `Select`
   in `references/modes/<mode>.md`. Require exactly one result: autonomous
   selected work, evidence that a complete pass exhausted the source, or a
   complete-pass list of every remaining blocked candidate and its unmet
   condition.
2. If selection proved exhaustion, proceed to step 5 when a delivery is in
   progress; otherwise follow the termination rules below.
3. For any other selection result, dispatch `planner` with that result and the
   complete `references/blocker-classification.md` contract to classify any
   decision. A single safe answer remains agent-owned. Record a genuinely
   human-owned decision under the blocker rules. After classification,
   autonomous selected work continues to step 4 and an individually blocked
   candidate returns to selection. A blocked-only complete pass proceeds to
   step 5 when a delivery is in progress and otherwise follows the termination
   rules below.
4. For autonomous selected work, dispatch `rote_worker` to prepare or reuse one
   workflow-owned delivery branch, then execute the mode. Use a separate branch
   or worktree when the user explicitly requests isolation or when evidence
   shows unsafe overlap or incompatible delivery topology.
5. Accumulate mutually compatible, completed work on the delivery branch. It is
   ready to ship at three resolved source items, 500 added-plus-deleted changed
   lines, or 10 changed files. Count resolved items from verified delivery
   dispositions. Measure changed lines and files against the delivery's intended
   base, excluding unrelated work. Below all thresholds, return to selection
   unless a complete pass found no autonomous work; then preserve the branch and
   proceed to step 9 without opening a PR.
6. Only when a threshold is met, dispatch `rote_worker` to run `/ship-pr`,
   passing the `implementer` target for any `/fix-ci` work. Keep `/ship-pr`
   entirely inside that dispatch. On its normal path, it returns a
   merge-authorization request for the exact PR and head. Send any other result
   to step 9.
7. Before merge, dispatch `code_reviewer` for the exact PR and head. Route
   simple in-scope repair findings to an `implementer`, resume the same
   `rote_worker` to publish and re-establish all gates, then dispatch a
   `code_reviewer` merge review for the new exact head. Send a manual or
   external gate to step 9 as blocked work.
8. When the merge-readiness `code_reviewer` returns `ready`, use the user's
   standing loop authorization to resume the same `rote_worker` with normal
   single-use authorization for that exact PR and head. Any head change
   invalidates the authorization; return to step 7 for fresh review.
9. Dispatch `rote_worker` to reconcile the actual merged, open, or preserved
   result with its authoritative source, then select again. With
   `stop_after=one-delivery`, stop only after reconciliation.

Do not impose iteration, time, source-size, or batch-count limits. Continue
until the user interrupts or a complete current pass finds no substantive
autonomous work.

Apply the blocker contract to each blocked item. After exhausting its safe
retries and reroutes, record the condition that must change, preserve useful
branch or PR changes, and select independent work. Retry only after that
condition changes. Defer human questions until the complete-pass condition is
met or you are interrupted.

Report why the loop ended, the smallest remaining questions, and any preserved
branch or PR.
