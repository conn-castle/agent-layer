---
name: auto-skill-loop
description: >-
  Run an explicitly authorized autonomous loop for tracked-issue remediation or broad codebase improvement: dispatch worker agents, preserve blocked work on pushed branches, ship and merge ready PRs, and continue until interrupted or autonomous work is exhausted.
---

# auto-skill-loop

This is an orchestrator skill. Do not implement or ship code yourself.
Dispatch external roles through `/agent-dispatch`.

## Required inputs

Fail before side effects unless all are present:
- `worker_skill`: exactly `/fix-issues` or `/improve-codebase`
- `implementer`: dispatch agent role
- `fixer`: dispatch agent role
- `shipper`: dispatch agent role
- `plan_reviewers`: one or more dispatch agent roles
- explicit standing authorization for this orchestrator to merge ready PRs

Dispatch agent roles may be terse (`codex xhigh`, `claude opus high`,
`antigravity`).

Pass `plan_reviewers` to any delegated skill that uses `/review-plan`.

## References

Read the one selected worker skill contract. Do not read both.
- `/fix-issues`: [references/fix-issues-loop.md](references/fix-issues-loop.md)
- `/improve-codebase`: [references/improve-codebase-loop.md](references/improve-codebase-loop.md)

Read [references/blocker-classification.md](references/blocker-classification.md)
only when a worker returns a checkpoint or blocker candidate.

Read [references/merge-readiness.md](references/merge-readiness.md) only when a
PR exists and is ready for final review or merge.

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

## Ledger

Create `.agent-layer/tmp/auto-skill-loop.<run-id>.state.md`. Update it before
and after dispatches, branch switches, pushes, PR actions, blockers, and
merges.

Record current step, branches, PRs, requested dispatch agent roles, terminal
dispatch selections, plan reviewer dispatch roles, merged PRs, blocked branches,
blocked PRs, recent touched paths, exhausted lenses, worker questions and
answers, user-only blockers, manual gates, PR-gate status, and verification
evidence.
Record worker deferrals in this ledger or worker artifacts, not as deferral
notes in `ISSUES.md`.

## Loop

1. Start each fresh attempt from a clean primary branch checkout. Do not stash
   or discard work; if leaving an attempt, commit and push its branch first.
2. Create or reuse one batch branch for autonomous, non-blocked work.
3. Dispatch the implementer with the selected worker skill:

   ```text
   /fix-issues
   plan_reviewers are {agent 1, agent 2, ...}
   ```

   Or dispatch the implementer with `/improve-codebase`.

   Provide the current ledger context needed to avoid repeating blocked or
   exhausted work.
4. Answer worker checkpoints as the human proxy when no user-only decision is
   required. If user input is required, commit and push the branch, leave any
   PR open, record the blocker, and checkout the primary branch. Then start a
   fresh attempt from #1.
5. If the PR gate has not been met, go back to #3 and continue on the same
   batch branch with the current uncommitted work or local commits. Once the PR
   gate has been met, continue to #6.
6. Use the shipper dispatch agent role through its green, open-PR endpoint:

   ```text
   /ship-pr
   implementer is {implementer}
   fixer is {fixer}
   plan_reviewers are {agent 1, agent 2, ...}
   ```

   Do not delegate merge execution.
7. Perform final readiness review yourself. If repository policy, PR automation,
   or any external gate requires explicit manual approval, leave the PR open,
   record the gate, and checkout the primary branch. Then continue back at #1
   with a fresh attempt.
8. Merge ready PRs under this skill's standing authorization.
9. Go back to #1 and continue. Stop only when interrupted or no autonomous work
   remains. When only a final small autonomous tail remains, ship it even if it
   misses the normal size gate.

## PR gate

Open or ship a PR when at least one is true:
- fixes at least 3 issues
- touches at least 5 meaningful files
- changes at least 500 meaningful lines
- fixes a high-severity security, data-loss, release-blocking, or correctness
  issue
- ships the final remaining autonomous work before exhaustion

Generated files, lockfile churn, and mechanical dependency noise do not count
unless they are the substance of the work.

## Guardrails

- Never close or delete a blocked PR or branch.
- Never weaken checks, tests, or skill definitions to keep the loop moving.
- Never treat low-value churn as progress.
- Keep the checkout clean between attempts.
