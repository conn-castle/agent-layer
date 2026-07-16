---
name: ship-pr
description: >-
  Explicit-only.
  Batch local repairs, ship one pull request, observe CI and review feedback,
  reply to every eligible comment, and request merge authorization before
  merging and cleaning up.
---

# ship-pr

Publish the intended local changes in one pull request, then merge only after
explicit authorization for that exact PR and head. Invoking this skill
authorizes the normal branch, staging, commit, push, PR creation/update, and
eligible comment-reply writes needed to prepare it for merge, plus the temporary
development webhook that observes the PR during this workflow.

Serialize working-tree changes. Run independent checks, CI, review, and
monitoring concurrently when useful, and group compatible fixes.

## Pull request files

- Use the repository default base unless the user specifies another. Derive the
  title from the intended changes and fill `assets/pr-body-template.md`, removing
  placeholders and unused sections. Write
  `.agent-layer/tmp/ship-pr-body.<run-id>.md` and use it as the PR body file.
- Maintain one canonical comment ledger at
  `.agent-layer/tmp/ship-pr-comments-<pr-number>.md`. Pass that exact path to
  `/address-pr-comments`; never create a second ledger for the same PR.
- Use `.agent-layer/tmp/ship-pr-events-<pr-number>.jsonl` only as the watcher's
  append-only event log. Events wake the workflow; fresh GitHub state remains
  authoritative.
- Bind check and review evidence to the exact tree or pushed head it covers.
  Rerun only evidence invalidated by changes or shown incomplete.
- Never finish with failed required checks, uncommitted PR changes,
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

Inspect the base branch, current branch and upstream, staged and unstaged
changes, untracked files, and unpublished commits. Use the current checkout
unless the user requested a linked worktree. Create a topic branch when the
intended changes are on the default branch.

Identify exactly which changes belong in the PR and preserve unrelated work
with path- or hunk-specific staging. If the changes cannot be separated safely,
report the overlapping files. If there are no changes to publish, return
`no-changes`.

Run `/run-and-fix-all-checks`. Stage only the intended changes, create a commit,
push it, and create or reuse the PR.

### 2. Observe and repair

Start the event watcher in a managed background session after creating or
reusing the PR:

```bash
bash <skill_dir>/scripts/watch-pr-events.sh \
  --repo <owner/name> \
  --pr <pr-number> \
  --log-file .agent-layer/tmp/ship-pr-events-<pr-number>.jsonl
```

Keep one watcher running until the PR is merged or the workflow stops, then
stop it explicitly. Set a separate five-minute readiness deadline for every
pushed head and run missing local checks concurrently. Wait on watcher output;
use new log lines only to recover after losing its session. When an event
arrives or the deadline expires, fetch the current head, comments, reviews,
checks, and mergeability with `gh`. Never infer current state from the event
log.

Group compatible fixes from current GitHub state:

- use `/address-pr-comments` for feedback
- use `/fix-ci` for diagnosed remote failures
- resolve mechanical conflicts, escalating only a substantive unresolved choice

Before editing, refresh the PR so newly arrived feedback can be included. After
editing, run the affected checks and refetch the PR head and feedback. Include
compatible new feedback, rerun any checks it invalidated, stage only the
intended changes, then commit and push. Resolve a remote head change before
publishing; feedback arriving later is handled by the watcher. Do not create
empty commits to retry CI. If no safe fix or retry remains, report the exact
blocking GitHub state.

Publish and validate pending replies, then observe the resulting head and any
new feedback again. Each push resets the five-minute deadline. Continue until
the current head is stable or a confirmed external or user blocker remains.

### 3. Request merge authorization

The PR is ready only when the latest head is mergeable, required local and
remote checks pass, the working tree has no uncommitted PR changes, a fresh
fetch finds no new feedback, every eligible comment has a posted validated
reply, and its five-minute readiness deadline has elapsed.

Request single-use merge authorization naming the exact PR and head. Do not
merge on general shipping approval or authorization for an earlier head.

### 4. Merge and clean up

After authorization, refetch the expected head, checks, mergeability, and
comments, then confirm the ledger and local tree are complete. If anything
changed, return to observation or repair and obtain fresh authorization for the
new ready state.

After confirmed merge, verify the merged default-branch commit, switch the clean
checkout to the default branch, and fast-forward to that exact commit. Delete
only workflow-owned branch/worktree state after verifying ownership, cleanliness,
and the expected heads. Stop the event watcher. Preserve state and report any
unsafe cleanup skipped.

## Completion

Return `no-changes`, the exact merge-authorization request with readiness
evidence, the merged PR with verified cleanup, or the exact blocker and any
required user decision.
