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

- Base: repository default branch. Derive title/body from delivered work.
- Observe every pushed head for at least 5 minutes.
- Use `.agent-layer/tmp/ship-pr-comments-<pr-number>.md` and
  `.agent-layer/tmp/ship-pr-monitor-<pr-number>.json`.
- Make one initial delivery commit when needed and one commit per accumulated
  repair batch, not per comment or failure.
- Reuse current passing evidence only when it covers the exact tree.
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
available, re-fetch and record the reply ID/URL, then audit the reply once with
`address-pr-comments/reviewer-prompt.md` in a fresh built-in subagent. Mark it
complete only on `pass`. Run independent audits concurrently after preparing
their packages; keep publishing and ledger edits serialized.

For `insufficient_evidence`, complete the audit package and rerun without a new
reply. Feed substantive audit failures back to `/address-pr-comments`; edit the
reply when supported or post and audit one explicit correction.

## Workflow

### 1. Publish the initial head

Resolve branch, base, upstream, and tree state. Create a topic branch when local
changes are on the default branch. Commit the delivery once, push, and create or
reuse the PR. Record PR, URL, head, push time, ledger, and monitor state. Stop
instead of creating an empty PR when there is nothing to ship.

### 2. Observe the current head

Start the read-only monitor immediately:

```bash
bash <skill_dir>/scripts/monitor-pr.sh \
  --pr <pr-number> \
  --state-file .agent-layer/tmp/ship-pr-monitor-<pr-number>.json \
  --minimum-ready-seconds <minimum_ready_seconds>
```

Omit `--timeout-seconds`. Concurrently run `/run-and-fix-all-checks` in a fresh
built-in subagent only when current passing evidence does not cover the pushed
tree. Queue monitor actions while a mutator runs. Preserve the report, repairs,
passing evidence, and covered tree; stop on a local-check blocker.

### 3. Build and publish one repair batch

Refresh comments, checks, mergeability, and ledger. Sequentially:

- run `/address-pr-comments` for new or incomplete feedback
- run `/fix-ci` for unresolved remote failures, never beside another mutator
- resolve mechanical conflicts; stop for genuine behavior or architecture
  choices

Run `/run-and-fix-all-checks` only when the tree changed since its latest
passing evidence. Refresh remote state before committing and add newly arrived
work to the same batch.

For `remote-retry-needed` without local changes, use the repository-supported
failed-check rerun once. Do not push empty commits. Stop on an unsupported or
repeated equivalent failure without a justified repair.

When no actionable work remains, commit and push all local repairs once,
replace applicable pending hashes, publish/audit eligible replies, and return
to observation for the new head. Without local changes, publish/audit remaining
replies and continue monitoring. Observe at least one monitor cycle after the
latest reply.

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
