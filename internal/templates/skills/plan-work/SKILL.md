---
name: plan-work
description: >-
  Write a scoped implementation plan and task list for a requested change.
  Use when the user asks for a plan, roadmap slice, execution strategy, task
  breakdown, current roadmap-phase slice, or pre-implementation design
  artifact before coding.
---

# plan-work

Write a plan that is clear enough for a fresh agent to execute without guessing.
The output is two artifacts:
- a narrative plan
- a small ordered task list

The plan should be specific, testable, and tightly scoped to the user's request.

## Defaults

- Default to planning only. Do not edit code unless the user explicitly asks for implementation too.
- Default scope is the smallest coherent slice that produces a reviewable outcome.
- If the request is to plan roadmap execution and no phase is named, use the first incomplete roadmap phase and plan the smallest coherent slice inside it.
- If the work is architectural or cross-cutting, read the project roadmap/decision context first.

## Required artifacts

Use the standard artifact naming rule under `.agent-layer/tmp/`:
- `.agent-layer/tmp/plan-work.<run-id>.plan.md`
- `.agent-layer/tmp/plan-work.<run-id>.task.md`

Use one shared `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create files with `touch` before writing.

## Inputs

Accept any combination of:
- a plain-language user request
- one or more target files or directories
- an issue report or review artifact
- roadmap phase/task references
- constraints such as time, risk tolerance, or verification depth

If the user does not provide targets, infer the minimum necessary scope from the request and say what you chose.

## Multi-agent pattern

Use subagents liberally when available.

Recommended roles:
1. `Scout`: gathers focused repo context and constraints.
2. `Planner`: drafts the plan and task list.
3. `Critic`: reviews the draft for missing scope, weak verification, and unsafe assumptions.
4. `Execution gatekeeper`: decides whether the artifact pair should `proceed`, `revise`, `escalate`, or `rewrite-because-out-of-scope`.

If subagents are unavailable, do these passes inline and label them clearly.

## Global constraints

- Do not edit code unless the user explicitly asked for implementation as part of the same request.
- Do not hide ambiguity inside the plan.
- Keep the plan grounded in the actual repo context, not generic best-practice filler.
- Treat tests, docs, and memory updates as first-class planned work when they are affected.
- Treat execution gating as an internal readiness decision for the artifact pair, not as a reason to ask the user unless a human checkpoint is actually triggered.

## Human checkpoints

- Required: ask when ambiguity would materially change scope, behavior, or architecture.
- Required: ask when repo context reveals multiple valid approaches with real user-facing or sequencing tradeoffs.
- Stay autonomous while gathering context, drafting, critiquing, and gating the artifact pair.

## Planning workflow

### Phase 1: Preflight (Scout)

1. Restate the objective in one sentence.
2. Identify the exact planning target:
   - feature
   - bug fix
   - refactor
   - roadmap slice
   - issue batch
3. If the change is architectural or cross-cutting, read:
   - `ROADMAP.md`
   - `DECISIONS.md`
   - relevant `BACKLOG.md` and `ISSUES.md` entries
4. Before recommending verification commands, read `COMMANDS.md`.
5. Read only the files needed to understand the target area. Avoid broad repo scans unless the request truly demands it.
6. If the target is a roadmap phase:
   - default to the first incomplete phase when none is named
   - assess whether it can be planned as one safe, coherent slice
   - if not, carve the smallest safe slice inside that phase and state the boundary explicitly
   - treat risky or ambiguous decomposition as a blocker instead of guessing

### Phase 2: Draft the plan (Planner)

The plan file must include these sections:

1. `# Objective`
   - what will change
   - what will not change
   - what success looks like for a user or maintainer
2. `## Scope`
   - in-scope work
   - out-of-scope work
   - assumptions
3. `## Context`
   - key files, modules, or docs involved
   - any relevant roadmap or decision constraints
4. `## Approach`
   - the intended design or execution path
   - why this path is preferred over obvious alternatives
5. `## Risks`
   - behavior regressions
   - migration or compatibility concerns
   - unclear dependencies
6. `## Verification`
   - exact commands to run
   - what each command proves
7. `## Exit Criteria`
   - objective conditions that define completion

Write prose first. Use bullets only when they add clarity.

### Phase 3: Draft the task list (Planner)

The task file should be a compact ordered checklist that mirrors the plan.

Requirements:
- keep items small and verifiable
- include tests/docs/memory updates when applicable
- include a final verification step
- group by execution order, not by file count

Preferred format:

```md
# Task List

- [ ] Confirm scope and context
- [ ] Implement change set A
- [ ] Add or update tests
- [ ] Update docs/memory if affected
- [ ] Run verification commands
```

### Phase 4: Critique the draft before presenting it (Critic)

Review the plan and task list against this checklist:
- Does the scope match the user request exactly?
- Are non-goals explicit?
- Are dependencies ordered before dependents?
- Is verification credible for the risk level?
- Are docs, tests, and memory updates accounted for when needed?
- Would a fresh agent know where to start without hidden context?

If the answer to any item is no, revise before presenting.

### Phase 5: Gate the execution handoff (Execution gatekeeper)

Choose exactly one verdict for the artifact pair:
- `proceed`: the plan and task list are ready for execution as written
- `revise`: the artifacts are close, but need another drafting pass first
- `escalate`: a human checkpoint is actually required
- `rewrite-because-out-of-scope`: the request should be rewritten around a smaller in-scope slice before handoff

If the verdict is `revise`, update the draft and repeat Phases 2-4 as needed.
If the verdict is `escalate`, ask the smallest question that unblocks a trustworthy plan.
If the verdict is `rewrite-because-out-of-scope`, rewrite the plan around the smallest safe in-scope slice and return to the earliest affected phase.

## Guardrails

- Do not produce vague tasks like `fix code` or `handle edge cases`.
- Do not hide large refactors inside a "simple" plan.
- Do not assume missing inputs, secrets, schema details, or desired behavior.
- Prefer root-cause plans over band-aids, but call out when root-cause work expands scope.
- If the right fix is materially larger than requested, say so in the scope section.

## Final handoff

After writing the artifacts:
1. Echo both artifact paths.
2. Summarize the plan in a few sentences.
3. State the gatekeeper verdict and highlight the biggest risk or open question, if any.
