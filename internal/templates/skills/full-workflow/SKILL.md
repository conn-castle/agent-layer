---
name: full-workflow
description: >-
  Orchestrate a full feature workflow from questions and spec alignment through
  plan, multi-agent plan review, implementation, and PR shipping.
---

# full-workflow

Parent orchestration skill. Own question, spec, and spec-gate alignment; after
that, delegate each stage to its dedicated skill.

## Required inputs

Fail before side effects unless all are present:
- `planner`: dispatch agent role
- `implementer`: dispatch agent role
- `shipper`: dispatch agent role
- `review_agents`: one or more dispatch agent roles
- the user's requested work

Dispatch agent roles may be terse (`codex xhigh`, `claude opus xhigh`,
`antigravity`). Infer the agent only when unambiguous. Before dispatching, follow
`agent-dispatch`, inspect live options, and fail if a requested override is
unsupported.

Example invocation:

```text
/full-workflow
planner is codex xhigh
implementer is codex high
shipper is claude opus xhigh
review agents are codex high, opus xhigh, antigravity
```

## Required artifacts

Use one shared `run-id = YYYYMMDD-HHMMSS-<short-rand>` for this workflow:
- `.agent-layer/tmp/full-workflow.<run-id>.state.md`
- `.agent-layer/tmp/full-workflow.<run-id>.spec.md`

Create both before writing. Record delegated artifact paths in the state file as
they appear.

## Rules

- The question phase drives the conversation. Ask substantive questions as soon
  as they arise, not as a batch at the spec gate.
- Ask only for decisions that affect user-facing behavior, architecture, scope,
  sequencing, risk, cost, or shipping expectations. Frame each as the decision,
  why it matters, and options when relevant; for tradeoffs include brief pros,
  cons, and a recommendation. Avoid generic questionnaires.
- The spec gate is mandatory. Explain the spec, invite questions or corrections,
  and move to planning only after the user indicates alignment.
- After the spec gate, do not perform child-stage work yourself. Use the planner
  for `plan-work`, review agents for `multi-agent-plan-review`, implementer for
  `implement-plan`, and shipper for `ship-pr`.
- Treat child returns as intermediate; continue orchestration after each return.
- Ask again if later evidence would materially change the aligned spec.
- Never replace a missing role with the current agent, widen scope beyond the
  spec, or guess alternate dispatch options after failure.
- `ship-pr` keeps its own merge authorization gate unless the user grants a
  separate policy.

## Workflow

### Phase 1: Initialize state

1. Validate required inputs.
2. Record dispatch agent roles, the original request, and current phase in the
   state file.

### Phase 2: Questions

Drive unknowns to ground:
- read focused repo context when code or docs can answer a question
- record resolved decisions and assumptions in the state file

Proceed when the work can be specified without guessing.

### Phase 3: Spec

Write the spec with these sections:
1. `# Objective`
2. `## Scope`
3. `## Non-goals`
4. `## Resolved Decisions`
5. `## Constraints`
6. `## Acceptance Criteria`
7. `## Shipping Expectations`
8. `## Open Questions`

`Open Questions` must be `None` before the spec can pass the gate.

### Phase 4: Spec gate

Explain:
- what will change
- what will not change
- the acceptance criteria
- the important constraints and decisions

Ask whether the spec matches the user's intent. If they ask questions or change
direction, revise the spec and repeat the gate.

### Phase 5: Plan

Dispatch the planner role with the `plan-work` skill. The prompt must include:
- the spec path
- the state path
- the user's original request
- instruction to produce the standard plan/task/context artifact set

Record the returned artifact paths in the state file.

### Phase 6: Cross-agent plan review

Use `multi-agent-plan-review` with:
- the reviewer dispatch agent roles
- the plan/task/context artifact paths
- the spec path as the review contract

Record the final review report path and the reviewed artifact paths.

### Phase 7: Implement

Dispatch the implementer role with the `implement-plan` skill and the reviewed
plan/task/context artifact paths. If implementation discovers a spec-level
change, return to the earliest affected phase instead of continuing on stale
alignment.

### Phase 8: Ship

Dispatch the shipper role with the `ship-pr` skill. Stop at any `ship-pr` human
checkpoint, including merge authorization.

## Definition of done

- Required dispatch agent roles were present and normalized before dispatch.
- The spec has no open questions and was aligned with the user before planning.
- `plan-work`, `multi-agent-plan-review`, `implement-plan`, and `ship-pr` were
  invoked through the requested dispatch agent roles.
- Final status and artifact paths are recorded in the state file.

## Final handoff

Report the spec, plan/task/context, review report, implementation report, and PR
paths or URLs. State whether the PR is open, green, merged, or blocked at a
human checkpoint.
