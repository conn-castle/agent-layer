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

2. Start one watcher in a managed background session. Keep it running until the
   PR merges or the workflow stops, then stop it. If it exits after a transient
   transport failure, refetch authoritative state and restart it with the same
   append-only log.

   ```bash
   bash <skill_dir>/scripts/watch-pr-events.sh \
     --repo <owner/name> \
     --pr <pr-number> \
     --log-file .agent-layer/tmp/ship-pr-events-<pr-number>.jsonl \
     --interval-seconds 300
   ```

3. Fetch the current head, checks, and mergeability with `gh`. Read comments
   with this stateless command; never infer current state from the watcher log:

   ```bash
   bash <skill_dir>/scripts/read-pr-comments.sh \
     --repo <owner/name> \
     --pr <pr-number>
   ```

   Dispatch `pr_worker` for unresolved feedback with the exact PR, head, and
   `references/address-pr-comments.md`; for a failed required check, also
   provide its evidence and `references/fix-ci.md`. The worker edits the local
   tree but does not commit, push, or post replies. Resolve mechanical conflicts
   automatically.

   The PR is ready to merge only when:

   - The PR is mergeable at its latest head.
   - At least one agent or human reviewer has posted feedback as a formal review
     or comment.
   - Every required check and repository gate is green.
   - Every eligible comment has a validated reply.
   - If the optional repository policy exists, every merge criterion it defines
     is met.

   If no agent or human reviewer posts feedback within 10 minutes of PR
   creation, stop and report that no feedback was received. If ready, continue
   to step 5. Otherwise, address the full actionable round before committing.

4. Commit and push accepted fixes. Post each supported worker-proposed reply one
   at a time: reply natively to inline comments; for conversation comments or
   review summaries, post an issue comment linking the source. Rerun the comment
   command and correct any missing or unsupported reply. Return to step 3 until
   ready. If only checks or reviews are pending, wait for the watcher.

5. Request single-use merge authorization for the exact PR and head. Report any
   substantive findings, a concise comment disposition summary, and readiness
   evidence.

6. After authorization, refetch the head, checks, mergeability, and comments.
   Confirm the local tree is complete and every eligible comment has a supported
   posted reply. If anything changed, return to step 3 and obtain new
   authorization for the resulting PR head; otherwise merge.

7. Confirm the checkout is clean, switch to the default branch, fast-forward it,
   and delete branches or worktrees created by this workflow. Preserve state and
   report any cleanup that is unsafe to perform.
