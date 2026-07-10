---
name: review-plan
description: >-
  Review and repair a plan/task/context artifact set with required dispatched
  plan reviewers, synthesize suspect feedback, revise accepted issues, and repeat
  until the plan is implementation-ready.
---

# review-plan

Cross-agent pre-implementation plan review and repair. Dispatch plan reviewers
for independent critique; keep judgment, synthesis, artifact revision, and
readiness decisions with the current orchestrator.

## Required inputs

Fail before side effects unless all are present:
- `plan_reviewers`: one or more dispatch agent roles
- plan artifact path
- task artifact path
- context artifact path

Optional input:
- spec artifact path, used as the review contract when present

Dispatch agent roles may be terse (`codex high`, `claude opus xhigh`,
`antigravity`). Infer the agent only when unambiguous. Before dispatching,
inspect live `al dispatch options` output and fail if a requested override is
unsupported.

## Required artifacts

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`:
- `.agent-layer/tmp/review-plan.<run-id>.state.md`
- `.agent-layer/tmp/review-plan.<run-id>.report.md`

Create both before writing. Store plan reviewer prompt/output artifacts under
`.agent-layer/tmp/` with the same prefix and record each path in state. For
each dispatched reviewer, assign a unique child report path:
`.agent-layer/tmp/review-plan.<run-id>.<role-slug>.report.md`.

## Context preservation

You are the orchestrator for this skill. Do not do work that belongs to
subagents or delegated skills in the orchestration context. Preserve your
context to make strategic decisions, enforce gates, reconcile returned outputs,
and continue this skill's workflow after every delegation returns.

## Compaction guidance

When compaction is needed, retain this entire skill verbatim. Also preserve the
current workflow step or phase, active artifact paths, selected scope, pending
gate verdicts, delegated skills and subagents already run and their outcomes,
unresolved blockers or user checkpoints, and the next exact step.

## Rules

- Review against the spec when a spec path is supplied; otherwise review against
  the plan's stated objective and scope.
- Use `assets/agent-review-prompt.md` for dispatched plan reviewer runs. Do not
  ask plan reviewers to edit artifacts.
- Treat plan reviewer output as suspect. Accept a suggestion only when you agree with
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
- Dispatch plan reviewers one at a time unless the dispatch implementation is known
  to be safe for concurrent plan reviewer launches in the current repo.

## Workflow

### Phase 1: Preflight

1. Read the plan, task, and context artifacts fully.
2. Read the spec if one was supplied.
3. Confirm the artifacts describe the same objective, scope, and execution
   contract.
4. Normalize dispatch agent roles through live `al dispatch` options.
5. Record artifact paths, dispatch agent roles, normalized dispatch flags, and
   current round in the state file.

### Phase 2: Dispatch Plan Reviewers

For each plan reviewer dispatch role, dispatch a focused prompt using
`assets/agent-review-prompt.md` and containing:
- plan, task, context, and optional spec paths
- the unique child report path assigned for that reviewer
- instruction to review only the artifact set
- instruction to report blockers, weak verification, missing scope, sequencing
  gaps, hidden assumptions, and docs/tests/memory gaps
- instruction to avoid rewriting the plan
- instruction not to write or modify the parent
  `.agent-layer/tmp/review-plan.<run-id>.report.md`

Capture the useful plan reviewer result or report path in the state file.

### Phase 3: Synthesize

For every plan reviewer finding:
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

- Do not treat plan reviewer consensus as truth or hide rejected suggestions.
- Do not widen implementation scope or weaken verification to satisfy a
  plan reviewer preference.

## Definition of done

- All required artifacts and dispatch agent roles were validated.
- Every plan reviewer dispatch role ran with `assets/agent-review-prompt.md`.
- Every plan reviewer finding was classified with a reason.
- Accepted findings were resolved in the artifacts or escalated through a human
  checkpoint.
- The final report exists and declares `implementation-ready` or
  `blocked-for-user-decision`.

## Final handoff

Echo the report path, the reviewed plan/task/context paths, the plan reviewer
dispatch roles used, accepted changes made, rejected suggestions, and
final readiness.
