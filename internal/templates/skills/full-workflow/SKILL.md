---
name: full-workflow
description: >-
  Orchestrate a full feature workflow from questions and spec alignment through
  reviewed planning, fully implemented local work, and PR shipping.
---

# full-workflow

Own specification alignment, dispatch `/plan-work`, then dispatch `/ship-plan`.

## Required inputs

Fail before side effects unless all are present:
- `planner`: dispatch agent role
- `implementer`: dispatch agent role
- `fixer`: dispatch agent role
- `shipper`: dispatch agent role
- `plan_reviewers`: one or more dispatch agent roles
- the user's requested work

Dispatch agent roles may be terse (`codex xhigh`, `claude opus xhigh`,
`antigravity`). Infer the agent only when unambiguous. Before dispatching,
inspect live `al dispatch options` output; fail rather than substituting an
unsupported role or override.

## Required artifact

Create `.agent-layer/tmp/full-workflow.<run-id>.spec.md`, where
`run-id = YYYYMMDD-HHMMSS-<short-rand>`.

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

- Put every user-facing question, summary, and approval request in chat; do not
  require the user to read artifacts.
- Ask only for decisions affecting end-user-facing behavior, architecture, scope,
  sequencing, risk, cost, or shipping; include options, tradeoffs, and a
  recommendation when useful.
- If either called skill needs a user answer or approval, relay its exact
  request, wait, then resume the same phase. If it fails or omits output required
  by that phase, report the failure and end the run. Otherwise, continue.
- If planning or shipping requires a change to the approved spec, update the
  spec, run Phases 3-5 again, then continue with Phase 6.
- Do not widen scope beyond the approved spec.

## Workflow

### Phase 1: Initial questions

Read focused repo context for facts. Ask a blocking question only when the
request is too ambiguous to draft; leave other unresolved choices for Phase 3.

### Phase 2: Draft spec

Write a draft spec with these sections:
1. `# Objective`
2. `## Scope`
3. `## Non-goals`
4. `## User-Confirmed Decisions`
5. `## Constraints`
6. `## Acceptance Criteria`
7. `## Shipping Expectations`
8. `## Open Questions`

Put facts in `Constraints`, choices fixed by the request or answered by the user
in `User-Confirmed Decisions`, and every other choice in `Open Questions`.

### Phase 3: Spec iteration

While `Open Questions` is not empty:
- ask exactly one open question
- update the spec after the answer
- repeat

### Phase 4: Approve spec

Summarize the draft spec:
- what will change
- what will not change
- the acceptance criteria
- important constraints and decisions

Ask for approval or corrections. Apply corrections, resolve resulting open
questions through Phase 3, and repeat this phase until the user approves the
spec.

### Phase 5: Plan and review

Dispatch the planner role with:

```text
/plan-work
{relative path to spec}
plan_reviewers are {agent 1, agent 2, ...}
```

Require plan, task, context, and review report paths with final readiness
`implementation-ready`.

### Phase 6: Ship plan

Dispatch the shipper role with:

```text
/ship-plan
Plan artifacts:
{relative path to reviewed plan artifact}
{relative path to reviewed task artifact}
{relative path to reviewed context artifact}
implementer is {implementer}
fixer is {fixer}
plan_reviewers are {agent 1, agent 2, ...}
```

## Final handoff

Report the spec path, all Phase 5 artifact paths and readiness, and the
`/ship-plan` final handoff.
