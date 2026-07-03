---
name: multi-agent-plan-review
description: >-
  Review a plan/task/context artifact set with required dispatched reviewer
  agents, synthesize suspect feedback, revise accepted issues, and repeat until
  the plan is implementation-ready.
---

# multi-agent-plan-review

Cross-agent pre-implementation plan review. Dispatch reviewers for independent
critique; keep judgment, synthesis, artifact revision, and readiness decisions
with the current orchestrator.

## Required inputs

Fail before side effects unless all are present:
- `review_agents`: one or more dispatch agent roles
- plan artifact path
- task artifact path
- context artifact path

Optional input:
- spec artifact path, used as the review contract when present

Dispatch agent roles may be terse (`codex high`, `claude opus xhigh`,
`antigravity`). Infer the agent only when unambiguous. Before dispatching, follow
`agent-dispatch`, inspect live options, and fail if a requested override is
unsupported.

## Required artifacts

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`:
- `.agent-layer/tmp/multi-agent-plan-review.<run-id>.state.md`
- `.agent-layer/tmp/multi-agent-plan-review.<run-id>.report.md`

Create both before writing. Store reviewer prompt/output artifacts under
`.agent-layer/tmp/` with the same prefix and record each path in state.

## Rules

- Review against the spec when a spec path is supplied; otherwise review against
  the plan's stated objective and scope.
- Use `review-plan` for dispatched reviewer runs. Do not ask reviewers to edit
  artifacts.
- Treat reviewer output as suspect. Accept a suggestion only when you agree with
  its evidence and impact.
- Classify each finding as `accepted`, `rejected`, `duplicate`, or
  `substantive-user-decision`; record a one-line reason.
- Revise artifacts only for accepted findings and resolved user decisions.
- Ask the user before applying a substantive change or tradeoff not settled by
  the spec or plan. Use concrete options with brief pros, cons, and a
  recommendation.
- Iterate until no accepted unresolved findings remain and no substantive user
  decision is pending.
- Dispatch reviewers one at a time unless the dispatch implementation is known
  to be safe for concurrent reviewer launches in the current repo.

## Workflow

### Phase 1: Preflight

1. Read the plan, task, and context artifacts fully.
2. Read the spec if one was supplied.
3. Confirm the artifacts describe the same objective, scope, and execution
   contract.
4. Normalize dispatch agent roles through live `al dispatch` options.
5. Record artifact paths, dispatch agent roles, normalized dispatch flags, and
   current round in the state file.

### Phase 2: Dispatch reviewers

For each reviewer dispatch agent role, dispatch `review-plan` with a focused
prompt containing:
- plan, task, context, and optional spec paths
- instruction to review only the artifact set
- instruction to report blockers, weak verification, missing scope, sequencing
  gaps, hidden assumptions, and docs/tests/memory gaps
- instruction to avoid rewriting the plan

Capture the useful reviewer result or report path in the state file.

### Phase 3: Synthesize

For every reviewer finding:
1. Validate it against the artifacts and repo context.
2. Classify it under `Rules`.
3. Ignore speculative, unsupported, out-of-scope, or merely stylistic findings.
4. Merge duplicates before revising artifacts.

### Phase 4: Revise accepted findings

Keep revisions scoped to implementation readiness:
- clarify scope or non-goals
- fix sequencing
- strengthen verification
- add missing docs/tests/memory steps
- update context paths, entry point, or constraints

Do not convert rejected suggestions into churn.

### Phase 5: Repeat to alignment

After any artifact revision, repeat Phases 2-4.

### Phase 6: Report

Write the final report with these sections:
1. `# Multi-Agent Plan Review Summary`
2. `## Inputs`
3. `## Reviewer Rounds`
4. `## Accepted Changes`
5. `## Rejected Suggestions`
6. `## User Decisions`
7. `## Final Readiness`

`Final Readiness` must be exactly one of:
- `implementation-ready`
- `blocked-for-user-decision`

## Guardrails

- Do not treat reviewer consensus as truth or hide rejected suggestions.
- Do not widen implementation scope or weaken verification to satisfy a
  reviewer preference.

## Definition of done

- All required artifacts and dispatch agent roles were validated.
- Every reviewer dispatch agent role ran through `review-plan`.
- Every reviewer finding was classified with a reason.
- Accepted findings were resolved in the artifacts or escalated through a human
  checkpoint.
- The final report exists and declares `implementation-ready` or
  `blocked-for-user-decision`.

## Final handoff

Echo the report path, the reviewed plan/task/context paths, the reviewer
dispatch agent roles used, accepted changes made, rejected suggestions, and
final readiness.
