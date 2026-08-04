---
name: implement
description: >-
  Implement a large and substantive code change directly or through a planned cross-agent
  workflow, adding independent plan or code review only when warranted.
---

# implement

## Inputs

Require a task, request, or spec, referred to as `<input>`.

Require dispatch targets named `implementer`, `plan_reviewers`, and
`code_reviewer`. The workflow decisions below determine which targets are used;
providing a target does not require or imply its use. Do not infer or substitute missing
targets. Use `/agent-dispatch` for every dispatch.

## Decide Implementation Approach

Decide separately whether to plan, whether planned work needs independent plan
review, and whether the implementation needs independent code review. Choosing
one does not imply choosing another.

Planning and each independent review add significant cost. Account for that
cost instead of choosing the lowest-risk workflow by default.

If you are confident you can proceed and no substantive requirements or
tradeoffs remain unresolved, implement directly. Code review can be selected
for either direct or planned implementation.

## Implementation

If a plan is not required, implement `<input>` directly without dispatching or
writing a plan.

If a plan is required, do the following:

- Write a self-contained plan under `.agent-layer/tmp` without dispatching.
- If independent plan review is required, write each reviewer a self-contained
  prompt that asks it to review the plan against `<input>` without editing files
  or implementing the plan. Dispatch all `plan_reviewers` concurrently, then
  update the plan to address any findings you agree with. Do not repeat plan
  review.
- Dispatch `implementer` to implement the plan.

## Finish

1. Inspect and simplify the completed implementation:
   - Remove unnecessary complexity, duplication, dead code, and premature
     abstractions without changing required behavior.
   - Keep code readable and easy to reason about. Prefer direct code over new
     indirection.
   - Remove new or modified tests that cannot detect a concrete product defect,
     encode implementation details, or are tautological. Keep mocks minimal.
   - Do not broaden the task into cleanup of unrelated existing code.
2. If independent code review is required, dispatch `code_reviewer` to review
   the implementation without editing it. Address any findings you agree with.
3. Confirm the final implementation satisfies `<input>` and, for planned work,
   every plan requirement. If a dispatched implementer returns with unresolved
   requirements, you may use `/agent-dispatch` once to continue that same
   conversation. Include the specific unresolved requirements and accepted
   review findings in the continuation prompt. Resolve any gaps and run
   required checks before reporting completion.
