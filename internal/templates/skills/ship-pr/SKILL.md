---
name: ship-pr
description: >-
  Ship one pull request, monitor hosted CI and review feedback,
  follow PR policy, and request authorization before merging and cleaning up.
---

# ship-pr

Invoking this skill authorizes branch creation, staging, commits, pushes, PR
creation or updates, and eligible comment replies needed to prepare one PR for
merge. It does not authorize the merge itself.

If `references/repo-specific-pr-policy.md` exists, read it before starting the
workflow and treat it as authoritative.

## Inputs

Require a `pr_worker` dispatch target and use `/dispatch-agent` for every
dispatch. Relay any additional caller input in every `pr_worker` prompt.

Unless the user narrows the scope, include the entire current working tree.

## Workflow

1. Create a branch when repository norms require one. Commit the intended
   changes, push, and create or reuse the PR. Derive its title from the changes
   and fill `assets/pr-body-template.md`, removing unused sections and
   placeholders.

2. If the environment supports an output-triggered monitor, start one watcher
   in a managed background session, retain its task/session ID, and attach the
   monitor to wake on its events. Keep it running while actively monitoring the
   PR; do not wait for this persistent watcher to exit. Before returning to the
   caller for authorization, a blocker, or any other handoff, stop it through
   its managed session and verify it has stopped. Antigravity print mode can
   withhold a saved final response while a managed background task is running.
   If monitoring resumes, restart with the same append-only log; refetch
   authoritative state first, including after a transient transport failure.
   Stop the watcher when the PR merges or the workflow ends.

   ```bash
   bash <skill_dir>/scripts/watch-pr-events.sh \
     --repo <owner/name> \
     --pr <pr-number> \
     --log-file .agent-layer/tmp/ship-pr-events-<pr-number>.jsonl \
     --interval-seconds 60 \
     --review-deadline-seconds 600
   ```

   Without an output-triggered monitor, proceed to step 3. Whenever step 4
   requires waiting, run the same command in the foreground with
   `--exit-on-change`, reusing the log and keeping only one watcher active.
   The log detects changes between invocations and suppresses duplicate
   deadline notifications; it never replaces fresh GitHub state. Keep JSON
   payloads file-backed to avoid process argument-size limits.

3. Fetch the current head, checks, and mergeability with `gh`. Read comments
   with this stateless command; never infer current state from the watcher log:

   ```bash
   bash <skill_dir>/scripts/read-pr-comments.sh \
     --repo <owner/name> \
     --pr <pr-number>
   ```

   Dispatch `pr_worker` for unresolved feedback with the exact PR, head, and
   `references/address-pr-comments.md`; for a failed required check, also
   provide its evidence and `references/fix-ci.md`. Delegate all merge-conflict
   resolution, including mechanical conflicts, to `pr_worker` with the exact
   PR, head, base branch, and available conflict evidence. Require resolved
   changes and verification results, or an explicit blocker. The worker edits
   the local tree but does not commit, push, or post replies.

   The PR is ready to merge only when:

   - The PR is mergeable at its latest head.
   - At least one agent or human reviewer has posted feedback as a formal review
     or comment.
   - Every required check and repository gate is green.
   - Every eligible comment has a validated reply.
   - If the optional repository policy exists, every merge criterion it defines
     is met.

   Ten minutes after PR `createdAt` is a deadline, not a mandatory wait.
   Act on each wakeup and check the deadline against fresh state, including
   when resuming. If the deadline passes without agent or human reviewer
   feedback, stop and report that none was received. If ready, continue to
   step 5. Otherwise, address the full actionable round before committing.

4. Commit and push accepted fixes. Replace every worker proposal beginning
   `Fixed.` with `Fixed in <full commit SHA>.`, naming the pushed commit that
   contains that fix; preserve its explanation after the canonical prefix.
   Post each supported worker-proposed reply one at a time: reply natively to
   inline comments; for conversation comments or review summaries, post an
   issue comment linking the source. Posted disposition replies must begin
   exactly `Fixed in <full commit SHA>.`, `Deferred.`, or `Disagreed.`. Rerun
   the comment command and correct any missing, noncanonical, or unsupported
   reply. Return to step 3 until ready. If only checks or reviews are pending,
   wait for the next watcher event as described in step 2.

5. Stop the watcher and verify it has stopped before returning the single-use
   merge authorization request for the exact PR and head. Report any substantive
   findings, a concise comment disposition summary, and readiness evidence.

6. After authorization, refetch the head, checks, mergeability, and comments.
   Confirm the local tree is complete and every eligible comment has a supported
   posted reply. If anything changed, restart the watcher as described in step 2,
   return to step 3, and obtain new authorization for the resulting PR head;
   otherwise merge.

7. Confirm the checkout is clean, switch to the default branch, fast-forward it,
   and delete branches or worktrees created by this workflow. Preserve state and
   report any cleanup that is unsafe to perform.
