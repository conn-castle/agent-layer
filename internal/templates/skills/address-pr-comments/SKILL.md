---
name: address-pr-comments
description: >-
  Evaluate feedback on an open pull request, implement accepted fixes, prepare
  replies, and return an updated ledger without committing, pushing, or posting.
---

# address-pr-comments

Resolve pull-request comments through one durable ledger.

## Inputs

Accept a PR number/URL, ledger, comment IDs, comments, or priorities; default to
the current branch's PR. Create
`.agent-layer/tmp/address-pr-comments-<pr-number>-ledger.md` when needed. Never
stage, commit, push, post, or call reply APIs; return uncommitted fixes and the
ledger.

## Ledger contract

Create one row per conversation comment, inline comment, or review body using
`conversation:<id-or-url>`, `review:<id>`, or `review-body:<id>`:

| Comment key | Location | Decision | State | Reply URL or ID | Reply audit | Notes |
| --- | --- | --- | --- | --- | --- | --- |

Decisions are `fix`, `disagree`, `defer`, or `excluded`. Notes hold evidence,
fix/check results, trackers, and reply drafts. A verdict reply supports its
parent unless it adds a request.

Local states are `fixed_uncommitted`, `ready_to_reply`,
`blocked_user_decision`, or `excluded`; publishing may later set
`reply_posted_pending_audit` or `complete`. Completion requires fresh reply
evidence, a passing audit, and every claimed fix/tracker. `Reply audit` is
`not-run`, `pass`, or a failure reason. Explain exclusions.

## Workflow

Fetch every comment type, merge stable keys, and reconcile replies from fresh
GitHub data. Exclude only status/CI messages, factual statements, and verdicts
without new requests. Stop if all rows are complete.

Validate each remaining comment against the current tree:

- `fix`: correct, beneficial, and in PR scope
- `disagree`: unsupported, harmful, or not beneficial
- `defer`: worthwhile new feature, pre-existing issue, or unrelated refactor

Never disagree to avoid work or defer a defect introduced by the PR. Continue
independent work before escalating under repository rules.

Repair accepted root causes and required tests/docs/memory; group coupled work
and run focused checks. Track deferrals locally, without external issue creation
unless authorized. Preserve caller-owned reply/audit state unless fresh evidence
supersedes it.

Prepare one reply per non-excluded, unblocked row:

- **Fixed in `<pending-commit>`.** Describe the fix.
- **No change — `<reason>`.** Give evidence.
- **Deferred — tracked in `<location>`.** Explain the boundary.

Finish when every row has a supported decision and local terminal state. Return
the ledger, counts, fixes/checks, trackers, pending replies, blockers, and
confirmation that nothing was published.
