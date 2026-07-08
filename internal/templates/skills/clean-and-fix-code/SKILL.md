---
name: clean-and-fix-code
description: >-
  Run a lightweight, non-iterative cleanup/fix pass over uncommitted
  working-tree changes: prune uncommitted test changes, simplify changed
  production code, review the diff, and fix accepted review findings.
---

# clean-and-fix-code

Use only for uncommitted working-tree changes. Do not sweep old commits, the
whole repository, or unrelated known issues unless explicitly asked. Run each
phase once, then stop.

## Required inputs

- `review_agents`: one or more dispatch agent roles to pass through to
  `/plan-work` if accepted findings need fixes.

If `review_agents` is missing, ask for it before starting. Do not invent a
default review agent list.

## Scope

Default target is the full uncommitted working tree:

- staged diff from `git diff --cached`
- unstaged diff from `git diff`
- untracked files from `git ls-files --others --exclude-standard` (Include
  untracked files in the default target.)

Review and fix staged, unstaged, and untracked files as one combined change set.
If the target is empty, stop and ask instead of reviewing history.

## Workflow

For every subagent step, use a built-in subagent with fresh context.

1. Run a subagent with the prompt defined in
   `assets/prune-uncommitted-tests.md`.
   - Run only when the diff adds or modifies test files or test cases.
2. Run a subagent with the prompt defined in
   `assets/simplify-uncommitted-code.md`.
   - Run only when the diff adds or modifies production code.
3. Run `/review-uncommitted-code` directly (not as a subagent).
   - Pass the combined target: staged diff, unstaged diff, and untracked files.
4. Apply the Finding Gate.
5. Run a subagent with `/plan-work`.
   - Use the accepted review findings from the gate as the task source.
   - Pass the required `review_agents`.
   - Plan to fix all findings regardless of severity.
   - Do not require a separate spec when the findings are concrete enough to
     plan from.
6. Run `/implement-plan` with the plan, task, and context paths produced by
   `/plan-work`.
7. Run a subagent with `/verify-work`.
   - Verify against the plan that fixes the findings.

## Finding Gate

For this skill, "findings" means `/review-uncommitted-code` findings under
`### Recommended Accept`.

- If `### Recommended Accept` is `None`, finish the skill after reporting the
  review report path and cleanup built-in subagent outcomes.
- Continue only with `### Recommended Accept` findings.
- If `### Recommended Defer` contains anything that blocks planning or fixing,
  stop for the human checkpoint; do not treat the run as clean.
- Ignore `### Recommended Reject` and `### Recommended Already Resolved` for fix
  planning, but mention their counts in the final handoff when available.

## Guardrails

- Do not do delegated workflow work yourself. Delegate the two cleanup pre-passes to
  subagents, run `/review-uncommitted-code` directly, then let `/plan-work`,
  `/implement-plan`, and `/verify-work` own their contracts.
- Subagent and sub-skill returns are intermediate until this structure reaches
  its final step or the no-findings gate exits early.
- Do not stage, commit, discard, or destructively rewrite changes unless the
  user explicitly asks.
- Keep scope tight to the uncommitted target plus directly required support
  edits for accepted findings.
- A broad-but-clear fix is still in scope when it resolves an accepted finding
  against the working-tree target and does not trigger a human checkpoint.
- Do not run another review round after implementation. If `/verify-work`
  reports incomplete work, surface that result and stop.

## Handoff

When the skill finishes, report:

- cleanup subagent outcomes, or `not applicable`
- `/review-uncommitted-code` report path and whether the no-findings gate exited
- reject, defer, and already-resolved counts when available
- `resolved_findings`: every accepted finding fixed by this run, with title,
  severity, and files; use an empty list when no accepted findings were fixed
- if fixes ran: plan, task, context, implementation report, and verification
  report paths, plus final `/verify-work` verdict
