---
name: auto-skill-loop
description: >-
  Run an explicitly authorized autonomous loop for tracked-issue remediation or
  broad improvement: preserve blocked branches, ship and merge ready PRs, and
  continue until interrupted or work is exhausted.
---

# auto-skill-loop

Orchestrate external workers through `/agent-dispatch`; do not implement or ship
code in this context.

## Inputs and references

Require `worker_skill` (`/fix-issues` or `/improve-codebase`), `implementer`,
`shipper`, and standing merge authorization. `/fix-issues` also requires
`plan_reviewers` passed unchanged.

Read only the selected worker reference:

- `/fix-issues`: [references/fix-issues-loop.md](references/fix-issues-loop.md)
- `/improve-codebase`:
  [references/improve-codebase-loop.md](references/improve-codebase-loop.md)

Read [references/blocker-classification.md](references/blocker-classification.md)
for blocker candidates and [references/merge-readiness.md](references/merge-readiness.md)
when a PR reaches final review or merge.

## State

Maintain `.agent-layer/tmp/auto-skill-loop.<run-id>.state.md` before and after
dispatches, branch changes, pushes, PR actions, blockers, and merges. Record
only resumable state: current step, roles, branches/PRs, completed scope, recent
paths, unresolved gates, and verification evidence. Link delegated artifacts
rather than copying them. Keep worker deferrals out of ISSUES.md.

If context compacts, preserve this skill, the ledger, completed delegations,
unresolved gates, and next step.

## Loop

1. Start each attempt from a clean primary branch. Never stash or discard work;
   commit and push an attempt before leaving it.
2. Create or reuse one batch branch and dispatch the implementer with the
   selected worker skill plus ledger context. Workers leave changes uncommitted
   and unpushed.
3. Resolve routine worker checkpoints as the authorized proxy. For a genuine
   user-only decision, preserve the branch and open PR, record the blocker,
   return to primary, and start another attempt.
4. Continue the same batch until the PR gate is met, then dispatch the shipper
   with `/ship-pr` through its green open-PR endpoint. Do not delegate merging.
5. Apply the merge-readiness contract. Leave externally gated PRs open and move
   to another attempt. Merge ready PRs under standing authorization.
6. Continue until interrupted or autonomous work is exhausted; ship the final
   small tail even when it misses the normal gate.

## PR gate and guardrails

Ship when work fixes at least 3 issues, touches 5 meaningful files, changes 500
meaningful lines, fixes one high-severity security/data-loss/release/correctness
issue, or is the final autonomous tail. Ignore generated and mechanical churn
unless it is the substance.

Never delete blocked branches or PRs, weaken checks or skills, treat churn as
progress, or leave the primary checkout dirty between attempts.
