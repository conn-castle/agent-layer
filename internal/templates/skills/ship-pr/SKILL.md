---
name: ship-pr
description: >-
  Explicit-only.
  Ship one pull request, observe hosted CI and review feedback, follow PR
  policy, reply to every eligible comment, and request merge authorization
  before merging and cleaning up.
---

# ship-pr

Invoking this skill authorizes the normal branch, staging, commit, push, PR
creation/update, and eligible comment-reply writes needed to prepare one PR for
the merge-authorization gate below.

If `references/repo-specific-pr-policy.md` exists, read it before starting the
workflow and treat it as authoritative.

## Pull request files

- Use the repository default base unless the user specifies another. Derive the
  title from the intended changes and fill `assets/pr-body-template.md`,
  removing placeholders and unused sections. Write
  `.agent-layer/tmp/ship-pr-body.<run-id>.md` and use it as the PR body file.
- Use `.agent-layer/tmp/ship-pr-events-<pr-number>.jsonl` only as the polling
  watcher's append-only state-change log.
- Bind check and review evidence to the exact tree or pushed head it covers.
  Rerun only evidence invalidated by changes or shown incomplete.

## Comment integration

`/address-pr-comments` owns feedback discovery and eligibility, dispositions,
local fixes, and proposed reply content; it never commits, pushes, or posts.

After committing accepted fixes, post supported replies serially in their native
threads, then refetch their IDs and URLs. Correct any reply that fresh evidence
shows is missing or unsupported before closeout.

## Determining the intended changes

Inspect the base branch, current branch and upstream, staged and unstaged
changes, untracked files, and unpublished commits. Identify exactly which
changes belong in the PR and preserve unrelated work with path- or
hunk-specific staging. If the changes cannot be separated safely, report
the overlapping files.

## Workflow

1. Create a new branch if required, following repository norms. Then commit the
intended changes. Push the branch and create or reuse a PR.

2. Start the polling watcher in a managed background session. Keep one watcher
   running until the PR is merged or the workflow stops, then stop it explicitly.
   If the watcher ends unexpectedly, refetch authoritative state and restart it
   with the same append-only log after one transient transport failure.
```bash
bash <skill_dir>/scripts/watch-pr-events.sh \
  --repo <owner/name> \
  --pr <pr-number> \
  --log-file .agent-layer/tmp/ship-pr-events-<pr-number>.jsonl \
  --interval-seconds 300
```

3. Determine the timestamp of the last push and wait until at least 5 minutes
   has elapsed. The watcher records state changes during this review window, but
   a state change does not shorten it.

4. Fetch the current head, comments, reviews, checks, and mergeability with
   `gh`. Never infer current state from the polling log. Determine the next steps
   based on the following rules:

   - Call `/address-pr-comments` when there is unresolved feedback, batching all
     feedback
   - Call `/fix-ci` when required checks fail
   - Automatically resolve mechanical conflicts
   - Follow any repository-specific guidance in
     `references/repo-specific-pr-policy.md` if it exists
   - If the PR is ready to be merged, jump to step 6.

   A PR is ready to be merged when the latest head is mergeable, the required
   repository gates are green, all eligible comments have a validated reply,
   and when the optional policy exists, all merge criteria it defines are met.

   Do not commit until step 5. Iteratively repeat step 4, fetching the state
   after each round of fixes, until all items have been addressed locally. This
   allows new CI failures or new comments that may have arrived while you were
   working to be included in the current batch of fixes.

5. Commit and push. Go back to step 3 and repeat until all feedback is addressed
   and the PR is ready to be merged.

6. Request single-use merge authorization, specifying the exact PR and head.
   Report the final states, the comment disposition summary, and other evidence
   to the caller that the PR is ready to be merged.

7. After merge authorization, refetch the expected head, checks, mergeability,
   and comments, then confirm the local tree is complete and every eligible
   comment has a supported posted reply. If anything changed, return to step 4
   and obtain fresh merge authorization when reaching step 6. Otherwise, merge
   the PR.

8. Switch the clean checkout to the default branch, and fast-forward to the
   latest commit. Delete workflow-owned branch/worktree. Preserve state and
   report any unsafe cleanup skipped.
