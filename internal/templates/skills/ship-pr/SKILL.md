---
name: ship-pr
description: >-
  Run completed local work through PR delivery: commit, push, open PR,
  monitor CI and PR feedback with the bundled script, maintain one comment
  ledger, handle review comments, finish green, then stop for merge
  authorization; if authorized, merge and clean up the branch.
---

# ship-pr

## Defaults

- Default base branch is the repository's default branch (usually `main`).
- Default PR title and body are auto-generated from the branch name, commit history, and the work that was done.
- Default minimum ready observation time is 5 minutes for each pushed head SHA.
- Default comment ledger path is `.agent-layer/tmp/ship-pr-comments-<pr-number>.md`.

## Required inputs

- `review_agents`: one or more dispatch agent roles to pass to
  `/address-pr-comments`, `/run-and-fix-all-checks`, and any delegated skill
  that requires review agents.

If `review_agents` is missing, ask for it before starting. Do not invent a
default review agent list.

## Continuation and checkpoints

Continue the current step until this skill reaches a stop gate. Delegated-skill
returns are intermediate results, not stop gates. The only stop gates are Step 5,
Step 6, and checkpoints, including mirrored sub-skill checkpoints. At a
checkpoint, report the current branch, PR number if one exists, latest head SHA
if known, the blocking condition, and the smallest user decision or
authorization needed; after the user answers, resume the same step.

## Context Discipline

You are the orchestrator for this skill. Do not do work that belongs to
subagents or delegated skills in the orchestration context. Preserve your
context to make strategic decisions, enforce gates, reconcile returned outputs,
and continue this skill's workflow after every delegation returns.

## Comment ledger

Create exactly one comment ledger for the PR and use it for the full run. After
the PR number is known, create or reuse
`.agent-layer/tmp/ship-pr-comments-<pr-number>.md`, then pass that same ledger to
every `/address-pr-comments` invocation. Do not create per-loop ledgers.

The ledger is the source of truth for comment state. It must use the ledger
schema defined by `/address-pr-comments`, including the `GitHub reply posted`
column. Preserve it across monitor loops, CI repair loops, merge-conflict
repair loops, and closeout.

`/address-pr-comments` must not commit, push, or post GitHub replies. This
skill owns those boundaries:

- When this skill has local changes ready to publish, commit immediately. Then
  push.
- Run `/run-and-fix-all-checks` from Background local CI, not as a pre-commit
  step.
- Commit and push any required local changes. Then post GitHub replies only from
  this skill.
- For `fix` rows, replace `Fixed in <pending-commit>` with the actual pushed
  short hash. Then post.
- Post a reply. Then update the ledger's `GitHub reply posted` and reply
  URL/note from fresh GitHub data.

When posting replies from the ledger:

1. Re-fetch the PR comments, review comments, and review bodies.
2. Post only eligible rows with `GitHub reply posted=no`:
   - `fix`: requires an actual pushed commit hash in the row.
   - `disagree`: requires a `No change — <reason>` reply body and no pending
     local changes from the current workflow loop.
   - `defer`: requires a real tracking location, a `Deferred — tracked in
     <location>` reply body, and no pending local changes from the current
     workflow loop.
3. Use the GitHub CLI or API:
   - review comments: `gh api repos/{owner}/{repo}/pulls/{pr-number}/comments/{comment-id}/replies -f body="<reply>"`
   - conversation comments: `gh pr comment <pr-number> --body "<reply>"`
4. Re-fetch and update the ledger with posted reply IDs/URLs.
5. Run the fresh-context reply audit using
   [`address-pr-comments/reviewer-prompt.md`](address-pr-comments/reviewer-prompt.md)
   for posted feedback rows. If any row fails, keep the row non-complete and
   feed it back into the next `/address-pr-comments` call.

## Background local CI

Optimize for fast PR feedback by parallelizing GitHub Actions and local CI.
Always commit, push, and create or confirm the PR. Then start
`/run-and-fix-all-checks`. This applies to the initial PR and every follow-up
fix commit.

Once the PR exists, run background local CI for each pushed head SHA as a
parallel diagnostic signal:

```text
/run-and-fix-all-checks
review_agents are {review agent 1, review agent 2, ...}
```

Do not treat it as a pre-commit or pre-push gate. If GitHub Actions pass for
the current head before local CI finishes, stop local CI immediately and
continue to closeout when the other gates are clean. If GitHub Actions fail,
keep local CI running when it can provide reproducer evidence for the failure.
If background local CI produces local repair changes, route them through the
repeatable workflow's local-changes path: commit immediately, push immediately,
restart background local CI for the new pushed head, and start the whole process
over.

## Orchestration loop

### Step 1: Prepare and push

1. Determine the current branch, default branch, `git status --porcelain`, and upstream ahead count.
2. If there are no uncommitted changes and the current branch is the default branch, checkpoint and report whether ahead commits exist.
3. If uncommitted changes exist on the default branch, create and switch to a branch derived from the changes.
4. If uncommitted changes exist, stage all and commit them immediately. Do not
   run `/run-and-fix-all-checks` first.
5. Push the branch to the remote immediately.
6. If a PR number and comment ledger already exist, publish eligible ledger replies whose prerequisites are now satisfied.

### Step 2: Create or use the PR

1. Use the existing branch PR if `gh pr view` finds one; otherwise create one using Defaults: `gh pr create --title "<title>" --body "<body>" --base <base-branch>`.
2. If PR creation fails due to an existing PR or branch conflict, checkpoint.
3. Record the PR number/URL and current head SHA.
4. Start or restart background checks for the current pushed head SHA now that
   the PR is created or confirmed:

   ```text
   /run-and-fix-all-checks
   review_agents are {review agent 1, review agent 2, ...}
   ```

5. Create or reuse the single comment ledger for this PR.
6. Publish eligible ledger replies whose prerequisites are already satisfied.

### Step 3: Monitor and reconcile

The monitor script is the only remote check and feedback polling loop.

1. Run:

   ```bash
   bash <skill_dir>/scripts/monitor-pr.sh \
     --pr <pr-number> \
     --state-file .agent-layer/tmp/ship-pr-monitor-<pr-number>.json \
     --minimum-ready-seconds <minimum_ready_seconds>
   ```

   Use Defaults unless the user supplied overrides. Omit `--timeout-seconds`
   so the monitor script uses its own timeout default.
2. React to the JSON `.action`:
   a. `feedback_changed`: run Step 4. The script intentionally omits comment bodies.
   b. `ci_failed`: run Step 4. Then decide whether CI repair is still needed.
   c. `merge_conflict`: run Step 4. Then decide whether merge repair is still needed.
   d. `pr_not_open`: investigate without a human checkpoint. Re-fetch PR state, branch state, and monitor state; correct stale PR selection or recoverable branch/PR state when safe. If the PR is closed or merged and cannot be acted on, stop with the evidence and do not report it as green.
   e. `timeout`: investigate without a human checkpoint. Re-fetch checks, comments, mergeability, and monitor state; run Step 4 if any actionable work exists. Rerun the monitor when checks are pending or `.remaining_minimum_ready_seconds > 0`. If no checks appear, run `gh pr checks <pr-number>` once and keep investigating based on that output; do not treat the PR as green solely because the monitor timed out.
   f. `ready`: if there are no local changes, no pending ledger replies or
      reply-audit failures, and the repeatable workflow is not running, stop
      local CI immediately and go to Step 5. If local changes or pending ledger
      work exist, run Step 4.

### Step 4: Repeatable feedback, merge-conflict, and CI workflow

Run this full workflow after monitor actions `feedback_changed`, `ci_failed`, or
`merge_conflict`, and whenever `ready` still has local changes or pending ledger
work.

Repeat these substeps until all three have no immediate work left:

1. Automatically call with the PR number and single comment ledger:

   ```text
   /address-pr-comments
   {PR number}
   {relative path to single comment ledger}
   review_agents are {review agent 1, review agent 2, ...}
   ```

   It must return the updated ledger and must not commit, push, or post GitHub
   replies.
2. Check for merge conflicts using fresh PR mergeability state and local branch
   state. Resolve mechanical conflicts when the resolution does not change
   product behavior; leave the resolution as local changes for this skill to
   commit. If a conflict requires a substantive behavior or architecture
   decision, checkpoint.
3. Check whether CI is red using fresh PR check state. If CI is red and no local
   CI repair is already pending for that failure, call with the PR number:

   ```text
   /fix-ci
   {PR number}
   review_agents are {review agent 1, review agent 2, ...}
   ```

   Require it to return local repair changes plus reproducer evidence. Then
   continue to this skill's commit/push phase. If `/fix-ci` cannot honor
   ship-pr's commit/push boundary or reaches its own checkpoint, mirror that
   checkpoint. If CI is red only because a local repair is already pending
   commit/push, treat CI as having no immediate work for this loop.

After a loop pass has no immediate comment, merge-conflict, or CI repair work:

1. If the monitor reported `.remaining_minimum_ready_seconds > 0`, wait until
   that minimum time has elapsed, then run one more full loop through the three
   substeps above.
2. If local changes exist, stage all changes and commit them immediately. Update
   `fix` ledger rows from `<pending-commit>` to the new short hash where
   applicable, then return to Step 1 so the new commit is pushed and the whole
   process starts over. Follow Background local CI for the local check lane.
3. If no local changes exist, publish eligible ledger replies, update the
   ledger from fresh GitHub data, and return to Step 3 unless the latest monitor
   result was `ready`.
4. If the latest monitor result was `ready`, no local changes exist, and the
   ledger has no pending replies or reply-audit failures, go to Step 5.

### Step 5: Closeout

1. Final gate: latest Step 3 result is `ready` for the current head, all local changes are committed and pushed, local CI has been stopped for the current head, and the single comment ledger has no pending replies or reply-audit failures.
2. Report the PR URL, checks, comment ledger path, comment outcomes, `/fix-ci` evidence if used, and any comments re-addressed during audit.
3. Ask the user to authorize merging the exact PR number. If the user has not already issued explicit approval for this PR, stop here.

### Step 6: Merge after authorization

1. Treat authorization as single-use and scoped to the named PR.
2. Re-verify the PR is mergeable and CI is still green: `gh pr view <N> --json mergeable,mergeStateStatus,state`.
3. Determine the merge method:
   a. Run `gh repo view --json mergeCommitAllowed,rebaseMergeAllowed,squashMergeAllowed,viewerDefaultMergeMethod`.
   b. If exactly one method is allowed, run `gh pr merge <N>` with that explicit flag (`--merge`, `--rebase`, or `--squash`).
   c. If multiple methods are allowed and `viewerDefaultMergeMethod` names one of them, run `gh pr merge <N>` with the matching explicit flag.
   d. If multiple methods are allowed and no explicit default method is available, stop and ask the user to choose one of the allowed methods. Do not infer a method from branch names, commit history, or personal preference.
4. Merge with the selected method.
5. Clean up the source branch:
   a. Fetch metadata: `gh pr view <N> --json baseRefName,headRefName,headRepository,headRepositoryOwner,isCrossRepository,state`.
   b. Switch to the base branch and update it: `git checkout <base> && git pull`.
   c. Delete the local branch: `git branch -d <branch>`. If `-d` refuses, re-confirm the PR is merged via `gh pr view <N> --json state` before considering `-D`.
   d. Map the PR head repository to a local remote using `git remote -v`; stop if no remote maps unambiguously.
   e. Delete the mapped remote branch: `git push <remote> --delete <headRefName>`, unless `git ls-remote --heads <remote> <headRefName>` shows it is already gone.
   f. Verify deletion:
      - Local: `git branch --list <branch>` returns no output.
      - Remote: `git ls-remote --heads <remote> <headRefName>` returns no output.
