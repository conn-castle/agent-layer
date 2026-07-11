---
name: clean-and-fix-code
description: >-
  Run one cleanup and repair pass over uncommitted working-tree changes: prune
  test changes, simplify production changes, review the diff, and directly fix
  accepted findings.
---

# clean-and-fix-code

Use only for uncommitted working-tree changes. Run each stage once, directly
address accepted findings, and stop.

## Scope

The default target is the combined uncommitted working tree:

- staged diff from `git diff --cached`
- unstaged diff from `git diff`
- untracked files from `git ls-files --others --exclude-standard`

Do not sweep old commits, unrelated known issues, or the whole repository unless
explicitly asked. If the target is empty, report `no-findings` with zero counts
and no review report instead of reviewing history.

## Workflow

### 1. Cleanup pre-passes

Run each applicable pre-pass once, sequentially, in a fresh built-in subagent:

- `assets/prune-uncommitted-tests.md` when tests or test cases changed
- `assets/simplify-uncommitted-code.md` when production code changed

Record whether either pre-pass materially changed the target, then re-evaluate
the combined uncommitted working tree. If the pre-passes emptied the target,
skip review and finish with `completed`; do not invoke a reviewer on an empty
diff.

### 2. Review once

Run `/review-uncommitted-code` directly against staged, unstaged, and untracked
changes as one target. Use its Finding Gate below. Do not launch another review
round after repairs.

### 3. Address accepted findings directly

For each accepted finding or tightly coupled finding group:

1. Validate the finding against the current working tree and repository
   evidence.
2. Diagnose and repair the root cause within the bounded target.
3. Make directly required test, documentation, or memory edits.
4. Run the narrowest credible affected checks.
5. Inspect the resulting diff against the accepted finding.

Apply fixes sequentially against the latest working tree. Resolve routine repair
and verification details directly. Stop only when a required choice materially
affects behavior, architecture, scope, risk, or cost, or when a concrete failure
prevents safe repair.

## Finding Gate

Use `/review-uncommitted-code` findings as follows:

- Fix only `### Recommended Accept` findings.
- If `### Recommended Accept` is `None`, finish with `completed` when a
  cleanup pre-pass materially changed the target; otherwise finish with
  `no-findings`.
- Do not promote `Recommended Defer`, `Recommended Reject`, or
  `Recommended Already Resolved` into repair scope. Report their counts.
- If an accepted repair depends on a user-owned decision recorded under
  `Recommended Defer`, stop and name that decision.

## Guardrails

- Do not call `/plan-work`, `/implement-plan`, or `/verify-work` from this skill.
- Do not stage, commit, discard, or destructively rewrite changes unless the
  user explicitly asks.
- Keep repairs within the uncommitted target plus directly required supporting
  edits.
- Do not parallelize mutations against shared working-tree state.

## Completion contract

Report:

- `outcome`: `no-findings`, `completed`, or `blocked`, with any blocker or
  residual risk
- outcome basis: use `completed` when a cleanup pre-pass or accepted repair
  materially changed the target; use `no-findings` only when neither did
- cleanup pre-pass outcomes, or `not applicable`
- `/review-uncommitted-code` report path, `not run — empty target`, or `not run
  — target emptied by cleanup pre-passes`
- accepted, rejected, deferred, and already-resolved counts
- `resolved_findings`: each fixed finding with title, severity, and files; use an
  empty list when none were fixed
- focused check evidence and the final diff assessment for each fixed finding

Callers may pass `resolved_findings` to final verification as supplemental
obligations. Do not create a second obligations list.
