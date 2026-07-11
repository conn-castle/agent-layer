---
name: ship-pr
description: >-
  Batch local repairs, ship one pull request, observe CI and review feedback long
  enough to address it, reply to every eligible comment, and stop for merge
  authorization before merging and cleaning up.
---

# ship-pr

Own every commit and push for this workflow. Optimize wall-clock time by running
local CI and remote monitoring concurrently, but serialize all working-tree
mutations and batch currently known repairs into as few commits as possible.

## Defaults

- Base branch: repository default branch.
- Pull request title and body: derive from the branch, commits, and delivered
  work.
- Minimum observation time: 5 minutes for every pushed head SHA.
- Comment ledger: `.agent-layer/tmp/ship-pr-comments-<pr-number>.md`.
- Monitor state: `.agent-layer/tmp/ship-pr-monitor-<pr-number>.json`.

## Rules

- `ship-pr` is the sole owner of staging, committing, and pushing during this
  workflow. Child skills must return uncommitted local changes and evidence.
- Make one initial delivery commit when needed, then at most one commit for each
  accumulated repair batch. Do not commit each comment or CI fix separately.
- Treat local CI, remote CI, review comments, and merge conflicts as inputs to
  the same repair batch for the current head.
- Only one actor may mutate the working tree at a time. The remote monitor may
  run concurrently because it is read-only.
- Every pushed head must satisfy the minimum observation time. Do not shorten it
  because checks finish early.
- Never close out with unprocessed feedback, an unposted eligible reply, or a
  failed reply audit.
- Merge only after explicit authorization for the exact pull request and after
  all gates are freshly rechecked.

## Comment ledger and reply contract

Create or reuse exactly one ledger for the pull request and pass it to every
`/address-pr-comments` call. The ledger schema and comment decisions are owned
by that skill; this skill owns publishing state.

Every original non-excluded conversation comment, inline review comment, and
review body must have its own ledger row and its own reply. Do not combine
several comments into a generic response.

An eligible reply must begin with exactly one required bold verdict:

- **Fixed in `<short-hash>`.** The pushed commit must materially address the
  comment.
- **No change — `<reason>`.** The reason must be specific and evidence-backed.
- **Deferred — tracked in `<location>`.** The tracking location must exist and
  the work must genuinely be outside this pull request.

For every eligible row:

1. Re-fetch the original comment and current replies.
2. Post the row's reply in its native inline thread when one exists; otherwise
   post a dedicated pull request conversation reply that identifies the
   original comment or review body.
3. Re-fetch GitHub data, record the reply ID or URL, and set the row to
   `reply_posted_pending_audit`.
4. Run `address-pr-comments/reviewer-prompt.md` in a fresh built-in subagent
   with that row's original comment, posted reply, and verdict-specific
   evidence required by the asset.
5. Record the audit verdict. Set the row to `complete` only when it returns
   `pass`.

For `insufficient_evidence`, complete the audit package and rerun the audit
without posting another reply. Feed every substantive failed audit back into
`/address-pr-comments` with its evidence. Edit the posted reply when the GitHub
surface supports it; otherwise post one explicit corrective reply, record its
URL or ID, and audit that reply. A row remains incomplete until an audit passes;
generic acknowledgements never satisfy the contract.

## Workflow

### 1. Publish the initial head

1. Resolve the current branch, default branch, upstream, and working-tree state.
2. If local changes are on the default branch, create a topic branch.
3. Commit all current delivery changes once, push the branch, and create or
   reuse its pull request.
4. Record the pull request number, URL, head SHA, push time, ledger path, and
   monitor state path.

If there is no local work and no ahead commit to ship, stop with the concrete
state instead of creating an empty pull request.

### 2. Start concurrent observation for the head

For the current pushed head, start the read-only monitor immediately. Start
`/run-and-fix-all-checks` concurrently only when no current passing full-lane
evidence covers the exact pushed tree:

- the read-only monitor:

  ```bash
  bash <skill_dir>/scripts/monitor-pr.sh \
    --pr <pr-number> \
    --state-file .agent-layer/tmp/ship-pr-monitor-<pr-number>.json \
    --minimum-ready-seconds <minimum_ready_seconds>
  ```

- `/run-and-fix-all-checks` in a built-in subagent, when required

Omit `--timeout-seconds` so the script uses its documented default. While local
CI runs, queue monitor actions but do not launch another mutator. Local CI may
repair its failures directly, but it must leave changes uncommitted for this
skill.

When current passing evidence already covers the pushed tree, reuse it instead
of running the lane again. If a new local CI run is blocked, stop with its
evidence. Otherwise, preserve its report and any local repairs, the final
passing evidence, and the exact working-tree state that evidence covers. Then
reconcile the latest queued remote state.

### 3. Build one repair batch

Refresh comments, required checks, mergeability, and ledger state. Sequentially
address all currently known work:

- Run `/address-pr-comments` with the pull request and single ledger for new or
  incomplete feedback.
- Run `/fix-ci` for a remote failure not already resolved by pending local
  repairs. It must return uncommitted changes and local reproducer evidence.
- Resolve mechanical merge conflicts directly. Stop for a user-owned behavior
  or architecture decision.

After applying known work, compare the current working-tree state with the state
covered by the latest passing full-lane evidence. Run
`/run-and-fix-all-checks` over the combined local batch only when the tree has
changed since that passing run. Keep any repairs in the same batch. Reuse the
passing result when it already covers the current tree.

Refresh remote comments and state again before committing. If new actionable
work appeared, add it to the batch and rerun only the evidence invalidated by
those changes.

If `/fix-ci` returns `remote-retry-needed` with no local change, use the
repository-supported mechanism to rerun only the failed remote checks once for
that evidence-equivalent failure on the current head, then restart monitoring.
If no supported rerun exists, or the rerun produces the same failure and a
second local diagnosis still cannot justify a repair, stop with the collected
evidence. Do not push an empty commit to trigger CI.

If `/fix-ci` returns `blocked` or `repeated-failure`, stop with its evidence
unless another already-applied repair in the current batch directly invalidates
that failure evidence.

When no currently known actionable work remains:

- If local changes exist, stage all of them, create one repair commit, push
  once, replace applicable `<pending-commit>` placeholders with its short hash,
  publish and audit eligible replies, then restart at Step 2 for the new head.
- If no local changes exist, publish and audit eligible disagreement, deferral,
  or already-pushed fix replies, then return to monitoring.

After publishing any reply, run at least one additional monitor cycle so new
responses are observed before closeout.

### 4. Reconcile monitor actions

Use `monitor-pr.sh` as the only remote polling loop:

- `feedback_changed`: add the feedback to the current repair batch.
- `ci_failed`: add the remote failure evidence to the batch.
- `merge_conflict`: add mechanical resolution to the batch or stop for a
  user-owned decision.
- `timeout`: refresh checks, comments, mergeability, and monitor state. Continue
  monitoring when checks are pending or observation time remains; timeout alone
  is never evidence of readiness.
- `pr_not_open`: investigate stale selection or recoverable state. Stop with
  evidence when the pull request is closed or merged and cannot be acted on.
- `ready`: apply the closeout gate below.

### 5. Closeout gate

Closeout requires all of the following for the current head:

- the monitor returned `ready` after the full minimum observation time
- required remote checks are green
- the full local lane passed for the delivered tree
- no merge conflict or local change remains
- a fresh final fetch found no unledgered feedback or new requested change
- every non-excluded ledger row has an eligible posted reply and a `pass` audit
- at least one monitor cycle ran after the most recently posted reply

When every gate passes, report the pull request URL, head SHA, checks, ledger,
comment outcomes, reply audits, repair commits, and residual risk. Ask the user
to authorize merging the exact pull request, then stop.

### 6. Merge after authorization

Authorization is single-use and applies only to the named pull request at the
approved head. Before merging, re-fetch the head, checks, mergeability, comments,
and ledger. Resume the workflow and request fresh authorization if any gate is
no longer satisfied.

Use the repository's sole allowed merge method or the viewer's explicit default.
If several methods are allowed without a default, ask the user to choose. Merge,
switch to and update the base branch, then delete the local and mapped remote
source branches only after confirming the pull request is merged. Verify both
branches are gone; do not guess the remote for a cross-repository pull request.

## Completion contract

Return either:

- the exact merge-authorization request with all closeout evidence
- the merged pull request and verified branch cleanup
- a concrete blocker with the pull request, head SHA, ledger, and next required
  decision
