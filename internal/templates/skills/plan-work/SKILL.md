---
name: plan-work
description: >-
  Write a scoped implementation plan, task list, and context file for a
  requested change, roadmap slice, execution strategy, task breakdown, or
  pre-implementation design artifact.
---

# plan-work

Write a plan that is clear enough for a fresh agent to execute without guessing.
The output is three artifacts:
- a narrative plan
- a small ordered task list
- an implementation context file

The plan should be specific, testable, and tightly scoped to the user's request.
The context file provides the orientation a fresh agent needs to begin implementing
without re-discovering what the planner already found.

Use `implement-plan` instead when a valid plan/task/context set already exists and the request is to execute it. Use `complete-current-phase` instead when the scope is a full roadmap phase that also needs orchestrated implementation and closeout.

## Defaults

- Default to planning only. Do not edit code unless the user explicitly asks for implementation too.
- Default scope is the smallest coherent slice that produces a reviewable outcome.
- If the request is to plan roadmap execution and no phase is named, use the first incomplete roadmap phase and plan the smallest coherent slice inside it.
- If the work is architectural or cross-cutting, read the project roadmap/decision context first.

## Required artifacts

Use the standard artifact naming rule under `.agent-layer/tmp/`:
- `.agent-layer/tmp/plan-work.<run-id>.plan.md`
- `.agent-layer/tmp/plan-work.<run-id>.task.md`
- `.agent-layer/tmp/plan-work.<run-id>.context.md`

Use one shared `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create all three files with `touch` before writing.

## Inputs

Accept any combination of:
- a plain-language user request
- one or more target files or directories
- an issue report or review artifact
- roadmap phase/task references
- constraints such as time, risk tolerance, or verification depth

If the user does not provide targets, infer the minimum necessary scope from the request and say what you chose.

## Multi-agent pattern

Recommended roles:
1. `Scout`: gathers focused repo context and constraints.
2. `Planner`: drafts the plan and task list.
3. `Critic`: reviews the draft for missing scope, weak verification, and unsafe assumptions.
4. `Execution gatekeeper`: decides whether the artifact set should `proceed`, `revise`, `escalate`, or `rewrite-because-out-of-scope`.

If subagents are unavailable, do these passes inline and label them clearly.

## Global constraints

- Do not edit code unless the user explicitly asked for implementation as part of the same request.
- Do not hide ambiguity inside the plan.
- Drive every substantive unknown to ground before finishing the plan. Resolve unknowns by reading the relevant code, consulting docs or online sources, running a small experiment in `.agent-layer/tmp` when behavior can only be confirmed empirically, or asking the user. Hedge words ("likely", "probably", "should work", "I think") in the plan signal an unresolved unknown — investigate or escalate instead of writing them.
- Do not defer substantive decisions to implementation. If a decision affects user-facing behavior, architecture, sequencing, or scope, surface it during planning with concrete options.
- Keep the plan grounded in the actual repo context, not generic best-practice filler.
- Treat tests, docs, and memory updates as first-class planned work when they are affected.
- Treat execution gating as an internal readiness decision for the artifact set, not as a reason to ask the user unless a human checkpoint is actually triggered.

## Human checkpoints

- Required: Ask substantive questions as they arise during planning, before choosing or writing the affected approach.
- Substantive questions are questions where the answer changes user-facing behavior, architecture, scope, sequencing, risk, or cost.
- Required: ask when ambiguity would materially change scope, behavior, or architecture.
- Required: ask when repo context reveals multiple valid approaches with real user-facing or sequencing tradeoffs.
- When a checkpoint involves a genuine tradeoff between substantive alternatives, present at least two options with brief pros and cons, state which you recommend and why, and let the human decide.
- Do not save substantive questions for the execution gatekeeper. The gatekeeper catches questions discovered late; it is not a holding area for known decisions.
- After the user answers, incorporate the decision into the draft and record the chosen direction in the plan's assumptions, approach, or risks as appropriate.
- Decide non-substantive details autonomously using repo conventions, documented defaults, and the smallest coherent scope.
- Stay autonomous while gathering context, drafting, critiquing, and gating the artifact set.

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

Draft interactively when substantive decisions emerge. While writing the plan, pause and ask before committing to any approach that depends on a human-impacting or architecture-impacting tradeoff. Continue drafting only after the user's answer resolves that decision.

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

### Phase 3b: Draft the context file (Planner)

The context file is the orientation document for a fresh implementing agent that starts
with an empty conversation. It must contain everything the agent needs to begin work
without re-discovering what the planner already found.

The context file must include these sections:

1. `# Implementation Context`
   - one-sentence summary of what this plan changes and why
2. `## Key Files`
   - relative file paths with a brief description of each file's role in this plan
   - include files to read, files to modify, and files to create
   - order by relevance: most important first
3. `## Current State`
   - how the relevant code or system behaves before this plan is applied
   - include specific function names, types, or patterns when helpful
4. `## Constraints`
   - non-obvious facts, dependencies, or invariants discovered during planning
   - roadmap or decision constraints that affect implementation choices
   - version requirements, compatibility notes, or migration concerns
5. `## Entry Point`
   - where the implementing agent should start reading
   - the first file or function to open and why

Requirements:
- all file paths must be relative to the repository root
- every file listed must actually exist (or be explicitly marked as new)
- keep descriptions brief: one line per file in the key files list
- do not duplicate the plan's narrative; reference the plan for rationale
- do not include generic best practices; only include project-specific facts

### Phase 4: Critique the draft before presenting it (Critic)

Review the plan, task list, and context file against this checklist:
- Does the scope match the user request exactly?
- Are non-goals explicit?
- Are dependencies ordered before dependents?
- Is verification credible for the risk level?
- Are docs, tests, and memory updates accounted for when needed?
- Does the context file list every file the plan touches?
- Are all file paths in the context file valid (existing or explicitly marked as new)?
- Does the plan contain hedge words ("likely", "probably", "should work") that point to unresolved unknowns?
- Would a fresh agent with only these three artifacts know where to start without hidden context?

If the answer to any item is no, revise before presenting.

### Phase 5: Gate the execution handoff (Execution gatekeeper)

Choose exactly one verdict for the artifact set:
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

## Definition of done

- All three artifacts exist at `.agent-layer/tmp/plan-work.<run-id>.{plan,task,context}.md` under one shared run-id.
- The plan contains every required section (`Objective`, `Scope`, `Context`, `Approach`, `Risks`, `Verification`, `Exit Criteria`); the task file has an ordered checklist ending in a verification step; the context file lists key files, current state, constraints, and entry point.
- The Critic pass recorded an answer for every checklist question in Phase 4, and any `no` answers were revised before presenting.
- The execution gatekeeper recorded exactly one verdict (`proceed`, `revise`, `escalate`, or `rewrite-because-out-of-scope`) and the artifacts are in a state consistent with that verdict.

## Final handoff

After writing the artifacts:
1. Echo all three artifact paths (plan, task, context).
2. Summarize the plan in a few sentences.
3. State the gatekeeper verdict and highlight the biggest risk or open question, if any.
