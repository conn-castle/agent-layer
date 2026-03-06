---
name: implement-plan
description: >-
  Implement an approved plan and task-list pair, keep changes aligned to the
  artifacts, and verify code, tests, docs, and memory updates before closing
  the task.
---

# implement-plan

Implement a plan without freelancing.
This skill expects two separate artifacts:
- a plan file
- a task file

If they are not supplied explicitly, discover the latest matching pair under `.agent-layer/tmp/`.

## Defaults

- Do not start coding until you have both artifacts or clear user approval to infer the missing one.
- Default scope is exactly what the plan and task list describe.
- If the plan is ambiguous in a way that changes code behavior or scope, treat that as a blocker instead of guessing.

## Artifact discovery

Use the standard artifact naming rule under `.agent-layer/tmp/`:
- `<workflow>.<run-id>.plan.md`
- `<workflow>.<run-id>.task.md`

Discovery rules:
1. List `.agent-layer/tmp/*.plan.md` and `.agent-layer/tmp/*.task.md`.
2. Keep only files matching the standard naming rule with a valid `run-id`.
3. Build candidate pairs only when both files exist for the exact same `<workflow>` and `<run-id>`.
4. Select the pair with the latest `run-id` in lexicographic order.
5. If the user meant an older or different pair, require explicit paths instead of guessing.

Fallback:
- If no valid pair exists, ask the user for explicit plan and task paths or regenerate them first.

## Required artifact

Write an execution report to:
- `.agent-layer/tmp/implement-plan.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create the file with `touch` before writing.

## Multi-agent pattern

Use subagents liberally when available.

Recommended roles:
1. `Context scout`: maps plan steps to actual files and dependencies.
2. `Implementer`: owns a focused subset of the changes.
3. `Verifier`: runs commands and checks behavior against the plan.

For multi-file work, parallelize read-only exploration first, then implement in reviewable batches.

## Global constraints

- Treat the plan/task pair as the execution contract.
- Do not start coding without a valid artifact pair or explicit user approval to infer the missing artifact.
- Keep changes tightly scoped to the approved plan.
- Tests, docs, and memory updates are part of implementation when the plan requires them.
- If new evidence invalidates part of the plan, jump back to the earliest affected task or planning assumption instead of pushing forward.

## Human checkpoints

- Required: ask when the plan/task pair is missing, mismatched, or non-latest and the intended pair is unclear.
- Stay autonomous while executing a clear approved plan.

## Implementation workflow

### Phase 1: Preflight (Context scout)

1. Load the plan and task artifacts.
2. Confirm they actually match:
   - same objective
   - same scope
   - compatible task ordering
3. Read project standards and verification commands as needed:
   - `README.md`
   - `ROADMAP.md` and `DECISIONS.md` when the work is architectural
   - `COMMANDS.md` before choosing validation commands
4. Read the minimum code and docs needed to execute the first task batch.

### Phase 2: Execute the task list (Implementer)

Execution rules:
- work in task order unless a dependency forces reordering
- keep diffs explainable and localized
- update or add tests as part of the same change
- update docs when behavior or workflow changed
- update project memory files when the change materially affects roadmap, decisions, issues, backlog, or repeatable commands

When a task becomes larger than expected:
- split it
- note the split in the report
- continue only if scope still matches the plan

If the touched scope accumulates obvious local mechanical complexity, dead scaffolding, or oversized files and the cleanup would remain behavior-preserving and in-scope:
- use the `mechanical-cleanup` skill
- then continue to Phase 4

### Phase 3: Track deviations (Implementer)

If you must deviate from the plan:
- document the deviation in the report
- explain why
- note whether the change is:
  - equivalent
  - narrower
  - broader

If the deviation broadens scope materially, stop execution and return to planning instead of freelancing.

### Phase 4: Verify against the plan (Verifier)

Before wrapping up, confirm:
- every planned user-visible or maintainer-visible outcome exists
- every required test/doc/memory update was handled
- verification commands were actually run
- no major task list item was skipped silently

When no broader orchestrator already owns closeout, use the `finish-task` skill after Phase 4.
If it finds stale memory, incomplete plan work, or missing verification, jump back to the earliest affected phase.

## Execution report format

Write `.agent-layer/tmp/implement-plan.<run-id>.report.md` with:

1. `# Objective`
2. `## Inputs`
   - plan path
   - task path
3. `## Work Completed`
4. `## Deviations`
5. `## Verification Run`
6. `## Docs and Memory Updates`
7. `## Remaining Follow-up`

## Guardrails

- Do not treat the plan as inspiration. Treat it as the execution contract.
- Do not skip tests because they are inconvenient.
- Do not leave docs or memory stale when the plan called for updates.
- Do not claim verification without observed command output.
- Do not expand into unrelated cleanup just because you noticed it.

## Final handoff

After execution:
1. Echo the report path.
2. Summarize completed work.
3. State whether the plan appears complete or whether follow-up remains.
