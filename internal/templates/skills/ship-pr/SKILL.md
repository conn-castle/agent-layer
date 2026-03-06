---
name: ship-pr
description: >-
  Orchestrate the full PR lifecycle: audit uncommitted changes, commit, push,
  create a PR, wait for CI (fixing failures), wait for review comments, address
  them, and ensure CI passes before finishing. Delegates to fix-ci and
  address-pr-comments sub-skills.
---

# ship-pr

This is the PR lifecycle orchestrator.
It should:
- audit and commit any uncommitted changes
- push and create a PR
- monitor CI and fix failures
- wait for review comments
- address every feedback comment
- ensure CI passes at the end

## Defaults

- Default base branch is the repository's default branch (usually `main`).
- Default PR title and body are auto-generated from the branch name, commit history, and the work that was done.
- Default comment wait time is 15 minutes from PR creation.
- If explicit PR title, body, or base branch instructions are provided, use those instead of auto-generating.

## Inputs

Accept any combination of:
- explicit PR title
- explicit PR body or description instructions
- explicit base branch
- explicit comment wait time (default 15 minutes)
- whether to skip the comment wait period

## Required behavior

Use subagents liberally when available.

Delegate to:
- `audit-and-fix-uncommitted-changes` for pre-commit quality gates
- `fix-ci` for CI failure diagnosis and repair
- `address-pr-comments` for review comment handling

## Global constraints

- Do not create a PR if there is nothing to push.
- Do not skip CI checks.
- Every PR comment must have a reply before the skill completes.
- The skill must end with CI passing.
- Do not force-push unless explicitly instructed.

## Human checkpoints

- Required: ask when the working tree has no changes and no commits ahead of the remote.
- Required: ask when PR creation fails due to an existing PR or branch conflict.
- Required: ask when CI failures persist after 3 fix-ci iterations.
- Stay autonomous during normal commit, push, PR creation, CI monitoring, and comment handling.

## Orchestration loop

### Phase 1: Prepare and push (Committer)

1. Run `git status --porcelain` to check for uncommitted changes.
2. If uncommitted changes exist:
   a. Use the `audit-and-fix-uncommitted-changes` skill to stabilize the working tree.
   b. Stage all changes: `git add -A`
   c. Craft a commit message that describes the work done.
   d. Commit the changes.
3. Push the branch to the remote.

### Phase 2: Create the PR (PR creator)

1. Check if a PR already exists for the current branch using `gh pr view`.
2. If no PR exists:
   a. Auto-generate the PR title from the branch name and commit history, unless explicit title was provided.
   b. Auto-generate the PR body summarizing what was done, unless explicit body was provided.
   c. Create the PR: `gh pr create --title "<title>" --body "<body>" --base <base-branch>`
3. If a PR already exists, use that PR.
4. Record the PR number/URL and the current time as `start_time`.

### Phase 3: Wait for CI and fix failures (CI monitor)

1. Poll CI status using `gh pr checks <pr-number>`.
2. Wait for all CI checks to complete.
3. If any CI check failed:
   a. Use the `fix-ci` skill, passing the PR number.
   b. The fix-ci skill handles the internal loop of diagnose, fix, audit, commit, push, re-check.
4. CI must be passing before proceeding.

### Phase 4: Wait for review comments (Timer)

1. Calculate elapsed time since `start_time`.
2. If less than 15 minutes (or the configured wait time) have elapsed, wait for the remaining time.
3. If the wait time has already elapsed, proceed immediately.

### Phase 5: Address PR comments (Comment handler)

1. Read all PR comments (review comments and conversation comments).
2. If there are comments to address:
   a. Use the `address-pr-comments` skill, passing the PR number and all comments.
   b. The address-pr-comments skill handles implementation, audit, commit, push, and replies.
   c. Every comment must receive a reply — do not skip or filter any comments.
3. If no comments exist, proceed.

### Phase 6: Final CI verification (CI monitor)

1. If changes were pushed in Phase 5:
   a. Wait for CI to complete.
   b. If CI fails, use the `fix-ci` skill again.
   c. Repeat until CI passes.
2. Confirm CI is green.

### Phase 7: Audit comment coverage (Comment auditor)

Independently verify that every review comment was properly handled. Do not
trust the sub-skill output alone — re-read the PR state and validate.

1. Re-fetch all PR comments (review comments, conversation comments, and review
   bodies) using the same commands from Phase 5 / the address-pr-comments skill.
2. For every feedback comment, verify:
   a. A reply exists from this agent (not just from a human or bot).
   b. If the comment's suggestion was implemented, the reply describes the
      concrete change that was made.
   c. If the comment's suggestion was declined, the reply contains a
      substantive, technically grounded justification — not a deferral or
      generic dismissal.
3. Flag any comment that fails verification:
   - **Missing reply:** the comment was never responded to.
   - **Hollow agreement:** the reply claims the change was made but no
     corresponding code change exists.
   - **Unjustified deferral:** the reply declines the suggestion by deferring
     it (e.g., "will address later", "out of scope", "tracked in backlog")
     without a genuine technical reason why implementing it now is wrong or
     harmful.
   - **Generic dismissal:** the reply is vague or batch-style rather than
     specific to the comment.
4. If any comments are flagged:
   a. Re-address them: implement the fix or write a proper justification.
   b. Audit, commit, and push the new changes.
   c. Post a new follow-up reply on each re-addressed comment. If a previous
      reply declined the suggestion but the audit caused it to be implemented,
      the new reply must acknowledge the reversal and describe the concrete
      change that was made (e.g., "On reflection, this was a valid point.
      Implemented X in Y."). Never leave a declined-reply as the last word
      when the suggestion was subsequently implemented.
   d. Re-run this phase to confirm all flags are resolved.
5. Only proceed when every feedback comment passes verification.

### Phase 8: Close the run (Reporter)

1. Confirm:
   - CI is passing
   - every comment has a reply that passes the Phase 7 audit
   - all changes are committed and pushed
2. Summarize the PR lifecycle outcome.

## Guardrails

- Do not skip the audit-and-fix step before committing.
- Do not leave any comment without a reply.
- Do not end with CI failing.
- Do not force-push or rewrite history unless explicitly instructed.
- Do not create duplicate PRs.
- Reply to every comment, including bot status comments and CI notifications.
- If a comment's suggestion is implemented after a previous reply declined it, post a new follow-up reply acknowledging the reversal and describing the change. The declined reply must never be the final word on an implemented suggestion.

## Final handoff

After the run:
1. Echo the PR URL.
2. Summarize: what was committed, CI status, comments addressed.
3. State whether all comments passed the Phase 7 audit or if any require further human attention.
4. If any comments were re-addressed during the audit, list them and explain what was corrected.
