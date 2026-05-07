---
name: implement-plan
description: >-
  Execute an existing plan/task/context artifact set: make code changes, keep
  them aligned to the artifacts, and verify code, tests, docs, and memory before
  closeout. Use when the user asks to implement an already-written plan. Use
  `plan-work` when no plan exists and `complete-current-phase` for roadmap
  phases.
---

# implement-plan

Implement a plan without freelancing.
This skill expects three artifacts:
- a plan file
- a task file
- a context file (orientation for a cold start)

If they are not supplied explicitly, discover the latest matching set under `.agent-layer/tmp/`.

Use `plan-work` instead when no plan exists and the artifact set must be written first. Use `verify-against-plan` instead when the request is to report completeness without making changes.

## Defaults

- Do not start coding until you have a plan and task file, or clear user approval to infer a missing artifact.
- Default scope is exactly what the plan and task list describe.
- If the plan is ambiguous in a way that changes code behavior or scope, treat that as a blocker instead of guessing.
- If the context file is missing, proceed with just the plan and task but note its absence in the final handoff.

## Artifact discovery

Use the standard artifact naming rule under `.agent-layer/tmp/`:
- `<workflow>.<run-id>.plan.md`
- `<workflow>.<run-id>.task.md`
- `<workflow>.<run-id>.context.md`

Discovery rules:
1. List `.agent-layer/tmp/*.plan.md`, `.agent-layer/tmp/*.task.md`, and `.agent-layer/tmp/*.context.md`.
2. Keep only files matching the standard naming rule with a valid `run-id`.
3. Build candidate sets when both `.plan.md` and `.task.md` exist for the exact same `<workflow>` and `<run-id>`. A matching `.context.md` is expected but not required.
4. Select the set with the latest `run-id` in lexicographic order.
5. If the user meant an older or different set, require explicit paths instead of guessing.

Fallback:
- If no valid plan/task pair exists, ask the user for explicit paths or regenerate them first.

## Multi-agent pattern

Recommended roles:
1. `Context scout`: maps plan steps to actual files and dependencies.
2. `Execution gatekeeper`: decides whether the current task batch should `proceed`, `revise`, `escalate`, or `rewrite-because-out-of-scope`.
3. `Implementer`: owns a focused subset of the changes.
4. `Verifier`: runs commands and checks behavior against the plan.

For multi-file work, parallelize read-only exploration first, then implement in reviewable batches.

## Global constraints

- Treat the plan/task pair as the execution contract and the context file as the orientation guide.
- Do not start coding without a valid plan/task pair or explicit user approval to infer a missing artifact.
- Keep changes tightly scoped to the plan after the gatekeeper returns `proceed`.
- Tests, docs, and memory updates are part of implementation when the plan requires them.
- If new evidence invalidates part of the plan, jump back to the earliest affected task or planning assumption instead of pushing forward.
- Treat readiness gating as an internal execution decision, not as a reason to ask the user unless a human checkpoint is actually triggered.

## Human checkpoints

- Required: ask when the plan/task pair is missing, mismatched, or non-latest and the intended set is unclear.
- Required: ask when implementation would materially deviate from the plan or change behavior the plan did not settle.
- Required: ask before destructive or irreversible actions that are not explicitly covered by the plan.
- When a checkpoint involves a genuine tradeoff between substantive alternatives, present at least two options with brief pros and cons, state which you recommend and why, and let the human decide.
- Stay autonomous while executing a clear gated plan.

## Implementation workflow

### Phase 1: Preflight (Context scout)

1. Load the context file first (if it exists). Read it fully to orient on:
   - the key files and their roles
   - the current state of the code before changes
   - constraints and dependencies
   - the recommended entry point
2. Load the plan and task artifacts.
3. Confirm all artifacts match:
   - same objective
   - same scope
   - compatible task ordering
   - context file key files align with the plan's scope
4. Read project standards and verification commands as needed:
   - `README.md`
   - `ROADMAP.md` and `DECISIONS.md` when the work is architectural
   - `COMMANDS.md` before choosing validation commands
5. Read the entry point file(s) identified in the context file, then any additional code and docs needed to execute the first task batch.
6. The execution gatekeeper then chooses exactly one verdict:
   - `proceed`: the current batch is ready to implement as written
   - `revise`: the plan or task list needs updates first
   - `escalate`: a human checkpoint is actually required
   - `rewrite-because-out-of-scope`: the current batch should be rewritten to an equivalent in-scope slice before coding

If the verdict is `proceed`, continue to Phase 2.
If the verdict is `revise`, update or regenerate the plan/task pair and restart Phase 1.
If the verdict is `escalate`, ask the smallest question that unblocks trustworthy execution.
If the verdict is `rewrite-because-out-of-scope`, rewrite the current task batch or task ordering to stay inside the plan's real scope, record the rewrite for the final handoff, and restart Phase 1.

### Phase 2: Execute the task list (Implementer)

Execution rules:
- work in task order unless a dependency forces reordering
- keep diffs explainable and localized
- update or add tests as part of the same change
- update docs when behavior or workflow changed
- update project memory files when the change materially affects roadmap, decisions, issues, backlog, or repeatable commands

When a task becomes larger than expected:
- split it
- note the split for the final handoff
- continue only if scope still matches the plan

If the touched scope accumulates obvious local complexity, dead scaffolding, or oversized files and the simplification would remain behavior-preserving and in-scope:
- use the `simplify-code` skill
- then continue to Phase 4

### Phase 3: Track deviations (Implementer)

If you must deviate from the plan:
- document the deviation for the final handoff
- explain why
- note whether the change is:
  - equivalent
  - narrower
  - broader

If the deviation broadens scope materially, hand it back to the execution gatekeeper instead of freelancing.

### Phase 4: Verify against the plan (Verifier)

Before wrapping up, confirm:
- every planned user-visible or maintainer-visible outcome exists
- every required test/doc/memory update was handled
- verification commands were actually run
- no major task list item was skipped silently

When no broader orchestrator already owns closeout, use the `finish-task` skill after Phase 4.
If it finds stale memory, incomplete plan work, or missing verification, jump back to the earliest affected phase.

## Guardrails

- Do not treat the plan as inspiration. Treat it as the execution contract.
- Do not skip tests because they are inconvenient.
- Do not leave docs or memory stale when the plan called for updates.
- Do not claim verification without observed command output.
- Do not expand into unrelated cleanup just because you noticed it.

## Definition of done

- Every planned task-list item is either marked complete (with observable code/test/doc evidence) or recorded as a named deviation with classification (equivalent / narrower / broader).
- Tests, docs, and memory updates promised by the plan were delivered in the same run; any skipped updates are listed in the final handoff with reasons.
- Verification commands from the plan ran and their observed output is recorded — no "should pass" claims without execution.

## Final handoff

After execution:
1. Summarize completed work, including plan/task/context paths used.
2. Name any deviations, task splits, or missing context file.
3. State whether the plan appears complete or whether follow-up remains.
