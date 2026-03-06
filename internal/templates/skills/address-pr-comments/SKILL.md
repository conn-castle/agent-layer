---
name: address-pr-comments
description: >-
  Review all PR comments, implement fixes for agreed feedback, reply to every
  comment with what was done or justification for not addressing it, audit
  changes, and commit and push.
---

# address-pr-comments

This is the PR comment resolution skill.
It should:
- read all PR comments (review comments and conversation comments)
- evaluate each piece of feedback
- implement fixes for agreed feedback
- prepare justifications for disagreed feedback
- audit all changes before committing
- commit and push
- reply to every feedback comment

## Defaults

- Default PR is the current branch's open PR.
- Default scope is all unresolved feedback comments on the PR.
- Comments that are bot status messages, CI notifications, or statements of fact (not feedback) are excluded from processing.

## Inputs

Accept any combination of:
- a PR number or URL
- specific comment IDs to address
- pre-fetched comment data from the caller
- guidance on which comments to prioritize

## Required behavior

Use subagents liberally when available.

Delegate to:
- `audit-and-fix-uncommitted-changes` before committing

## Global constraints

- Every feedback comment must receive a reply. No exceptions.
- Only decline to implement a comment's suggestion when you genuinely believe it is wrong, harms the codebase, or is not actually beneficial or correct. Do not defer work as a reason to not implement.
- When agreeing with a comment, implement the fix and reply describing what was done.
- When disagreeing with a comment, reply with a clear, respectful justification explaining why the suggestion was not implemented.
- Do not batch-dismiss comments with generic responses. Each reply must be specific to the comment.

## Human checkpoints

- Required: ask when a comment requests a change that would require a materially broader scope or architectural decision.
- Required: ask when a comment's intent is ambiguous and could be interpreted multiple ways.
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

Evaluation rules:
- Do not disagree merely to avoid work.
- Do not disagree merely to defer the issue.
- Genuinely consider whether the suggestion improves correctness, readability, performance, or maintainability.
- If the comment points out a real issue but suggests the wrong fix, agree with the issue and implement a better fix.

### Phase 3: Implement agreed changes (Fixer)

1. Implement fixes for all agreed comments.
2. Keep changes focused on what each comment requested.
3. If multiple comments request related changes, group them logically.
4. If a comment's suggestion conflicts with another comment, note the conflict and ask if needed.

### Phase 4: Audit and commit (Auditor + Committer)

1. Use the `audit-and-fix-uncommitted-changes` skill to review and stabilize all changes.
2. Stage all changes: `git add -A`
3. Craft a commit message summarizing the comment-driven changes.
4. Commit and push.

### Phase 5: Reply to every comment (Replier)

1. For each agreed comment:
   - Reply describing the specific fix that was implemented.
   - Reference the relevant code change if helpful.
2. For each disagreed comment:
   - Reply with a clear justification explaining why the suggestion was not implemented.
   - Be respectful and specific. Explain the reasoning, not just the conclusion.
3. Use the GitHub CLI or API to post replies:
   - For review comments: `gh api repos/{owner}/{repo}/pulls/{pr-number}/comments/{comment-id}/replies -f body="<reply>"`
   - For conversation comments: `gh pr comment <pr-number> --body "<reply>"`

## Guardrails

- Do not leave any feedback comment without a reply.
- Do not use generic batch responses like "addressed all comments."
- Do not disagree with a comment just to avoid work or defer the issue.
- Do not implement changes that conflict with the project's established patterns without justification.
- Do not skip the audit-and-fix step before committing.
- Do not reply to comments before the changes are committed and pushed.

## Final handoff

After all comments are addressed:
1. State how many comments were agreed with and fixed.
2. State how many comments were disagreed with and why (briefly).
3. Confirm all comments have replies.
4. State whether changes were pushed.
