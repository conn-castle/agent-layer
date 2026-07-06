---
name: ship-pr
description: >-
  Run completed local work through PR delivery: audit changes, verify locally,
  commit, push, open PR, monitor CI and PR feedback with
  the bundled script, handle review comments, finish green, then merge after
  approval and clean up the branch. Use `/fix-ci` for failing PR checks.
---

# ship-pr

This is the PR lifecycle orchestrator.
It should:
- audit and commit any uncommitted changes
- push and create a PR
- monitor CI and PR feedback with the bundled script
- address every feedback comment
- ensure CI passes at the end

## Defaults

- Default base branch is the repository's default branch (usually `main`).
- Default PR title and body are auto-generated from the branch name, commit history, and the work that was done.
- Default minimum ready observation time is 5 minutes for each pushed head SHA.
- Default monitor timeout is 30 minutes per script run.
- If explicit PR title, body, or base branch instructions are provided, use those instead of auto-generating.

## Inputs

Accept any combination of:
- explicit PR title
- explicit PR body or description instructions
- explicit base branch
- explicit minimum ready observation time (default 5 minutes)

## Required behavior

Delegate to:
- `/audit-and-fix-uncommitted-changes` for pre-commit quality gates
- `/repair-checks` for the local check lane, run in parallel with remote CI, when the current session has not already observed the repo-defined local lane passing after the latest changes
- `/fix-ci` for CI failure diagnosis and repair
- `/address-pr-comments` for review comment handling

## Continuation rule

Sub-skill returns are intermediate, not terminal. This also applies when this skill is running inside a dispatched/headless subagent: after each sub-skill return, resume the current phase. After every delegation (`/audit-and-fix-uncommitted-changes`, `/repair-checks`, `/fix-ci`, `/address-pr-comments`), continue to the next numbered step in the same turn — the sub-skill's closing summary is not this skill's closeout. The most common failure is stopping after `/audit-and-fix-uncommitted-changes` returns, before staging and commit happen.

The loop exits only at end of Phase 8, a listed human checkpoint, or a mirrored sub-skill checkpoint (e.g., `/fix-ci` halting without pushing — Phase 3 step 3c, Phase 6 step 1c).

## Context Discipline

You are the orchestrator. Do not do the child/subagent work yourself. Your job is to preserve your context to make strategic decisions, ensure each child skill or subagent follows its assigned contract, reconcile their outputs, enforce this workflow's gates, and continue the parent workflow after every child return.

## Global constraints

- Do not create a PR if the current branch is the default branch and there is nothing to ship.
- Run the repo's local check lane in parallel with remote CI rather than before the push: push early to start CI, then reconcile the local lane's result in Phase 3. Do not push commits that fail the commit-time fast lane (e.g. pre-commit lint and tests).
- Do not let this skill push CI-fix commits unless `/fix-ci` found a local reproducer and observed it pass after the fix, or `/fix-ci` hit a human checkpoint without pushing.
- The local lane must use the repo-documented CI-equivalent lane when one exists; do not skip it or silently downgrade to only the fast lane.
- Use `bash <skill_dir>/scripts/monitor-pr.sh` for PR check and PR feedback polling. Do not hand-roll a polling loop for those phases.
- The bundled monitor script supports GitHub PR repositories through authenticated `gh`, `git`, and `jq`. If those prerequisites are missing or the project is not a GitHub PR repository, stop and report that `/ship-pr` monitoring is unsupported instead of improvising a different monitor.
- Before any repair commit or push, re-fetch live PR feedback and batch new actionable comments when feasible; pass this requirement to `/fix-ci`, `/repair-checks`, and `/address-pr-comments`.
- Do not skip CI checks.
- PR feedback handling must pass the `/address-pr-comments` definition of done before this skill completes.
- The skill must end with CI passing.
- Do not force-push unless explicitly instructed.

## Human checkpoints

- Required: ask when the current branch is the default branch and there are no changes to ship (no uncommitted changes, no commits ahead of the remote, and no non-default branch to PR).
- If the working tree has no uncommitted changes but the current branch is not the default branch, proceed — the user is asking for a PR of the branch's commits.
- Required: ask when PR creation fails due to an existing PR or branch conflict.
- Required: ask when CI failures persist after 3 /fix-ci iterations.
- Stay autonomous during normal commit, push, PR creation, CI monitoring, and comment handling.

## Orchestration loop

### Phase 1: Prepare and push (Committer)

1. Determine the current branch and the repository's default branch.
2. Run `git status --porcelain` to check for uncommitted changes.
3. If uncommitted changes exist and the current branch is the default branch:
   a. Create a new branch with a descriptive name derived from the changes (e.g., `feat/add-widget-support` or `fix/null-pointer-in-parser`).
   b. Switch to the new branch before continuing.
4. If uncommitted changes exist:
   a. Use the `/audit-and-fix-uncommitted-changes` skill to stabilize the working tree.
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
4. Record the PR number/URL and the current head SHA.
5. Start the repo-defined local check lane in parallel (run it or delegate to `/repair-checks`), unless the current session already observed it passing after the latest changes. Do not wait on it here; Phase 3 reconciles it with remote CI.

### Phase 3: Monitor CI and PR feedback (Monitor script)

The monitor exits on CI failure, feedback, merge conflict, closed PR, timeout, or green checks after the minimum ready window. The ready window is a short quiet floor for fast CI, not a fixed comment wait.

1. Run:

   ```bash
   bash <skill_dir>/scripts/monitor-pr.sh \
     --pr <pr-number> \
     --state-file .agent-layer/tmp/ship-pr-monitor-<pr-number>.json \
     --minimum-ready-seconds <minimum_ready_seconds> \
     --timeout-seconds 1800
   ```

   Use 300 seconds unless the user supplied a different minimum ready time. If the script exits non-zero before emitting JSON, stop and report stdout/stderr.
2. React to the JSON `.action`:
   a. `feedback_changed`: proceed to Phase 5. The script intentionally omits comment bodies; `/address-pr-comments` and Phase 7 own per-comment reply coverage.
   b. `ci_failed`: re-fetch live PR feedback first. If actionable feedback exists, go to Phase 5 before CI repair. Otherwise delegate to `/fix-ci` with the PR number and require it to re-fetch feedback before committing, batching new comments when feasible or returning without push if batching needs broader scope. If `/fix-ci` hits a human checkpoint, stop here too. After any push, restart the local lane and return to Phase 3.
   c. `merge_conflict`: trigger a human checkpoint unless the conflict is mechanically resolvable without changing product behavior. Do not guess through substantive conflicts.
   d. `pr_not_open`: stop and report the PR state.
   e. `timeout`: rerun if checks are pending or `.remaining_minimum_ready_seconds > 0`. If `.statuses` is empty, run `gh pr checks <pr-number>` once and rerun only if it reports pending checks. Otherwise report that no PR checks appeared within 30 minutes; do not treat the PR as green.
   f. `ready`: remote checks are terminal and the current head passed the minimum ready window; continue to step 3.
3. Reconcile the parallel local check lane before leaving this phase:
   a. If it surfaced a failure, fix the cause — `/repair-checks` for a local-only failure, or `/fix-ci` when it overlaps a failing remote check.
   b. If the fix or lane changed files, re-fetch live feedback, batch actionable comments when feasible, then audit, commit, push, and re-run the lane.
   c. If the local lane is still running when the script returns `ready`, wait for it to complete.
4. Remote CI and the local lane must both be passing before proceeding.

### Phase 4: No standalone comment wait

Do not manually sleep for comments. Phase 3 owns feedback polling and the minimum-ready floor.

### Phase 5: Address PR comments (Comment handler)

1. Use the `/address-pr-comments` skill, passing the PR number.
2. If it reports no feedback comments, proceed.
3. Tell `/address-pr-comments` to re-fetch feedback before committing and batch newly arrived actionable feedback when feasible.
4. If it addressed comments or pushed changes, return to Phase 3.

### Phase 6: Final CI verification (CI monitor)

1. Confirm the latest Phase 3 monitor result was `ready` for the current head SHA.
2. If any commit was pushed after that monitor result, return to Phase 3 instead of doing a manual CI wait.
3. Confirm CI is green.

### Phase 7: Audit comment coverage (Comment auditor)

Independently verify that `/address-pr-comments` reached its definition of done.
Do not trust the sub-skill output alone — re-fetch the PR state and validate.

1. Re-fetch PR comments, review comments, and review bodies.
2. Verify the `/address-pr-comments` definition of done against the fetched PR state.
3. If any feedback comment fails that definition, run `/address-pr-comments` again with the flagged comments. If that pushes changes, return to Phase 3; otherwise repeat this audit.
4. Only proceed when every feedback comment passes the `/address-pr-comments` definition of done.

### Phase 8: Close the run (Reporter)

1. Confirm:
   - CI is passing
   - every comment has a reply that passes the Phase 7 audit
   - all changes are committed and pushed
2. Summarize the PR lifecycle outcome.
3. Tell the user the exact PR number and ask them to authorize the merge (Phase 9).

### Phase 9: Merge — only on explicit user authorization

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
- `monitor-pr.sh` was used for remote PR check and PR feedback polling; it returned `ready` for the final head SHA after the minimum ready observation window.
- The repo-defined local check lane passed (run in parallel with remote CI), and CI-fix commits were not pushed without a local reproducer and passing post-fix local verification.
- `/address-pr-comments` reached its definition of done, and Phase 7 independently verified that result by re-fetching the PR state.
- The skill did not force-push, did not create a duplicate PR, and did not end with CI failing.
- The skill ends after Phase 8 with the PR open and green unless the user explicitly approves your request to do so; in that case the PR is merged with an explicit, unambiguous GitHub merge method and the source branch is deleted both locally and remotely.

## Final handoff

After the run:
1. Echo the PR URL.
2. Summarize: what was committed, the local check lane run in parallel with CI, CI status, comments addressed.
3. For any CI fixes, summarize the `/fix-ci` local-reproducer evidence.
4. State whether all comments passed the Phase 7 audit or if any require further user attention.
5. If any comments were re-addressed during the audit, list them and explain what was corrected.
6. Request user authorization to merge, explicitly stating this PR's number. If the user has not issued approval, do not merge.
7. If a merge was performed, report the merge outcome and confirm both local and remote branch deletion succeeded.
