---
name: address-pr-comments
description: >-
  Review comments on an open pull request, implement agreed fixes, reply to
  every comment, audit changes, then commit and push. Use when the user asks to
  handle PR review comments or reviewer feedback. Use `fix-ci` for failing
  checks and `ship-pr` to create or ship a PR.
---

# address-pr-comments

This is the PR comment resolution skill.
It should:
- read all PR comments (review comments and conversation comments)
- evaluate each piece of feedback
- implement fixes for agreed feedback
- prepare justifications for disagreed feedback
- track legitimate deferrals before replying
- audit all changes before committing
- commit and push
- reply to every feedback comment using the required verdict format

## Defaults

- Default PR is the current branch's open PR.
- Default scope is all unresolved feedback comments on the PR.
- Pure bot status messages, CI notifications, and statements of fact (not feedback) are excluded from processing.
- Automated review comments from tools such as Copilot or CodeRabbit are treated as feedback and require replies, not excluded as bot messages.

## Inputs

Accept any combination of:
- a PR number or URL
- specific comment IDs to address
- pre-fetched comment data from the caller
- guidance on which comments to prioritize

## Required behavior

Delegate to:
- `audit-and-fix-uncommitted-changes` before committing

## Global constraints

- Every feedback comment must receive a reply. No exceptions.
- Only decline to implement a comment's suggestion when you genuinely believe it is wrong, harms the codebase, or is not actually beneficial or correct. Do not defer work as a reason to not implement.
- Only defer when the comment requests a new feature outside the PR scope, identifies a pre-existing issue not introduced by this PR, or requires a non-trivial refactor unrelated to the PR's purpose. The deferred item must be tracked before replying.
- When agreeing with a comment, implement the fix and reply describing what was done.
- When disagreeing with a comment, reply with a clear, respectful justification explaining why the suggestion was not implemented.
- Do not batch-dismiss comments with generic responses. Each reply must be specific to the comment.

## Human checkpoints

- Required: ask when a comment requests a change that would require a materially broader scope or architectural decision.
- Required: ask when a comment's intent is ambiguous and could be interpreted multiple ways.
- When a checkpoint involves a genuine tradeoff between substantive alternatives, present at least two options with brief pros and cons, state which you recommend and why, and let the human decide.
- Stay autonomous when the feedback is clear and the fix or justification is straightforward.

## Comment resolution workflow

### Phase 1: Gather comments (Comment reader)

1. Read all PR comments using:
   - `gh pr view <pr-number> --comments` for conversation comments
   - `gh api repos/{owner}/{repo}/pulls/{pr-number}/comments` for review (inline) comments
   - `gh api repos/{owner}/{repo}/pulls/{pr-number}/reviews` for review bodies
2. Filter out:
   - bot-generated status messages and CI notifications
   - pure statements of fact that are not requesting or suggesting a change
   - comments that have already been resolved with a reply in a previous run of this skill
3. List each feedback comment with its ID, author, location (if inline), and content.

### Phase 2: Evaluate each comment (Evaluator)

For each feedback comment, decide:
- **Agree**: the suggestion is correct, beneficial, and should be implemented.
- **Disagree**: the suggestion is wrong, would harm the codebase, or is not actually beneficial or correct.
- **Defer**: the suggestion has merit but is legitimately outside this PR's scope and is tracked before replying.

Evaluation rules:
- Do not disagree merely to avoid work.
- Do not disagree merely to defer the issue.
- Do not defer a bug or correctness issue introduced by this PR.
- Genuinely consider whether the suggestion improves correctness, readability, performance, or maintainability.
- If the comment points out a real issue but suggests the wrong fix, agree with the issue and implement a better fix.

### Phase 3: Implement agreed changes (Fixer)

1. Implement fixes for all agreed comments.
2. Keep changes focused on what each comment requested.
3. If multiple comments request related changes, group them logically.
4. If a comment's suggestion conflicts with another comment, note the conflict and ask if needed.
5. Record every deferred item in the appropriate tracker (`ISSUES.md`, `BACKLOG.md`, or a GitHub issue) before replying with a deferred verdict.

### Phase 4: Audit and commit (Auditor + Committer)

1. Use the `audit-and-fix-uncommitted-changes` skill to review and stabilize all changes.
2. Stage all changes: `git add -A`
3. Craft a commit message summarizing the comment-driven changes.
4. Commit and push.

### Phase 5: Reply to every comment (Replier)

1. For each agreed comment:
   - Reply with **Fixed in `<short-hash>`.** and describe the concrete fix.
2. For each disagreed comment:
   - Reply with **No change — `<reason>`.** and a clear technical justification explaining why the suggestion was not implemented.
3. For each deferred comment:
   - Reply with **Deferred — tracked in `<location>`.** and a concise explanation of why the item is outside this PR's scope.
4. If a previously declined suggestion is subsequently implemented, the follow-up reply must acknowledge the reversal and describe the concrete change.
5. Use the GitHub CLI or API to post replies:
   - For review comments: `gh api repos/{owner}/{repo}/pulls/{pr-number}/comments/{comment-id}/replies -f body="<reply>"`
   - For conversation comments: `gh pr comment <pr-number> --body "<reply>"`

### Phase 6: Audit reply coverage (Comment auditor)

1. Re-fetch all PR comments, review comments, and review bodies.
2. For every feedback comment, verify:
   - a reply exists from this agent
   - the reply opens with one of the required bold verdicts
   - **Fixed** replies name a commit that contains a relevant change
   - **No change** replies contain a specific, technically grounded justification
   - **Deferred** replies name a real tracked location and the deferral is legitimate
3. Re-address any flagged comment before finishing:
   - missing reply
   - missing verdict
   - hollow fix
   - unjustified decline
   - lazy deferral
   - generic dismissal

## Comment reply format

Every reply to a review comment must open with a **bold verdict** on one line,
followed by a concise justification. There are exactly three verdicts:

1. **Fixed in `<short-hash>`.** — The suggestion was implemented. Describe the
   concrete change.
2. **No change — `<reason>`.** — The suggestion was evaluated and declined.
   `<reason>` is a short label: `by design`, `pre-existing behavior`,
   `not a regression`, `testability`, etc. Follow with the technical
   justification.
3. **Deferred — tracked in `<location>`.** — The suggestion has merit but is
   out of scope. `<location>` names where it was recorded (e.g.,
   `ISSUES.md`, `BACKLOG.md`, a GitHub issue link). The suggestion must
   actually be recorded there before using this verdict.

Do not use "deferred" as a way to avoid doing work that belongs in this PR.
A comment is only legitimately deferred when:
- It requests a new feature or enhancement beyond the PR's scope.
- It identifies a pre-existing issue not introduced by this PR.
- Fixing it would require a non-trivial refactor unrelated to the PR's purpose.

If the suggestion points to a bug or correctness issue introduced by this PR,
it must be fixed, not deferred.

## Guardrails

- Do not leave any feedback comment without a reply.
- Do not use generic batch responses like "addressed all comments."
- Do not disagree with a comment just to avoid work or defer the issue.
- Do not defer a comment without first recording the item in the named location.
- Do not implement changes that conflict with the project's established patterns without justification.
- Do not skip the audit-and-fix step before committing.
- Do not reply to comments before the changes are committed and pushed.
- If a previously declined suggestion is subsequently implemented, the follow-up reply must acknowledge the reversal.

## Definition of done

- Every non-excluded feedback comment on the PR has a reply posted by this agent, with no generic batch responses.
- Every feedback reply opens with one of the three bold verdicts (`Fixed in <hash>`, `No change — <reason>`, `Deferred — tracked in <location>`).
- For every agreed comment, the implemented change is present in a pushed commit and the reply names that commit.
- For every disagreed comment, the reply contains a specific technical justification.
- For every deferred comment, the named tracking location exists and the deferral is legitimate.
- The Phase 6 reply audit completed with no missing reply, missing verdict, hollow fix, unjustified decline, lazy deferral, or generic dismissal.
- The `audit-and-fix-uncommitted-changes` skill ran before the final commit, and all resulting changes are pushed to the PR branch.

## Final handoff

After all comments are addressed:
1. State how many comments were agreed with and fixed.
2. State how many comments were disagreed with and why (briefly).
3. State how many comments were deferred and where they were tracked.
4. Confirm all comments have replies and passed the reply audit.
5. State whether changes were pushed.
