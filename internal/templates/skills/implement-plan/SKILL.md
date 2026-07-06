---
name: implement-plan
description: >-
  Use when the user provides plan, task, and context artifact paths and
  asks to apply the planned changes.
---

# implement-plan

Implement a plan without freelancing. This skill requires three explicit
artifact paths:

- a plan file
- a task file
- a context file for cold-start orientation

If any artifact path is missing, stop and ask for the missing path. Do not
discover, infer, or auto-select artifacts from `.agent-layer/tmp/`.

## Defaults

- Do not start coding until explicit plan, task, and context paths are
  available.
- If any artifact path does not exist, treat that as a blocker instead of
  guessing.
- Default scope is exactly what the plan and task list describe.
- If the plan is ambiguous in a way that changes code behavior or scope, treat
  that as a blocker instead of guessing.
- Do not turn implementation into a new planning, review, or verification
  workflow.
- Do not run tests, builds, linters, formatters, or other verification commands
  from this skill.
- Record what changed, what deviated, and what remains. Independent completion
  judgment is outside this skill.

## Required inputs

The caller must provide paths to all three artifacts. They usually follow the
standard artifact naming rule under `.agent-layer/tmp/`:

- `<workflow>.<run-id>.plan.md`
- `<workflow>.<run-id>.task.md`
- `<workflow>.<run-id>.context.md`

Do not select artifacts by listing `.agent-layer/tmp/`. If the user intended a
specific set, require its exact paths.

## Required artifact

Write an execution report to:

- `.agent-layer/tmp/implement-plan.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`. Create the file before writing.

## Implementation workflow

### Phase 1: Preflight

1. Load the context file first.
2. Load the plan and task artifacts.
3. Confirm all artifacts match the same objective, scope, and task ordering.
4. Read the entry point files identified in the context file, then any
   additional code and docs needed for the first task batch.
5. Choose one readiness verdict:
   - `proceed`: the current batch is ready to implement as written
   - `revise`: the plan or task list needs updates before coding
   - `escalate`: a human checkpoint is required
   - `rewrite-because-out-of-scope`: the current batch should be rewritten to
     stay inside the plan's real scope

If the verdict is not `proceed`, resolve that condition before coding. Record
any equivalent task rewrite in the execution report.

### Phase 2: Execute the task list

Execution rules:

- work in task order unless a dependency forces reordering
- keep diffs explainable and localized
- update docs when user-facing or workflow behavior changed
- update memory files when the change materially affects roadmap, decisions,
  issues, backlog, repeatable commands, or stable project context

When a task becomes larger than expected:

- split it
- note the split in the report
- continue only if scope still matches the plan

### Phase 3: Track deviations

If you must deviate from the plan:

- document the deviation in the report
- explain why
- tag the change as:
  - `equivalent`
  - `narrower`
  - `broader`

If the deviation broadens scope materially, stop and ask before implementing
the broader change.

## Execution report format

Write `.agent-layer/tmp/implement-plan.<run-id>.report.md` with only these
sections:

1. `## Deviations`
   - Each deviation tagged `equivalent`, `narrower`, or `broader`, with a
     one-line reason.
   - Include task splits and any `rewrite-because-out-of-scope` rewrites here.
   - Use `None` when no deviations occurred.
2. `## Remaining Follow-up`
   - Plan items skipped, docs/memory updates deferred, verification commands
     left for a verifier, and any other open threads, each with a reason.
   - Use `None` when nothing is outstanding.

Keep the report short. Do not re-narrate work that is already visible in the
diff.

## Guardrails

- Do not treat the plan as inspiration. Treat it as the execution contract.
- Do not skip in-scope docs or memory updates silently.
- Do not expand into unrelated cleanup just because you noticed it.
- Do not mark independent completion or broad verification as done from inside
  this implementation step.

## Definition of done

- Every planned task-list item is implemented or recorded as a named deviation
  or remaining follow-up.
- Docs and memory updates promised by the plan were delivered or listed under
  `## Remaining Follow-up` with reasons.
- Verification commands are left for a separate verification workflow and are
  not claimed here.
- No major task list item was skipped silently.

## Final handoff

After execution:

1. Echo the report path.
2. Summarize completed work, including plan/task/context paths used.
3. Name any deviations or task splits.
4. State whether implementation is ready for independent verification or
   follow-up remains.
