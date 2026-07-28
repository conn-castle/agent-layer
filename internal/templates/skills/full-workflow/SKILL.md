---
name: full-workflow
description: >-
  Explicit-only.
  Align a feature specification, produce a reviewed plan, complete the local
  work, and ship the pull request.
---

# full-workflow

Orchestrate the complete development process, from a user request to a shipped
and ready-to-merge PR.

## Inputs

Require:

- the requested work
- `implementer`: self-contained dispatch target for implementation
- `code_reviewer`: self-contained semantic-review dispatch target
- `fixer`: self-contained dispatch target for bounded repairs
- `plan_reviewers`: one or more self-contained dispatch target specifications

Ask for any missing target; do not infer roles or target specifications.

Invoking this workflow authorizes the normal branch, commit, push, and PR writes
needed.

## Delivery boundary

Use the current checkout and default base unless the user requested otherwise.
Preserve unrelated work with explicit diff boundaries and path- or hunk-specific
staging; block only when overlapping work cannot be separated safely.

## Workflow

Dispatch using `/agent-dispatch`.

1. Write `.agent-layer/tmp/full-workflow.<run-id>.spec.md` with objective,
   scope, non-goals, constraints, settled decisions, and any remaining
   user-owned choice. Resolve factual unknowns before continuing.
2. Run `/plan-work` with the spec and `plan_reviewers`. Validate any reported
   user blocker against repository escalation rules.
3. Dispatch `implementer` with `/implement-plan` and the complete reviewed
   artifacts.
4. Concurrently run `/verify-work` in a fresh built-in subagent and dispatch
   `code_reviewer` with `/review-uncommitted-code` against the same delivery
   tree. Validate and deduplicate supported findings.
5. Dispatch `fixer` with valid open findings in one bounded batch.
6. Continue until verification is complete and every in-scope finding is
   resolved or rejected with evidence; ask the user only for a genuine
   substantive decision that cannot be resolved under repository escalation
   rules.
7. Run `/ship-pr`. Return its exact merge-authorization request when
   required, then resume only with the caller's answer.

## Completion

Complete only when the approved contract is satisfied and `/ship-pr` reports a
merged PR with verified cleanup. Return the artifact paths, shipping result, or
a concrete blocker.
