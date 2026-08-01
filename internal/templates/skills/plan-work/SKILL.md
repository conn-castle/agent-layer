---
name: plan-work
description: >-
  Explicit-only.
  Produce and review an implementation-ready plan from a task source.
---

# plan-work

## Inputs

Require a task source or user request and one or more self-contained
`plan_reviewers` target specifications. Accept an optional dedicated
specification document. Resolve each reviewer through `/agent-dispatch`'s live
metadata; use an unambiguous match without confirmation.

## Workflow

1. Write the plan and context artifacts using
   `YYYYMMDD-HHMMSS-<short-rand>` as the run ID:

   - `.agent-layer/tmp/write-plan.<run-id>.plan.md`
   - `.agent-layer/tmp/write-plan.<run-id>.context.md`

   The context artifact should contain key file paths, current state, and useful
   implementation information not appropriate for the plan.

2. Update the plan as needed. Ensure the artifacts have no gaps, are
   unambiguous, leave no substantive decisions unresolved, and contain
   everything a competent junior developer needs to complete the
   implementation. Ask the user only when a substantive choice cannot be
   resolved under repository escalation rules.

3. Dispatch to each supplied reviewer concurrently using
   `references/review-prompt.md`. Provide the plan artifact paths and optional
   source or specification. Each reviewer returns findings and a review verdict.

4. Address accepted findings by updating the plan artifacts.

5. Return the finalized plan and context paths with one status:

   - `implementation-ready`: accepted findings are addressed
   - `blocked-for-user-decision`: name the unresolved substantive decision
