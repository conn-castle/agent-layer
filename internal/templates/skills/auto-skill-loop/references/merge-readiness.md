# Merge Readiness

Dispatch `code_reviewer` in a fresh merge-readiness context for the exact open
PR and head. Return `ready` only when all of these are true:

- the PR is mergeable and conflict-free
- continuous integration and required local evidence are green for this head
- every eligible review comment has a reply and no actionable feedback remains
- no simple in-scope issue was deferred
- the batch is coherent, substantive, and high-value

Required external approval leaves the PR open and does not block other work.
Return simple in-scope repair findings to a fresh `implementer`; after the same
`rote_worker` publishes and rechecks them, review the new exact head afresh.
After `ready`, resume the same `rote_worker` with one normal single-use
exact-PR/head authorization derived from the recorded standing authorization.
Any changed head requires a fresh review and authorization.
