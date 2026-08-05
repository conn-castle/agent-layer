---
name: ship-pr
description: >-
  Ship one pull request, monitor hosted CI and review feedback,
  follow PR policy, and request authorization before merging and cleaning up.
---

# ship-pr

Invoking this skill authorizes branch creation, staging, commits, pushes, PR
creation or updates, and eligible comment replies needed to prepare one PR for
the merge-authorization gate. It does not authorize the merge itself.

If `references/repo-specific-pr-policy.md` exists, read it before starting the
workflow and treat it as authoritative.

## Inputs

Require a `pr_worker` dispatch target. Use `/agent-dispatch` for every dispatch.

## PR worker

The `pr_worker` fixes review feedback and failed CI checks in the local working
tree without committing or pushing. It returns proposed comment replies without
posting them. Relay any additional caller input in every `pr_worker` prompt.

After committing accepted fixes, post each supported reply in its original
thread, one at a time. Then refetch the reply IDs and URLs. Before closeout,
correct any reply that current evidence shows is missing or unsupported.

## Determining the intended changes

Unless the user defines a narrower scope, include every change in the current
working tree in the PR.

## Workflow

1. Create a branch if required by repository norms. Commit the intended changes,
   push the branch, and create or reuse a PR. Derive the PR title from the
   intended changes. Fill `assets/pr-body-template.md`, removing placeholders
   and unused sections.

2. Start the polling watcher in a managed background session. Keep one watcher
   running until the PR is merged or the workflow stops, then stop it explicitly.
   If the watcher ends unexpectedly, refetch authoritative state and restart it
   with the same append-only log after a transient transport failure.

   ```bash
   bash <skill_dir>/scripts/watch-pr-events.sh \
     --repo <owner/name> \
     --pr <pr-number> \
     --log-file .agent-layer/tmp/ship-pr-events-<pr-number>.jsonl \
     --interval-seconds 300
   ```

3. Fetch the current head, comments, reviews, checks, and mergeability with
   `gh`. Never infer current state from the polling log. Determine the next steps
   based on the following rules:

   - For unresolved feedback, dispatch `pr_worker` with the exact PR and head,
     all feedback, and `references/address-pr-comments.md`.
   - For a failed required check, dispatch `pr_worker` with the exact PR and
     head, failure evidence, and `references/fix-ci.md`.
   - Resolve mechanical conflicts automatically.

   The PR is ready to merge only when:

   - The PR is mergeable at its latest head.
   - At least one agent or human reviewer has posted feedback as a formal review
     or comment.
   - Every required check and repository gate is green.
   - Every eligible comment has a validated reply.
   - If the optional repository policy exists, every merge criterion it defines
     is met.

   If no agent or human reviewer posts feedback within 10 minutes of PR
   creation, stop and tell the user that no reviewer feedback was received. If
   the PR is ready, proceed to step 5. Otherwise, address every actionable item
   locally before proceeding to step 4. Do not commit midway through a round of
   fixes.

4. Commit and push any local fixes, then post supported replies as described
   above. Return to step 3 until the PR is ready to merge. If nothing is
   actionable while checks or reviews are pending, wait for the watcher to
   report a change or reach its next polling interval.

5. Request single-use merge authorization for the exact PR and head. Report any
   substantive findings, the comment disposition summary, and the evidence that
   the PR is ready to merge.

6. After merge authorization, refetch the expected head, checks, mergeability,
   and comments, then confirm the local tree is complete and every eligible
   comment has a supported posted reply. If anything changed, return to step 3
   and obtain fresh merge authorization when reaching step 5. Otherwise, merge
   the PR.

7. Confirm the checkout is clean, switch to the default branch, and fast-forward
   it to the latest commit. Delete any branch or worktree created by this
   workflow. Preserve state and report any unsafe cleanup that was skipped.
