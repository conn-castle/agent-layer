---
name: address-pr-comments
description: >-
  Handle reviewer feedback on an open PR: evaluate comments, implement fix
  decisions in the working tree, justify disagreements, track deferrals, prepare
  reply text in a comment ledger, and return the ledger without committing or
  posting replies.
---

# address-pr-comments

## Required inputs

- `plan_review_agents`: one or more dispatch agent roles to pass to
  `/clean-and-fix-code`, `/plan-work`, and `/fully-implement-plan`.

If `plan_review_agents` is missing, ask for it before starting. Do not invent a
default plan review agent list.

## Optional inputs

- a PR number or URL; default is the current branch's open PR
- an optional comment ledger path or existing ledger content
- specific comment IDs to address
- pre-fetched comment data from the caller
- guidance on which comments to prioritize

If no ledger is found for the PR, create and return
`.agent-layer/tmp/address-pr-comments-<pr-number>-ledger.md`.

## Boundaries

- Never run `git add`, `git commit`, `git push`, `gh pr comment`, or GitHub reply APIs.
- Never post prepared replies.
- Never set `GitHub reply posted` to `yes` unless fresh GitHub data shows the reply is already posted.
- Leave implemented fixes as local working-tree changes for the caller.
- Return the updated comment ledger on every non-checkpoint exit.

## Comment Ledger

The ledger is the handoff contract with the caller. It must include one row for
every fetched original PR comment or review body. Agent-authored verdict replies
are evidence for the parent row, not new feedback rows, unless they contain a
new requested change.

Use stable comment keys:
- `review:<comment-id>` for inline review comments
- `conversation:<comment-id-or-url>` for PR conversation comments
- `review-body:<review-id>` for review summary bodies

Maintain this summary table:

| Comment key | Location | Decision | State | GitHub reply posted | Notes |
| --- | --- | --- | --- | --- | --- |

Column values:
- `Decision`: `untriaged`, `fix`, `disagree`, `defer`, or `excluded`.
- `State`: concise state such as `needs_evaluation`, `fixed_uncommitted`, `ready_to_reply`, `complete`, or `blocked_user_decision`.
- `GitHub reply posted`: `yes` or `no`.
- `Notes`: exact reply draft or detail-section link; include tracking, commit, reply URL, escalation report paths, cleanup notes, or narrow check results when relevant.

Use detail sections keyed by comment key for full comment text, surrounding code
excerpts, long reply drafts, cleanup notes, narrow check results, and
caller-provided reply evidence.

Fully addressed means:
- `fix` with a pushed commit hash, `GitHub reply posted` = `yes`, and a posted `Fixed in <hash>` reply.
- `disagree` with `GitHub reply posted` = `yes` and a posted `No change — <reason>` reply.
- `defer` with a real tracking location, `GitHub reply posted` = `yes`, and a posted `Deferred — tracked in <location>` reply.
- `excluded` with an explicit exclusion reason.

## Workflow

### Phase 1: Gather And Merge

1. Read all PR comments:
   - `gh pr view <pr-number> --comments` for conversation comments
   - `gh api repos/{owner}/{repo}/pulls/{pr-number}/comments` for review comments
   - `gh api repos/{owner}/{repo}/pulls/{pr-number}/reviews` for review bodies
2. Merge fetched original comments into the ledger by stable comment key.
3. Mark rows as `excluded` only for bot status messages, CI notifications,
   pure statements of fact, and already-posted verdict replies that contain no
   new requested change.
4. Reconcile posted replies from fresh GitHub data.
5. If every non-excluded feedback row is fully addressed, return immediately.

### Phase 2: Evaluate

For each non-fully-addressed feedback comment, choose:
- `fix`: the suggestion is correct, beneficial, and belongs in this PR.
- `disagree`: the suggestion is wrong, harmful, or not beneficial.
- `defer`: the suggestion has merit but is outside this PR's scope.

Do not disagree merely to avoid work. Defer only when the comment requests a
new feature, identifies a pre-existing issue, or requires a non-trivial refactor
unrelated to this PR. If a comment reports a bug introduced by this PR, fix it.

Ask before continuing when a comment is ambiguous or requires a materially
broader scope or architecture decision.

### Phase 3: Implement And Track

1. Implement `fix` decisions directly when they are small enough to handle
   safely inside this skill.
2. If a required `fix` decision is large, cross-cutting, behavior-changing, or
   complex enough that direct editing would be risky, batch related comments
   into one scoped task and run:

   ```text
   /plan-work
   {scoped task from batched comments}
   plan_review_agents are {agent 1, agent 2, ...}
   ```

   Then run:

   ```text
   /fully-implement-plan
   Plan artifacts:
   {relative path to reviewed plan artifact}
   {relative path to reviewed task artifact}
   {relative path to reviewed context artifact}
   plan_review_agents are {agent 1, agent 2, ...}
   ```

   Resume this workflow after `/fully-implement-plan` returns local
   working-tree changes. Do not use this escalation for ordinary reviewer nits,
   docs-only edits, obvious local fixes, or one-file cleanup.
3. Record `defer` items in `ISSUES.md`, `BACKLOG.md`, or a GitHub issue. Then
   draft the deferred reply.
4. Update each affected ledger row:
   - fix rows: `Decision=fix`, `State=fixed_uncommitted`, `GitHub reply posted=no`
   - disagree rows: `Decision=disagree`, `State=ready_to_reply`, `GitHub reply posted=no`
   - defer rows: `Decision=defer`, `State=ready_to_reply`, `GitHub reply posted=no`

### Phase 4: Clean

If this skill made non-trivial local code changes, run cleanup:

```text
/clean-and-fix-code
plan_review_agents are {agent 1, agent 2, ...}
```

Then draft fixed replies.
Do not run the repo's full check lane here; `ship-pr` owns the final pre-commit
check. Run only narrow, task-local commands when needed to debug or confirm an
implementation step, and record cleanup notes or check results in the ledger.

### Phase 5: Draft Replies

Every prepared reply must start with one bold verdict:
- **Fixed in `<pending-commit>`.** Describe the concrete fix. The caller
  replaces `<pending-commit>` with the actual short hash after committing.
- **No change — `<reason>`.** Give a specific technical justification.
- **Deferred — tracked in `<location>`.** Explain why the tracked item is
  outside this PR's scope.

Draft a specific reply for every non-excluded, non-fully-addressed row unless
the row is blocked on a user decision. If a previously declined suggestion is
subsequently implemented, the draft must acknowledge the reversal.

### Phase 6: Return

Before returning, re-read the ledger and ensure every fetched original comment
has a row. Every non-excluded feedback row must be fully addressed, have a
prepared reply, or be marked `blocked_user_decision`.

The caller owns committing, pushing, replacing `<pending-commit>` placeholders,
posting replies, setting `GitHub reply posted=yes`, and running the fresh-context
reply audit with [`reviewer-prompt.md`](reviewer-prompt.md) after replies exist.

## Final Handoff

Return the ledger path/content and state:
- whether the skill exited immediately because all comments were fully addressed
- counts of fix, disagree, defer, already-replied, and caller-reply-needed rows
- tracking locations for `defer` rows
- `/plan-work` and `/fully-implement-plan` report paths for large-fix escalations
- cleanup result and any narrow checks run, or why none were needed
- confirmation that no stage, commit, push, or GitHub reply operation was performed
