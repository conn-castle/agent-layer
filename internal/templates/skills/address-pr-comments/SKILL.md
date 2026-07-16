---
name: address-pr-comments
description: >-
  Explicit-only.
  Evaluate feedback on an open pull request, implement accepted fixes, prepare
  replies, and return an updated ledger without committing, pushing, or posting.
---

# address-pr-comments

Resolve pull-request comments through one durable ledger.

## Inputs

Accept a PR number/URL, ledger, comment IDs, comments, or priorities; default to
the current branch's PR. Use
`.agent-layer/tmp/ship-pr-comments-<pr-number>.md` when no ledger is supplied.
Never create a second ledger for the same PR. Never stage, commit, push, post,
or call reply APIs; return uncommitted fixes and the ledger.

## Ledger contract

Keep one row per conversation comment, inline comment, or review body using a
stable comment ID or URL:

| Comment | Decision | Notes | Reply | Status |
| --- | --- | --- | --- | --- |

Use decisions `fix`, `disagree`, `defer`, or `excluded`. Keep each row
self-supporting: in `Notes`, briefly record the reason for the decision and any
relevant change, check result, tracker, or exclusion reason. Keep the proposed
reply—or the posted reply URL or ID—in `Reply`. Do not copy full analysis or
command output into the ledger. Use plain status text to show what remains, such
as `fixing`, `ready to reply`, `blocked`, `complete`, or `excluded`. Mark a row
`complete` only after fresh GitHub data confirms its supported reply.

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
unless authorized. Preserve caller-owned reply status unless fresh evidence
supersedes it.

Prepare one reply per non-excluded, unblocked row:

- **Fixed in `<pending-commit>`.** Describe the fix.
- **No change — `<reason>`.** Give evidence.
- **Deferred — tracked in `<location>`.** Explain the boundary.

Finish when every row has a supported disposition and no unblocked local work
remains. Return the ledger, counts, fixes/checks, trackers, pending replies,
blockers, and confirmation that nothing was published.
