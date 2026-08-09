---
name: auto-skill-loop
description: >-
  Run an autonomous loop intended to result in merged PRs.
disable-model-invocation: true
---

# auto-skill-loop

## Inputs

Required:

- a `mode` matching `references/modes/<mode>.md`
- `merge_authorization`: standing authorization to merge under the gate below
- `operator`, `planner`, one or more `plan_reviewers`, `implementer`, `code_reviewer`,
  `pr_worker`, and `rote_worker` dispatch targets

Optional:

- `loop_context`: additional context for the orchestrator only
- `planner_context`: additional context included only in step 1
- `ship_pr_context`: additional context included only in step 2
- `operator_context`: additional context included only in `operator` dispatches

Every input must be explicitly named in the skill invocation. Do not infer an
unnamed input from unstructured text or from another input.

## Rules

- Use `/dispatch-agent` for every dispatch.
- Act as the orchestrator. Delegate all work.
- Build each dispatch prompt only from its specified prompt template.
- When compacting, retain the original user inputs and this skill verbatim in
  addition to what you would normally retain.

## Acting on the User's Behalf

This loop must run without human intervention. Each iteration is intended to result
in a merged PR. For each loop that requires human input, dispatch `operator` in
a fresh session. Use `dispatch_continue` for multiple invocations within a
single loop. The first prompt should include the complete contents of
`references/human-guidance.md`, followed by `operator_context` if provided, then
the item requiring human input with all provided details verbatim.

If the `operator` determines that real human intervention is required, save the
work to an appropriate remote branch for future handling, then check out the
primary branch. Continue with another loop iteration. Do not block the loop
waiting for human input.

## Loop

1. Dispatch `planner` with skill `implement`. Use the following as its prompt:

```text
<complete contents of references/modes/<mode>.md>
<planner_context, if provided>

implementer: <implementer>
plan_reviewers: <plan_reviewers>
code_reviewer: <code_reviewer>

Return a self-contained `<implementation_input>` that states the actual task,
request, or spec you implemented and includes paths to plan artifacts if used.
This context will preserve the intended scope during later review.
```

If `planner` is unable to find work to complete, repeat the dispatch one more
time. If two invocations in a row cannot find work, exit and inform the user.

2. Dispatch `rote_worker` with skill `ship-pr` and this prompt:

```text
pr_worker: <pr_worker>
<ship_pr_context, if provided>

Use the following context to preserve intended scope and behavior throughout the PR
workflow:

<implementation_input>
```

Continue when it returns a merge-authorization request.

3. Dispatch `operator` in a new session. Use the complete contents of
`references/merge-authorization.md` as its prompt, then append:

```text
<operator_context, if provided>
request: <rote_worker merge-authorization request>
```

4. If authorized, continue the dispatch session from step 2 with the exact
authorization. If not, continue the dispatch to preserve the PR and return to
the primary branch without merging. Then return to step 1.
