---
name: auto-skill-loop
description: >-
  Run an explicitly authorized autonomous loop for fix-issues or improve-codebase: dispatch worker agents, preserve blocked work on pushed branches, ship and merge ready PRs, and continue until interrupted or autonomous work is exhausted.
---

# auto-skill-loop

This is a parent orchestrator skill. Do not implement code yourself.

## Required inputs

Fail before side effects unless all are present:
- `worker_skill`: exactly `fix-issues` or `improve-codebase`
- `implementer`: dispatch agent role
- `reviewer`: dispatch agent role
- `helper`: dispatch agent role
- `primary_branch`
- explicit standing authorization for this orchestrator to merge ready PRs

Dispatch agent roles may be terse (`codex xhigh`, `claude opus high`,
`antigravity`). Infer the agent only when unambiguous from the model; otherwise
fail. Before dispatching, follow `agent-dispatch`, inspect live options, and
fail if a requested override is unsupported.

## References

Read only the selected worker contract:
- `fix-issues`: [references/fix-issues-loop.md](references/fix-issues-loop.md)
- `improve-codebase`: [references/improve-codebase-loop.md](references/improve-codebase-loop.md)

Read [references/blocker-classification.md](references/blocker-classification.md)
only when a worker returns a checkpoint or blocker candidate.

Read [references/merge-readiness.md](references/merge-readiness.md) only when a
PR exists and is ready for final review or merge.

## Ledger

Create `.agent-layer/tmp/auto-skill-loop.<run-id>.state.md`. Update it before
and after dispatches, branch switches, pushes, PR actions, blockers, and
merges.

Record current step, branches, PRs, dispatch agent roles, normalized dispatch
flags, merged PRs, blocked branches, blocked PRs, recent touched paths,
exhausted lenses, worker questions and answers, user-only blockers, manual
gates, PR-gate status, and verification evidence.

## Loop

1. Start from `primary_branch` with a clean working tree. Do not stash,
   discard, or preserve work only locally.
2. Create or reuse one batch branch for autonomous, non-blocked work.
3. Dispatch the implementer with the selected worker skill and the current
   ledger context.
4. Answer worker checkpoints as the human proxy when no user-only decision is
   required. If user input is required, commit and push the branch, leave any
   PR open, record the blocker, return to `primary_branch`, and start a new
   attempt.
5. After every worker iteration, commit and push the branch before continuing.
6. Do not create a PR until the work is substantive, unless an exception in the
   worker contract applies.
7. Use the reviewer dispatch agent role for `/ship-pr`, PR feedback, final
   readiness review, and merge execution.
8. If repository policy, PR automation, or any external gate requires manual
   approval, leave the PR open, record the gate, return to `primary_branch`,
   and continue with a new attempt.
9. Merge ready PRs under this skill's standing authorization.
10. Continue until interrupted or no autonomous work remains. When only a final
    small autonomous tail remains, ship it even if it misses the normal size
    gate.

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
- Keep the checkout clean when returning to `primary_branch`.
