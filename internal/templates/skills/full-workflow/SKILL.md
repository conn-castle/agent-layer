---
name: full-workflow
description: >-
  Orchestrate a full feature workflow from questions and spec alignment through
  reviewed planning, fully implemented local work, and PR shipping.
---

# full-workflow

Top-level orchestration skill. Own question, spec, and spec-gate alignment; after
that, delegate to `/plan-work`, `/fully-implement-plan`, and `/ship-pr`.

## Required inputs

Fail before side effects unless all are present:
- `planner`: dispatch agent role
- `implementer`: dispatch agent role
- `shipper`: dispatch agent role
- `review_agents`: one or more dispatch agent roles
- the user's requested work

Dispatch agent roles may be terse (`codex xhigh`, `claude opus xhigh`,
`antigravity`). Infer the agent only when unambiguous. Before dispatching,
inspect live `al dispatch options` output and fail if a requested override is
unsupported.

Example invocation:

```text
/full-workflow
{requested work}
planner is codex xhigh
implementer is codex high
shipper is claude opus xhigh
review_agents are codex high, opus xhigh, antigravity
```

## Required artifacts

Use one shared `run-id = YYYYMMDD-HHMMSS-<short-rand>` for this workflow:
- `.agent-layer/tmp/full-workflow.<run-id>.spec.md`

Create the spec before writing.

## Rules

- Treat the question and spec phases as user discussion, not automation.
  Planning starts only after explicit final spec approval.
- Assume the user will not read artifact files. Keep every question, decision,
  spec summary, and approval request self-contained in chat.
- Use initial questions only to learn enough for a first draft. Put unresolved
  gaps in the spec, then resolve them with the user one at a time.
- Ask only for decisions affecting end-user-facing behavior, architecture, scope,
  sequencing, risk, cost, or shipping; include options, tradeoffs, and a
  recommendation when useful.
- Separate facts from choices: repo reading may resolve facts, constraints, and
  existing behavior; inferred or recommended choices stay open until approved.
- After the spec gate, do not perform delegated-stage work yourself. Use the
  planner for `/plan-work`, implementer for `/fully-implement-plan`, and shipper
  for `/ship-pr`.
- Treat delegated returns as intermediate; continue orchestration after each return.
- Ask again if later evidence would materially change the aligned spec.
- Never replace a missing role with the current agent, widen scope beyond the
  spec, or guess alternate dispatch options after failure.
- `/ship-pr` keeps its own merge authorization gate unless the user grants a
  separate policy.

## Workflow

### Phase 1: Initialize

Validate required inputs and dispatch roles before writing artifacts or
dispatching child agents.

### Phase 2: Initial questions

Clarify only enough to write the first draft:
- read focused repo context for facts
- record facts separately from user decisions
- ask the smallest blocking question when the request is too ambiguous to draft

Proceed when a useful draft can capture remaining gaps.

### Phase 3: Draft spec

Write a draft spec with these sections:
1. `# Objective`
2. `## Scope`
3. `## Non-goals`
4. `## User-Confirmed Decisions`
5. `## Constraints`
6. `## Acceptance Criteria`
7. `## Shipping Expectations`
8. `## Open Questions`

Only put decisions in `User-Confirmed Decisions` when the user already answered
them or the original request explicitly fixed them. Put inferred or recommended
choices in `Open Questions`.

`Open Questions` is the iteration queue.

### Phase 4: Spec iteration

While the iteration queue is not empty:
- ask exactly one open question
- update the spec after the answer
- repeat

### Phase 5: Spec gate

Summarize the draft spec in chat:
- what will change
- what will not change
- the acceptance criteria
- important constraints and decisions

Ask for approval or corrections, then stop. If the user changes the spec, update
it and return to Phase 4 when questions or decisions remain.

### Phase 6: Plan And Review

Dispatch the planner role with:

```text
/plan-work
{relative path to spec}
review_agents are {review agent 1, review agent 2, ...}
```

### Phase 7: Fully Implement

Dispatch the implementer role with:

```text
/fully-implement-plan
Plan artifacts:
{relative path to reviewed plan artifact}
{relative path to reviewed task artifact}
{relative path to reviewed context artifact}
review_agents are {review agent 1, review agent 2, ...}
```

### Phase 8: Ship

Dispatch the shipper role with:

```text
/ship-pr
review_agents are {review agent 1, review agent 2, ...}
```

Stop at any `/ship-pr` human checkpoint, including merge authorization.

## Definition of done

- Required dispatch agent roles were present and normalized before dispatch.
- The spec gate completed before planning.
- `/plan-work`, `/fully-implement-plan`, and `/ship-pr` were invoked through the
  requested dispatch agent roles.
- Final status and artifact paths are reported in the final handoff.

## Final handoff

Report the spec, plan/task/context, review report, fully-implement-plan report,
and PR paths or URLs. State whether the PR is open, green, merged, or blocked at
a human checkpoint.
