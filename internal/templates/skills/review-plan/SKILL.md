---
name: review-plan
description: >-
  Review and repair a plan/task/context artifact set with required dispatched
  review agents, synthesize suspect feedback, revise accepted issues, and repeat
  until the plan is implementation-ready.
---

# review-plan

Cross-agent pre-implementation plan review and repair. Dispatch review agents
for independent critique; keep judgment, synthesis, artifact revision, and
readiness decisions with the current orchestrator.

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
`/agent-dispatch`, inspect live options, and fail if a requested override is
unsupported.

## Required artifacts

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`:
- `.agent-layer/tmp/review-plan.<run-id>.state.md`
- `.agent-layer/tmp/review-plan.<run-id>.report.md`

Create both before writing. Store review agent prompt/output artifacts under
`.agent-layer/tmp/` with the same prefix and record each path in state.

## Context Discipline

You are the orchestrator for this skill. Do not do work that belongs to
subagents or delegated skills in the orchestration context. Preserve your
context to make strategic decisions, enforce gates, reconcile returned outputs,
and continue this skill's workflow after every delegation returns.

## Rules

- Review against the spec when a spec path is supplied; otherwise review against
  the plan's stated objective and scope.
- Use `assets/agent-review-prompt.md` for dispatched review agent runs. Do not
  ask review agents to edit artifacts.
- Treat review agent output as suspect. Accept a suggestion only when you agree with
  it.
- Classify each finding as `accepted`, `rejected`, `duplicate`, or
  `substantive-user-decision`; record a one-line reason.
- This is not a report-only review: revise the plan/task/context artifacts for
  accepted findings and resolved user decisions.
- Revise artifacts only for accepted findings and resolved user decisions.
- Ask the user before applying a substantive change or tradeoff not settled by
  the spec or plan. Use concrete options with brief pros, cons, and a
  recommendation.
- Iterate until no accepted unresolved findings remain and no substantive user
  decision is pending.
- Dispatch review agents one at a time unless the dispatch implementation is known
  to be safe for concurrent review agent launches in the current repo.

## Workflow

### Phase 1: Preflight

1. Read the plan, task, and context artifacts fully.
2. Read the spec if one was supplied.
3. Confirm the artifacts describe the same objective, scope, and execution
   contract.
4. Normalize dispatch agent roles through live `al dispatch` options.
5. Record artifact paths, dispatch agent roles, normalized dispatch flags, and
   current round in the state file.

### Phase 2: Dispatch review agents

For each review agent dispatch role, dispatch a focused prompt using
`assets/agent-review-prompt.md` and containing:
- plan, task, context, and optional spec paths
- instruction to review only the artifact set
- instruction to report blockers, weak verification, missing scope, sequencing
  gaps, hidden assumptions, and docs/tests/memory gaps
- instruction to avoid rewriting the plan

Capture the useful review agent result or report path in the state file.

### Phase 3: Synthesize

For every review agent finding:
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
3. `## Review Agent Rounds`
4. `## Accepted Changes`
5. `## Rejected Suggestions`
6. `## User Decisions`
7. `## Final Readiness`

`Final Readiness` must be exactly one of:
- `implementation-ready`
- `blocked-for-user-decision`

## Guardrails

- Do not treat review agent consensus as truth or hide rejected suggestions.
- Do not widen implementation scope or weaken verification to satisfy a
  review agent preference.

## Definition of done

- All required artifacts and dispatch agent roles were validated.
- Every review agent dispatch role ran with `assets/agent-review-prompt.md`.
- Every review agent finding was classified with a reason.
- Accepted findings were resolved in the artifacts or escalated through a human
  checkpoint.
- The final report exists and declares `implementation-ready` or
  `blocked-for-user-decision`.

## Final handoff

Echo the report path, the reviewed plan/task/context paths, the review agent
dispatch roles used, accepted changes made, rejected suggestions, and
final readiness.
