---
name: address-pr-comments
description: >-
  Evaluate feedback on an open pull request, directly implement accepted fixes,
  prepare replies, and return an updated comment ledger without committing,
  pushing, or posting.
---

# address-pr-comments

Address each pull request comment once. Keep all local decisions, edits,
evidence, and reply drafts in one durable ledger for the caller.

## Inputs

Optional inputs:

- pull request number or URL; default to the current branch's open pull request
- existing comment ledger path or content
- specific comment IDs
- caller-provided comment data
- prioritization guidance

When no ledger exists, create
`.agent-layer/tmp/address-pr-comments-<pr-number>-ledger.md`.

## Boundaries

- Do not stage, commit, push, post comments, call GitHub reply APIs, or mark a
  reply posted without fresh GitHub evidence.
- Leave fixes as local working-tree changes.
- Return the ledger on every non-checkpoint exit.

## Ledger contract

Include one row for every original conversation comment, inline review comment,
or review body. Use stable keys:

- `review:<comment-id>`
- `conversation:<comment-id-or-url>`
- `review-body:<review-id>`

Maintain:

| Comment key | Location | Decision | State | Reply URL or ID | Reply audit | Notes |
| --- | --- | --- | --- | --- | --- | --- |

Use `fix`, `disagree`, `defer`, or `excluded` for decided rows. Notes must hold
the reply draft or link to detail containing the evidence, local fix, focused
checks, or tracking location. Agent verdict replies are evidence for their
parent row unless they contain a new requested change.

`address-pr-comments` may set `fixed_uncommitted`, `ready_to_reply`,
`blocked_user_decision`, or `excluded`. The publishing caller may subsequently
set `reply_posted_pending_audit` or `complete`. Use `not-run`, `pass`, or the
auditor's failure verdict in `Reply audit`.

Treat a non-excluded row as fully addressed only when fresh GitHub data supplies
its reply URL or ID, the reply audit is `pass`, and any claimed fix commit or
deferral tracker exists. An excluded row requires an explicit exclusion reason.

## Workflow

### 1. Gather and reconcile

Fetch conversation comments, inline review comments, and review bodies. Merge
them by stable key and reconcile already-posted replies from fresh GitHub data.
Exclude only status messages, continuous-integration notifications, pure factual
statements, and posted verdict replies with no new request.

If every non-excluded row is already fully addressed, return the reconciled
ledger without editing code.

### 2. Decide each comment

Choose:

- `fix`: correct, beneficial, and in scope for this pull request
- `disagree`: unsupported, harmful, or not beneficial
- `defer`: worthwhile but a new feature, pre-existing issue, or unrelated
  refactor outside this pull request

Validate comments against the current tree and repository evidence. Do not
disagree to avoid work or defer a defect introduced by this pull request.

Ask the user only when available evidence leaves multiple viable choices with
materially different behavior, architecture, scope, risk, or cost.

### 3. Implement accepted fixes directly

Address every `fix` decision directly, grouping tightly coupled comments when
useful. Diagnose the root cause, make required code, test, documentation, and
memory edits, and run the narrowest credible affected checks. Do not route large
or cross-cutting fixes through `/plan-work`, `/implement-plan`,
`/fully-implement-plan`, or `/verify-work`; complexity alone is not a user
decision.

For `defer`, record an appropriate local tracking location. If external issue
creation is required, prepare the tracking text but do not create it without
authorization.

Update each row to `fixed_uncommitted`, `ready_to_reply`, or
`blocked_user_decision` as applicable. Preserve caller-owned reply URLs, audit
verdicts, and completed state when reconciling an existing ledger unless fresh
GitHub evidence invalidates them.

### 4. Draft replies and return

Prepare one specific reply for every non-excluded row not blocked on a user
decision:

- **Fixed in `<pending-commit>`.** Describe the fix.
- **No change — `<reason>`.** Give the evidence-backed justification.
- **Deferred — tracked in `<location>`.** Explain the scope boundary.

Re-read the ledger and ensure every fetched original comment has a row and a
decision, reply draft, or user-decision blocker.

## Completion contract

Return the ledger path or content, decision counts, direct fixes and focused
checks, deferred tracking locations, caller replies still needed, and any
user-owned blocker. Confirm that no staging, commit, push, or GitHub reply
operation occurred.
