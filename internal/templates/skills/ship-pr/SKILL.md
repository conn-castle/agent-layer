---
name: ship-pr
description: >-
  Batch local repairs, ship one pull request, observe CI and review feedback,
  reply to every eligible comment, and stop for merge authorization before
  merging and cleaning up.
---

# ship-pr

Run staging, commits, pushes, pull-request delivery, and merge continuation as
a root-owned local procedure. Run local checks, remote review, continuous
integration, and read-only monitoring concurrently when independent; serialize
working-tree mutations and batch known repairs.

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
- Run focused preflight, then push and open the pull request promptly. Normally
  run the documented full lane once on the final stable head while remote gates
  and review proceed. Rerun only for changed coverage, failure reproduction,
  environment diagnosis, or missing/invalid evidence. Reuse evidence only for
  its exact tree fingerprint.
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

Fetch all eligible comment types once per cycle, prepare one ledger, and pass
the full batch to `/address-pr-comments`. Post replies serially in native
threads, then refetch once and record reply IDs/URLs. Validate mechanical ledger
invariants deterministically. For every newly posted or changed reply or
evidence-package batch, run one built-in auditor with the canonical
`reviewer-prompt.md` from `/address-pr-comments` and a complete package for each
row. Require exactly one result per input row and record each verdict in the
ledger; never launch an auditor per row.

For `insufficient_evidence`, complete the package without posting a new reply.
Feed substantive failures back to `/address-pr-comments`; edit the reply when
supported or post a correction. Audit only the changed failed-row package
before closeout.

## Workflow

### 1. Prepare and validate the initial head

Resolve branch, base, upstream, and tree state. Create a topic branch when local
changes are on the default branch. Commit the delivery once, run the checks and
shipping obligations appropriate for that head, then push and create or reuse
the PR with the prepared body file. If checks repair the tree, commit one batch
and rerun only invalidated checks. Record PR, head, push time, ledger, and
monitor state. Stop instead of creating an empty PR.

### 2. Observe the current head

Start the read-only monitor immediately and wait on this single blocking
process; do not run model-driven sleep, process, GitHub, or status polls:

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

### 5. Close out or merge atomically

When the current head has completed its observation, remote checks and the full
local lane pass, the tree is clean and mergeable, a fresh fetch finds no new
feedback, every non-excluded row has a posted audited reply, and a monitor cycle
followed the latest reply, resolve the merge method. Use the sole
repository-allowed method or an explicit repository/user default. If multiple
methods remain and no default exists, ask the user to choose one and resume this
phase with that single-use answer; the method choice is not merge authorization.
Then request single-use merge authorization for the exact ready head.

After single-use authorization, perform one uninterrupted continuation:
re-fetch the expected head, checks, mergeability, all eligible comments, ledger,
and clean local state; abort before merge if any gate changed. After confirmed
merge, update a base checkout only when it is separate from the caller checkout
and is workflow-owned or verified clean. Otherwise leave the caller checkout
unchanged and report that the base was not updated. Remove only workflow-owned
worktree/local/remote source state, verifying each cleanup.

## Completion contract

Return the exact merge-authorization request and gate evidence, the merged PR
and verified cleanup, or a concrete blocker with PR, head, ledger, and next
decision.
