---
name: implement
description: >-
  Implement a large, substantive code change directly or through a planned
  cross-agent workflow, adding plan or code review only when warranted.
---

# implement

## Inputs

Require a task, request, or spec as `<input>` and dispatch targets named
`implementer`, `plan_reviewers`, and `code_reviewer`. Providing a target does
not imply its use. Do not infer missing targets. Use `/agent-dispatch` for every
dispatch.

## Decide Implementation Approach

Inspect the relevant architecture. Reuse existing code and established patterns
when they fit `<input>`. Resolve substantive requirements and tradeoffs before
implementation.

Planning and independent review add significant cost. Before implementation,
answer each question yes or no:

- Is a written plan required to confidently complete and verify `<input>`?
- If a plan is required, does its material risk or complexity justify
  independent plan review?
- Does the implementation's material risk or complexity justify independent
  code review, whether implementation is direct or planned?

If no plan is required, implement `<input>` directly without dispatching or
writing a plan. Run only targeted checks as needed. Check completion against
`<input>`, resolve gaps until implementation is complete. Then continue to
the Finish section.

## Implementation with a Written Plan

If a plan is required, write the following self-contained artifacts. Set
`<run-id>` to `YYYYMMDD-HHMMSS-<short-rand>`:

- `.agent-layer/tmp/write-plan.<run-id>.plan.md` - the implementation plan
- `.agent-layer/tmp/write-plan.<run-id>.task.md` - a checklist for all
  requirements, used to assess completion
- `.agent-layer/tmp/write-plan.<run-id>.context.md` - all background and context
  required for executing the implementation plan

If independent plan review is required, write each reviewer a self-contained
prompt instructing it to review the plan artifacts against `<input>` without
editing files or implementing. Dispatch all `plan_reviewers` concurrently. Then
update the artifacts to address any findings you agree with. Do not repeat plan
review.

Finally, dispatch `implementer` with a prompt instructing it to implement the
plan from the artifacts, run only targeted checks as needed, and return after
completing the plan or when blocked. A blocked response must list the unmet
requirements and current implementation state. The prompt must also instruct it
not to invoke the `implement` skill or dispatch another implementer.

Check completion against the plan artifacts. If the implementation is
incomplete, call `dispatch_continue` once with the `implementer` session handle
and a prompt listing the unmet requirements and instructing it to complete the
plan before returning. If implementation remains incomplete after that
continuation, use a fresh dispatch whose prompt includes the original plan,
current implementation state, and remaining work. Repeat the instruction not
to invoke the `implement` skill or dispatch another implementer in the fresh
prompt. If the continuation did not make meaningful progress, detail a
different approach in that prompt.

## Finish

1. Inspect and simplify the implementation without changing required behavior
   or broadening the task. Remove unnecessary complexity, duplication, dead
   code, premature abstractions, and tests that cannot detect a product defect.

2. If independent code review is required, dispatch `code_reviewer` with a
   self-contained prompt that includes `<input>` and any plan artifacts and
   instructs it to review the implementation against them without editing
   files and return a list of findings. Consider the findings, then address
   those you agree with. Code review must cover the final implementation,
   including changes made in response to review findings. After addressing any
   accepted finding that changes the implementation, dispatch a fresh review of
   the final implementation: use a full review for substantial changes and a
   targeted review for smaller fixes. Repeat until no accepted finding changes
   the implementation.

3. Repeat the completion check. Resolve gaps until it passes. Passing tests
   are supporting evidence, not proof of completion.

4. Run the repository's complete required verification suite against the final
   candidate. Fix any failures, then run the minimal checks required to confirm
   the fixes.
