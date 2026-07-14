---
name: clean-and-fix-code
description: >-
  Run one cleanup and repair pass over uncommitted working-tree changes: prune
  test changes, simplify production changes, review the diff, and directly fix
  accepted findings.
---

# clean-and-fix-code

Clean, review, and repair the current uncommitted delivery.

## Scope

The default target is staged, unstaged, and untracked changes. Explicit scope
must intersect that target. Do not expand into committed history, unrelated
issues, or a repository-wide sweep. If the target is empty, return
`no-findings`.

## Workflow

1. Inspect the combined working-tree diff. Apply each relevant checklist once:
   - `references/prune-uncommitted-tests.md` for changed tests
   - `references/simplify-uncommitted-code.md` for changed production code
2. If changes remain, run `/review-uncommitted-code` once over the complete
   target. Recover or replace an unusable review instead of treating agent
   failure as a development blocker.
3. Validate every `Recommended Accept` finding against the current tree, repair
   its root cause, make directly required test, documentation, or memory edits,
   and run credible affected checks. Apply mutations sequentially against the
   latest tree.

Do not promote `Recommended Defer` findings into scope. Defer an accepted
finding only when it depends on a genuine user decision or lacks a safe
evidence-backed repair; continue independent work.

Do not call planning, implementation, or final-verification workflows from this
skill. Do not stage, commit, discard, or destructively rewrite changes without
explicit authorization. Avoid repeating broad cleanup or review for confidence;
rerun focused evidence invalidated by a repair.

## Completion contract

Return:

- `completed` when cleanup or accepted repairs changed the target
- `no-findings` when neither did
- `blocked` when a concrete unresolved constraint prevents a safe result

Include the cleanup outcomes, review report path when run, accepted and
deferred counts, resolved findings with affected files, checks, and residual
risk. This findings list is the supplemental obligation list for callers; do
not create another one.
