---
name: ship-pr
description: >-
  Batch local repairs, ship one pull request, observe CI and review feedback,
  reply to every eligible comment, and stop for merge authorization before
  merging and cleaning up.
---

# ship-pr

Own staging, commits, pushes, and PR delivery. Run local checks and read-only
remote monitoring concurrently, but serialize working-tree mutations and batch
known repairs.

## Defaults and rules

- Base: repository default branch. Derive the title from delivered work. Fill
  `assets/pr-body-template.md` without placeholders or unused sections, write
  `.agent-layer/tmp/ship-pr-body.<run-id>.md`, and create the PR with
  `--body-file`.
- Observe every pushed head for at least 5 minutes.
- Use `.agent-layer/tmp/ship-pr-comments-<pr-number>.md` and
  `.agent-layer/tmp/ship-pr-monitor-<pr-number>.json`.
- Make one initial delivery commit when needed and one commit per accumulated
  repair batch, not per comment or failure.
- Run risk-proportional checks for each committed head. Fulfill shipping
  obligations on the first committed head unless known pending work would
  invalidate them; run them earlier when risk warrants. Reuse evidence only for
  the exact tree. The final head must pass the documented full lane through
  `/run-and-fix-all-checks`.
- Never close out with failed checks, local changes, conflicts, unprocessed
  feedback, unposted eligible replies, or failed reply audits.
- Merge only after explicit authorization for the exact PR and a fresh gate
  check.

## Comment contract

Pass one durable ledger to every `/address-pr-comments` call. Every original
non-excluded conversation comment, inline comment, and review body gets its own
row and reply. Replies begin with exactly one verdict:

- **Fixed in `<short-hash>`.** The commit materially addresses the comment.
- **No change — `<reason>`.** Give specific evidence.
- **Deferred — tracked in `<location>`.** The location exists and work is
  genuinely outside this PR.

For each eligible row, re-fetch the comment, post in its native thread when
available, re-fetch and record the reply ID/URL, then audit the current reply
and evidence package with `address-pr-comments/reviewer-prompt.md` in a fresh
built-in subagent. Reuse a passing audit while that package remains unchanged;
mark the row complete only on `pass`. Run independent audits concurrently after
preparing their packages; keep publishing and ledger edits serialized.

For `insufficient_evidence`, complete the package and audit the changed package
without posting a new reply. Feed substantive failures back to
`/address-pr-comments`; edit the reply when supported or post a correction, then
audit the changed reply and evidence.

## Workflow

### 1. Prepare and validate the initial head

Resolve branch, base, upstream, and tree state. Create a topic branch when local
changes are on the default branch. Commit the delivery once, run the checks and
shipping obligations appropriate for that head, then push and create or reuse
the PR with the prepared body file. If checks repair the tree, commit one batch
and rerun only invalidated checks. Record PR, head, push time, ledger, and
monitor state. Stop instead of creating an empty PR.

### 2. Observe the current head

Start the read-only monitor immediately:

```bash
bash <skill_dir>/scripts/monitor-pr.sh \
  --pr <pr-number> \
  --state-file .agent-layer/tmp/ship-pr-monitor-<pr-number>.json \
  --minimum-ready-seconds <minimum_ready_seconds>
```

Omit `--timeout-seconds`. Run appropriate missing local checks while monitoring;
do not repeat evidence that covers the pushed tree. Queue monitor actions while
a mutator runs and stop on a local-check blocker.

### 3. Build and publish a repair batch

Refresh comments, checks, mergeability, and ledger. Sequentially:

- run `/address-pr-comments` for new or incomplete feedback
- run `/fix-ci` for unresolved remote failures, never beside another mutator
- resolve mechanical conflicts; stop for genuine behavior or architecture
  choices

Treat focused checks from `/address-pr-comments` as evidence for the tree they
cover. Acquire only missing or invalidated evidence before the required final
lane.

Refresh remote state before committing and add newly arrived work to the same
batch. When local changes exist, commit the accumulated repair batch, then run
the focused or full checks appropriate for that head. If checks repair the tree,
commit the resulting batch and rerun only invalidated checks. Do not publish a
head before its required evidence passes.

For `remote-retry-needed` without local changes, use the repository-supported
failed-check rerun. Do not push empty commits. If the same failure returns,
send its new run evidence through `/fix-ci`; rerun again only when new evidence
still identifies a transient cause. Stop when no supported retry or justified
repair remains.

When no actionable work remains, push any unpublished local head, replace
applicable pending hashes, publish/audit eligible replies, and return to
observation. Without a new head, publish/audit remaining replies and continue
monitoring. Observe at least one monitor cycle after the latest reply.

### 4. Reconcile monitor results

- `feedback_changed`, `ci_failed`: add evidence to the repair batch.
- `merge_conflict`: repair mechanically or stop for a genuine decision.
- `timeout`: refresh state and continue while checks or observation remain.
- `pr_not_open`: investigate; stop if the PR cannot be acted on.
- `ready`: apply the closeout gate.

### 5. Close out or merge

Request merge authorization only when the current head has completed its
observation, remote checks and the full local lane pass, the tree is clean and
mergeable, a fresh fetch finds no new feedback, every non-excluded row has a
posted audited reply, and a monitor cycle followed the latest reply.

After single-use authorization, re-fetch head, checks, mergeability, comments,
and ledger. Request fresh authorization if any gate changed. Use the sole
allowed merge method or the user's explicit default; ask only when several are
allowed without a default. After confirmed merge, update the base and delete
the local and mapped remote source branches, verifying both are gone.

## Completion contract

Return the exact merge-authorization request and gate evidence, the merged PR
and verified cleanup, or a concrete blocker with PR, head, ledger, and next
decision.
