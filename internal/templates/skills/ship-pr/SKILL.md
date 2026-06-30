---
name: ship-pr
description: >-
  Run completed local work through PR delivery: audit changes, verify locally,
  commit, push, open PR, monitor CI, handle review comments, finish green,
  then merge after approval and clean up the branch. Use `fix-ci` for failing
  PR checks.
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

Delegate to:
- `audit-and-fix-uncommitted-changes` for pre-commit quality gates
- `repair-checks` for the local check lane, run in parallel with remote CI, when the current session has not already observed the repo-defined local lane passing after the latest changes
- `fix-ci` for CI failure diagnosis and repair
- `address-pr-comments` for review comment handling

## Continuation rule

Sub-skill returns are intermediate, not terminal. This also applies when `ship-pr` is running inside a dispatched/headless subagent: after each sub-skill return, resume the current phase. After every delegation (`audit-and-fix-uncommitted-changes`, `repair-checks`, `fix-ci`, `address-pr-comments`), continue to the next numbered step in the same turn — the sub-skill's closing summary is not ship-pr's closeout. The most common failure is stopping after `audit-and-fix-uncommitted-changes` returns, before staging and commit happen.

The loop exits only at end of Phase 8, a listed human checkpoint, or a mirrored sub-skill checkpoint (e.g., `fix-ci` halting without pushing — Phase 3 step 3c, Phase 6 step 1c).

## Global constraints

- Do not create a PR if the current branch is the default branch and there is nothing to ship.
- Run the repo's local check lane in parallel with remote CI rather than before the push: push early to start CI, then reconcile the local lane's result in Phase 3. Do not push commits that fail the commit-time fast lane (e.g. pre-commit lint and tests).
- Do not let ship-pr push CI-fix commits unless `fix-ci` found a local reproducer and observed it pass after the fix, or `fix-ci` hit a human checkpoint without pushing.
- The local lane must use the repo-documented CI-equivalent lane when one exists; do not skip it or silently downgrade to only the fast lane.
- Do not skip CI checks.
- PR feedback handling must pass the `address-pr-comments` definition of done before this skill completes.
- The skill must end with CI passing.
- Do not force-push unless explicitly instructed.

## Human checkpoints

- Required: ask when the current branch is the default branch and there are no changes to ship (no uncommitted changes, no commits ahead of the remote, and no non-default branch to PR).
- If the working tree has no uncommitted changes but the current branch is not the default branch, proceed — the user is asking for a PR of the branch's commits.
- Required: ask when PR creation fails due to an existing PR or branch conflict.
- Required: ask when CI failures persist after 3 fix-ci iterations.
- When a checkpoint involves a genuine tradeoff between substantive alternatives, present at least two options with brief pros and cons, state which you recommend and why, and let the human decide.
- Stay autonomous during normal commit, push, PR creation, CI monitoring, and comment handling.

## Orchestration loop

### Phase 1: Prepare and push (Committer)

1. Determine the current branch and the repository's default branch.
2. Run `git status --porcelain` to check for uncommitted changes.
3. If uncommitted changes exist and the current branch is the default branch:
   a. Create a new branch with a descriptive name derived from the changes (e.g., `feat/add-widget-support` or `fix/null-pointer-in-parser`).
   b. Switch to the new branch before continuing.
4. If uncommitted changes exist:
   a. Use the `audit-and-fix-uncommitted-changes` skill to stabilize the working tree.
   b. Stage all changes: `git add -A`
   c. Craft a commit message that describes the work done.
   d. Commit the changes.
5. If no uncommitted changes exist and the current branch is not the default branch, proceed — the branch's existing commits are the content to ship.
6. If no uncommitted changes exist and the current branch is the default branch, trigger a human checkpoint — there is nothing to ship.
7. Push the branch to the remote.

### Phase 2: Create the PR (PR creator)

1. Check if a PR already exists for the current branch using `gh pr view`.
2. If no PR exists:
   a. Auto-generate the PR title from the branch name and commit history, unless explicit title was provided.
   b. Auto-generate the PR body summarizing what was done, unless explicit body was provided.
   c. Create the PR: `gh pr create --title "<title>" --body "<body>" --base <base-branch>`
3. If a PR already exists, use that PR.
4. Record the PR number/URL and the current time as `start_time`.
5. Start the repo-defined local check lane in parallel (run it or delegate to `repair-checks`), unless the current session already observed it passing after the latest changes. Do not wait on it here; Phase 3 reconciles it with remote CI.

### Phase 3: Wait for CI and fix failures (CI monitor)

1. Poll CI status using `gh pr checks <pr-number>`.
2. Wait for all remote CI checks and the parallel local check lane to complete.
3. If any CI check failed:
   a. Use the `fix-ci` skill, passing the PR number.
   b. The fix-ci skill handles the internal loop of diagnose, fix, audit, commit, push, re-check.
   c. Confirm `fix-ci` satisfied its local-reproducer definition of done; if it stopped at a human checkpoint, stop here too.
4. Reconcile the local check lane once it finishes:
   a. If it surfaced a failure, fix the cause — `repair-checks` for a local-only failure, or `fix-ci` when it overlaps a failing remote check.
   b. If the fix (or the lane itself) changed files: use `audit-and-fix-uncommitted-changes` to stabilize, stage with `git add -A`, commit, push, and re-run the lane.
5. Remote CI and the local lane must both be passing before proceeding.

### Phase 4: Wait for review comments (Timer)

The review-comment wait timer starts at PR creation (`start_time` from Phase 2). Time spent waiting for CI in Phase 3 counts toward this timer.

1. Calculate elapsed time since `start_time`.
2. If less than 15 minutes (or the configured wait time) have elapsed, wait for the remaining time.
3. If the wait time has already elapsed (e.g., CI took longer than the wait period), proceed immediately.

### Phase 5: Address PR comments (Comment handler)

1. Use the `address-pr-comments` skill, passing the PR number.
2. If it reports no feedback comments, proceed.
3. If it addressed comments, continue with CI verification before closing.

### Phase 6: Final CI verification (CI monitor)

1. If changes were pushed in Phase 5:
   a. Wait for CI to complete.
   b. If CI fails, use the `fix-ci` skill again.
   c. Confirm `fix-ci` satisfied its local-reproducer definition of done; if it stopped at a human checkpoint, stop here too.
   d. Repeat until CI passes.
2. Confirm CI is green.

### Phase 7: Audit comment coverage (Comment auditor)

Independently verify that `address-pr-comments` reached its definition of done.
Do not trust the sub-skill output alone — re-fetch the PR state and validate.

1. Re-fetch PR comments, review comments, and review bodies.
2. Verify the `address-pr-comments` definition of done against the fetched PR state.
3. If any feedback comment fails that definition, run `address-pr-comments` again with the flagged comments, then repeat this audit.
4. Only proceed when every feedback comment passes the `address-pr-comments` definition of done.

### Phase 8: Close the run (Reporter)

1. Confirm:
   - CI is passing
   - every comment has a reply that passes the Phase 7 audit
   - all changes are committed and pushed
2. Summarize the PR lifecycle outcome.
3. Tell the user the exact PR number and ask them to authorize the merge (Phase 9).

### Phase 9: Merge — only on explicit human authorization

This phase does not run automatically. After Phase 8 the skill stops and waits.

**Authorization trigger.** The agent may merge the PR only when the user explicitly approves the request. Authorization is single-use and scoped to the PR requested it names.

**Merge steps (after authorization):**

1. Re-verify the PR is mergeable and CI is still green: `gh pr view <N> --json mergeable,mergeStateStatus,state`.
2. Determine the merge method without guessing:
   a. Run `gh repo view --json mergeCommitAllowed,rebaseMergeAllowed,squashMergeAllowed,viewerDefaultMergeMethod`.
   b. If exactly one method is allowed, run `gh pr merge <N>` with that explicit flag (`--merge`, `--rebase`, or `--squash`).
   c. If multiple methods are allowed and `viewerDefaultMergeMethod` names one of them, run `gh pr merge <N>` with the matching explicit flag.
   d. If multiple methods are allowed and no explicit default method is available, stop and ask the user to choose one of the allowed methods. Do not infer a method from branch names, commit history, or personal preference.
3. Do not pass `--admin`. Do not bypass branch protections. If the merge fails, surface the error and stop. Do not retry destructively.

**Post-merge cleanup (after a successful merge):**

1. Fetch PR head metadata: `gh pr view <N> --json baseRefName,headRefName,headRepository,headRepositoryOwner,isCrossRepository,state`.
2. Switch to the base branch and update it: `git checkout <base> && git pull`.
3. Delete the local branch: `git branch -d <branch>`. If `-d` refuses, re-confirm the PR is merged via `gh pr view <N> --json state` before considering `-D`; never force-delete an unmerged branch.
4. Delete the remote branch only after mapping the PR head repository to a local git remote. Use `git remote -v` to find the remote whose fetch or push URL matches `headRepository`; if no remote maps unambiguously, stop and report the unmapped head repository instead of deleting from `origin` by assumption.
5. Delete the mapped remote branch: `git push <remote> --delete <headRefName>`. If `git ls-remote --heads <remote> <headRefName>` shows it is already gone (e.g., GitHub auto-deleted it), skip this step.
6. Verify the branch is gone:
   - Local: `git branch --list <branch>` returns no output.
   - Remote: `git ls-remote --heads <remote> <headRefName>` returns no output.

**Never delete the repository's default branch under any circumstances**, regardless of what the user says.

## Guardrails

- Do not skip the audit-and-fix step before committing.
- Do not end with CI failing.
- Do not force-push or rewrite history unless explicitly instructed.
- Do not create duplicate PRs.
- Do not merge the PR unless the user explicitly approves your request to do so.
- Never delete the repository's default branch.

## Definition of done

- A PR exists for the current branch and `gh pr checks` shows every required CI check passing on the final pushed commit.
- The repo-defined local check lane passed (run in parallel with remote CI), and CI-fix commits were not pushed without a local reproducer and passing post-fix local verification.
- `address-pr-comments` reached its definition of done, and Phase 7 independently verified that result by re-fetching the PR state.
- The skill did not force-push, did not create a duplicate PR, and did not end with CI failing.
- The skill ends after Phase 8 with the PR open and green unless the user explicitly approves your request to do so; in that case the PR is merged with an explicit, unambiguous GitHub merge method and the source branch is deleted both locally and remotely.

## Final handoff

After the run:
1. Echo the PR URL.
2. Summarize: what was committed, the local check lane run in parallel with CI, CI status, comments addressed.
3. For any CI fixes, summarize the `fix-ci` local-reproducer evidence.
4. State whether all comments passed the Phase 7 audit or if any require further human attention.
5. If any comments were re-addressed during the audit, list them and explain what was corrected.
6. Request user authorization to merge, explicitly stating this PR's number. If the user has not issued approval, do not merge.
7. If a merge was performed, report the merge outcome and confirm both local and remote branch deletion succeeded.
