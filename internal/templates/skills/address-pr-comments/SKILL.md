---
name: address-pr-comments
description: >-
  Evaluate feedback on an open pull request, implement accepted fixes, prepare
  replies, and return an updated ledger without committing, pushing, or posting.
---

# address-pr-comments

Resolve every PR comment and keep decisions, edits, evidence, and reply drafts
in one durable ledger.

## Inputs and boundaries

Accept an optional PR number/URL, existing ledger, comment IDs, caller-provided
comments, or prioritization guidance. Otherwise use the current branch's PR.
Create `.agent-layer/tmp/address-pr-comments-<pr-number>-ledger.md` when needed.

Do not stage, commit, push, post replies, call reply APIs, or mark a reply posted
without fresh GitHub evidence. Leave fixes uncommitted and return the ledger on
every exit.

## Ledger

Create one row per conversation comment, inline comment, or review body using
`conversation:<id-or-url>`, `review:<id>`, or `review-body:<id>`:

| Comment key | Location | Decision | State | Reply URL or ID | Reply audit | Notes |
| --- | --- | --- | --- | --- | --- | --- |

Decisions are `fix`, `disagree`, `defer`, or `excluded`. Notes hold the draft
reply or evidence, fix, checks, or tracking location. Agent verdict replies are
evidence for their parent unless they request new work.

This skill may set `fixed_uncommitted`, `ready_to_reply`,
`blocked_user_decision`, or `excluded`; the publisher may set
`reply_posted_pending_audit` or `complete`. Use `not-run`, `pass`, or the audit
failure in `Reply audit`. A non-excluded row is complete only when fresh GitHub
data supplies its reply ID/URL, audit is `pass`, and any claimed fix or tracker
exists. Exclusions require a reason.

## Workflow

### 1. Gather and decide

Fetch all comment types, merge by stable key, and reconcile posted replies from
fresh GitHub data. Exclude only status/CI messages, pure factual statements, and
verdict replies with no new request. Return immediately if all rows are complete.

Validate each comment against the current tree and choose:

- `fix`: correct, beneficial, and in PR scope
- `disagree`: unsupported, harmful, or not beneficial
- `defer`: worthwhile new feature, pre-existing issue, or unrelated refactor

Do not disagree to avoid work or defer a defect introduced by this PR. Ask only
when evidence leaves a material behavior, architecture, scope, risk, or cost
choice.

### 2. Implement fixes

Repair every `fix` root cause, grouping tightly coupled comments. Make directly
required code, test, docs, and memory edits and run focused checks. Do not route
complexity through planning or verification skills; complexity alone is not a
user decision.

For `defer`, record a local tracker. Prepare but do not externally create an
issue without authorization. Preserve caller-owned reply and audit state unless
fresh GitHub evidence invalidates it.

### 3. Draft replies and return

Prepare one reply per non-excluded, unblocked row:

- **Fixed in `<pending-commit>`.** Describe the fix.
- **No change — `<reason>`.** Give evidence.
- **Deferred — tracked in `<location>`.** Explain the boundary.

Return the ledger, decision counts, fixes and checks, trackers, replies needed,
and any genuine user blocker. Confirm no publishing operation occurred.
