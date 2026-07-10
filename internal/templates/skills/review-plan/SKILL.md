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
`antigravity`).

## Required artifacts

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`:
- `.agent-layer/tmp/review-plan.<run-id>.state.md`
- `.agent-layer/tmp/review-plan.<run-id>.report.md`

Create both before writing. Store plan reviewer prompt/output artifacts under
`.agent-layer/tmp/` with the same prefix and record each path in state. For
each dispatched reviewer, assign a unique child report path:
`.agent-layer/tmp/review-plan.<run-id>.round-<n>.<reviewer-index>-<role-slug>.report.md`.

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

- Dispatch external roles through `/agent-dispatch`.
- Review against the spec when a spec path is supplied; otherwise review against
  the plan's stated objective and scope.
- Treat plan reviewer output as suspect. Accept a suggestion only when you agree with
  it.
- Classify each finding as `accepted`, `rejected`, `duplicate`, or
  `substantive-user-decision`; record a one-line reason.
- Revise artifacts only for accepted findings and resolved user decisions.
- Ask the user before applying a substantive change or tradeoff not settled by
  the spec or plan. Use concrete options with brief pros, cons, and a
  recommendation.

## Workflow

### Phase 1: Preflight

1. Read the plan, task, and context artifacts fully.
2. Read the spec if one was supplied.
3. Confirm the artifacts describe the same objective, scope, and execution
   contract.
4. Record artifact paths, requested dispatch agent roles, and current round in
   the state file.

### Phase 2: Dispatch Plan Reviewers

Prepare every reviewer prompt with `assets/agent-review-prompt.md`. Supply the
artifact paths and that reviewer's unique child report path. For Round 1, supply
the full artifact set. For later rounds, also supply the artifact delta, prior
finding ledger, and exact changed clauses.

Start every reviewer dispatch in the round before waiting for any of them. Once
all have terminated, validate each child report, treat it as immutable, and
record its path, terminal result, and target/model/effort selection in state.

### Phase 3: Synthesize

For every plan reviewer finding:
1. Validate it against the artifacts and repo context.
2. Classify it under `Rules`.
3. Ignore speculative, unsupported, out-of-scope, or merely stylistic findings.
4. Merge duplicates before revising artifacts.
5. Assign one aggregate round impact:
   - `High`: the round exposes a blocker or a major correctness, scope,
     sequencing, verification, or unresolved user-decision gap.
   - `Medium`: the round exposes material implementation-readiness gaps but no
     High-impact gap.
   - `Low`: the round has no accepted material readiness gap; findings are
     minor alignment improvements or there are no accepted findings.
6. Record the rating and its reason under `## Review Agent Rounds`.

### Phase 4: Revise accepted findings

Keep revisions scoped to implementation readiness:
- clarify scope or non-goals
- fix sequencing
- strengthen verification
- add missing docs/tests/memory steps
- update context paths, entry point, or constraints

Do not convert rejected suggestions into churn.

### Phase 5: Repeat to alignment

Review rounds are sequential. Before revising, retain the prior artifact content
or an equivalent exact delta. Repeat Phases 2-4 only after a `High` or `Medium`
round. A `Low` round means there are no accepted material
implementation-readiness gaps; apply any minor accepted edits, record the stop
reason, and continue to Phase 6.

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
- Every finding was classified, and no accepted material readiness gap remains
  unresolved.
- The final report exists and declares `implementation-ready` or
  `blocked-for-user-decision`.

## Final handoff

Echo the report path, the reviewed plan/task/context paths, the plan reviewer
dispatch roles used, accepted changes made, rejected suggestions, and
final readiness.
