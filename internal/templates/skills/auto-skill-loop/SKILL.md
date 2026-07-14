---
name: auto-skill-loop
description: >-
  Run an explicitly authorized autonomous loop for tracked-issue remediation or
  broad improvement: preserve blocked branches, ship and merge ready PRs, and
  continue until interrupted or work is exhausted.
---

# auto-skill-loop

Run repeated improvement batches with minimal steering. Orchestrate the required
external workers through `/agent-dispatch`; do not implement or ship code in
this context. Worker output is evidence: recover valid work from incomplete
results without treating agent failure as a user blocker.

## Inputs

Require:

- `worker_skill`: `/fix-issues` or `/improve-codebase`
- `implementer`: one explicit, self-contained dispatch target specification
- `shipper`: one explicit, self-contained dispatch target specification
- standing authorization to merge pull requests that pass this workflow's gates

`/fix-issues` also requires exactly three self-contained `plan_reviewers` target
specifications, passed unchanged. Before any side effect, show the user the
exact role-to-target mapping and every plan-reviewer target specification. Ask
for any missing target; do not infer roles or target specifications.

Read the selected worker reference, plus
[`blocker-classification.md`](references/blocker-classification.md). Read
[`merge-readiness.md`](references/merge-readiness.md) before merging.

## Durable state

Maintain `.agent-layer/tmp/auto-skill-loop.<run-id>.state.md` at every branch,
push, PR, blocker, and merge boundary. Keep only what is needed to resume:
current step, roles, branches and PRs, completed scope, recently touched areas,
verification, unresolved gates, and the next action. Link other artifacts.

Preserve this state through context compaction. Keep worker-only deferrals out
of ISSUES.md.

## Loop

1. Start each attempt from a clean primary branch. Never stash or discard work;
   commit and push recoverable attempt work before leaving its branch.
2. Create or reuse one batch branch and dispatch `implementer` with the selected
   worker contract and ledger context. Resolve routine choices autonomously and
   keep compatible work in the batch.
3. When a genuine user-only decision blocks part of the work, preserve and push
   the branch, open or retain its PR when useful, record the smallest question,
   then continue with independent work.
4. Dispatch `shipper` with `/ship-pr` for a coherent, reviewable batch. Do not
   wait for an arbitrary issue, file, or line-count threshold; small high-value
   fixes and a final tail are valid batches. Do not delegate merging.
5. Apply merge readiness yourself. Leave externally gated PRs open; merge ready
   PRs under the standing authorization using `/ship-pr` mechanics.
6. Continue until interrupted or no safe, useful autonomous work remains.

Never delegate merge authorization, delete blocked branches or PRs, weaken
checks or skills, count churn as progress, or leave the primary checkout dirty
between attempts.
