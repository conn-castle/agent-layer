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

- `plan_reviewers`: one or more dispatch agent roles to pass through to
  `/plan-work` if accepted findings need fixes.

If `plan_reviewers` is missing, ask for it before starting. Do not invent a
default plan reviewer list.

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
5. Apply the Significance Gate to each accepted finding or tightly coupled
   finding group.
6. For a direct fix:
   - validate the finding against the current tree
   - diagnose and repair the root cause within the bounded target
   - make directly required test, doc, or memory edits
   - run the narrowest credible affected checks
   - audit the final diff against the accepted finding
7. For an exceptionally significant fix, run a subagent to plan the accepted
   finding group. Do not require a separate spec when the findings are concrete
   enough to plan from:

   ```text
   /plan-work
   {exceptionally significant accepted review findings from the gate}
   plan_reviewers are {agent 1, agent 2, ...}
   ```

8. For an exceptionally significant planned fix, run:

   ```text
   /implement-plan
   Plan artifacts:
   {relative path to plan artifact}
   {relative path to task artifact}
   {relative path to context artifact}
   ```

9. For every planned fix, run the plan's focused checks and audit the final diff
   against every planned finding. Return the cleanup plan, accepted cleanup
   findings, and focused evidence so a calling workflow can include them in its
   final verification when needed. Do not run `/verify-work` from this skill.

## Significance Gate

Choose based on fix complexity and decision risk, not finding severity alone.

- `direct`: the root cause and bounded fix are concrete, local, behaviorally
  clear, and safe to verify with focused evidence. A High-severity defect can
  still be direct when its repair is obvious and contained.
- `planned`: use `/plan-work` + `/implement-plan` only when the fix is
  exceptionally significant: cross-cutting, behavior-changing,
  architecture-sensitive, ambiguous, unsafe to bound directly, or dependent
  on a substantive user decision. A Medium finding can require planning when
  its design risk is high.

Record the classification and one-line reason for every accepted finding or
group. Preserve all existing human checkpoints and stop before destructive or
user-owned decisions.

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

- Delegate the two cleanup pre-passes to subagents and run
  `/review-uncommitted-code` directly. Apply bounded direct fixes in this skill;
  let `/plan-work` and `/implement-plan` own their contracts when the
  Significance Gate selects them.
- Subagent and sub-skill returns are intermediate until this structure reaches
  its final step or the no-findings gate exits early.
- Do not stage, commit, discard, or destructively rewrite changes unless the
  user explicitly asks.
- Keep scope tight to the uncommitted target plus directly required support
  edits for accepted findings.
- A broad-but-clear fix is still in scope when it resolves an accepted finding
  against the working-tree target and does not trigger a human checkpoint.
- Do not run another review round after implementation.
- Run the test-pruning and simplification pre-passes sequentially because each
  can mutate the diff consumed by the next phase.
- Apply direct and planned fixes sequentially against the latest working tree;
  do not parallelize mutations or decisions that share repository state.

## Handoff

When the skill finishes, report:

- `outcome`: `no-findings`, `completed`, or `blocked`, including any blocker or
  residual risk
- cleanup pre-pass outcomes, or `not applicable`
- `/review-uncommitted-code` report path and, for planned fixes, the plan, task,
  context, and implementation report paths
- accepted, rejected, deferred, and already-resolved counts
- `resolved_findings`: every accepted finding fixed by this run, with title,
  severity, files, and `direct` or `planned` classification; use an empty list
  when no accepted findings were fixed
- focused command evidence and the final diff audit for each fixed finding;
  command evidence includes the exact command, exit status, captured output or
  artifact, and the repository state it covered

Callers may pass `resolved_findings` directly to final verification as
supplemental obligations; do not create a duplicate obligations list.
