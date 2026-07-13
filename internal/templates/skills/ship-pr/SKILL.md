---
name: ship-pr
description: >-
  Batch local repairs, ship one pull request, observe CI and review feedback,
  reply to every eligible comment, and request merge authorization before
  merging and cleaning up.
---

# ship-pr

Ship one local delivery through a ready pull request, then merge only after
explicit authorization for that exact PR and head. Invoking this skill
authorizes the normal branch, staging, commit, push, PR creation/update, and
eligible comment-reply writes needed to reach that gate.

Serialize working-tree mutations. Run independent read-only checks, CI, review,
and monitoring concurrently when useful, and batch compatible repairs.

## Delivery and state

- Use the repository default base unless the user specifies another. Derive the
  title from the delivery and fill
  [`assets/pr-body-template.md`](assets/pr-body-template.md), removing placeholders
  and unused sections. Write `.agent-layer/tmp/ship-pr-body.<run-id>.md` and use
  it as the PR body file.
- Maintain `.agent-layer/tmp/ship-pr-comments-<pr-number>.md` as the comment
  ledger and `.agent-layer/tmp/ship-pr-monitor-<pr-number>.json` as monitor state.
- Bind check and review evidence to the exact tree or pushed head it covers.
  Rerun only evidence invalidated by changes or shown incomplete.
- Never close out with failed required checks, local delivery changes,
  conflicts, unprocessed feedback, missing eligible replies, or failed reply
  validation.

## Comment contract

Give every eligible conversation comment, inline comment, and review body one
durable ledger row and one native-thread reply. Begin replies with exactly one:

- **Fixed in `<short-hash>`.**
- **No change — `<specific reason>`.**
- **Deferred — tracked in `<existing location>`.**

Fetch all comment types, then pass new or incomplete rows together to
`/address-pr-comments`. Post supported replies serially, refetch their IDs and
URLs, and validate that every eligible row has an evidence-backed disposition
and reply. Independently audit substantive reply batches; repair or correct
unsupported replies before closeout. Agent formatting or omission failures are
recoverable and do not by themselves block shipping.

## Workflow

### 1. Prepare and publish the initial head

Resolve repository root, base, branch/upstream, staged and unstaged changes,
untracked files, and unpublished commits. Use the current checkout unless the
user requested a linked worktree. Create a workflow-owned topic branch when
delivery changes are on the default branch.

Define an exact delivery boundary and preserve unrelated work with path- or
hunk-specific staging. Block only when delivery and user work cannot be
separated safely; name the overlap. If no delivery remains, return
`no-delivery`.

Commit the delivery, run repository-required and risk-proportionate preflight,
then push and create or reuse the PR. Record the PR, head, push time, evidence,
ledger, and monitor state.

### 2. Observe and repair

Run the supplied monitor instead of model-driven polling:

```bash
bash <skill_dir>/scripts/monitor-pr.sh \
  --pr <pr-number> \
  --state-file .agent-layer/tmp/ship-pr-monitor-<pr-number>.json
```

Use the monitor's default observation window unless the repository or user
specifies another. While it runs, obtain missing local evidence that does not
duplicate checks for the same tree.

Refresh comments, checks, and mergeability after each actionable monitor result.
On `pr_not_open`, investigate the PR state and stop only when investigation
confirms no supported action remains. Build one compatible repair batch:

- use `/address-pr-comments` for feedback
- use `/fix-ci` for diagnosed remote failures
- resolve mechanical conflicts, escalating only a substantive unresolved choice

Before mutation, refresh remote state so newly arrived work can join the batch.
After mutation, re-establish the delivery boundary, stage only delivery/repair
hunks, run required invalidated checks, commit, and push. Do not use empty
commits to retry CI; use the repository-supported retry only for evidence-backed
transient failures. If no safe repair or retry remains, return the concrete
remote gate.

Publish and validate pending replies, then observe the resulting head and any
new feedback again. Continue until the current head is stable or a confirmed
external/user gate remains.

### 3. Request merge authorization

The PR is ready only when the latest head is mergeable, required local and
remote checks pass, the delivery tree is clean, a fresh fetch finds no new
feedback, and every eligible comment has a posted validated reply. Use the sole
allowed merge method or repository/user default; when neither exists, use this
workflow's documented squash default.

Request single-use merge authorization naming the exact PR and head. Do not
merge on general shipping approval or authorization for an earlier head.

### 4. Merge and clean up

After authorization, atomically refetch the expected head, checks,
mergeability, comments, ledger, and local state. If anything changed, return to
observation or repair and obtain fresh authorization for the new ready state.

After confirmed merge, verify the merged default-branch commit, switch the clean
checkout to the default branch, and fast-forward to that exact commit. Delete
only workflow-owned branch/worktree state after verifying ownership, cleanliness,
and the expected heads. Preserve state and report any unsafe cleanup skipped.

## Completion

Return `no-delivery`, the exact merge-authorization request with gate evidence,
the merged PR with verified cleanup, or a concrete blocker with PR, head,
ledger, preserved evidence, and smallest next decision.
