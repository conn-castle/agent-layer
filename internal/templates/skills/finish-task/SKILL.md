---
name: finish-task
description: >-
  Wrap up a completed task by checking plan alignment, updating the project
  memory files with only the necessary changes, running credible verification,
  and summarizing the outcome.
---

# finish-task

This is the closeout workflow after implementation.
It is not a broad audit and it is not a second planning pass.

## Defaults

- Default scope is the current working tree and the files touched by the just-completed task.
- Use the current run's plan and task artifacts when they are already known.
- Default verification depth is the fastest credible repo-defined check.
- Update memory only where the completed task actually changed project truth.

## Inputs

Accept any combination of:
- explicit scope paths
- known plan or task artifact paths
- a verification depth preference
- whether roadmap updates should be considered
- whether to skip specific memory files

## Required artifact

Write the report to:
- `.agent-layer/tmp/finish-task.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create the file with `touch` before writing.

## Multi-agent pattern

Use subagents liberally when available.

Recommended roles:
1. `Change reviewer`: compares the delivered work to the intended scope.
2. `Memory curator`: updates only the affected memory files.
3. `Verifier`: runs the best fast checks available.
4. `Reporter`: writes the final closeout summary.

## Global constraints

- Keep scope tight to the completed task and its nearby context.
- Do not start a new broad audit in this workflow.
- Do not invent plan alignment if no plan artifact exists.
- Keep memory updates compact, deduplicated, and truthful.

## Closeout workflow

### Phase 0: Preflight (Change reviewer)

1. Confirm baseline with:
   - `git status --porcelain`
   - `git diff --stat`
2. Read `COMMANDS.md` before choosing verification commands.
3. Determine the review scope:
   - explicit user scope first
   - otherwise current uncommitted task changes

### Phase 1: Check plan alignment (Change reviewer)

If a plan artifact is available:
- compare promised work to actual changes
- record completed items
- record meaningful deviations and omissions

If no plan artifact exists, state that explicitly and continue.

### Phase 2: Curate memory updates (Memory curator)

Use the authoritative files only as needed:
- remove resolved items from `ISSUES.md`
- remove implemented items from `BACKLOG.md`
- update `ROADMAP.md` only when status genuinely changed
- update `DECISIONS.md` only for non-obvious, durable choices

Merge near-duplicates instead of appending noise.

### Phase 3: Verify the delivered slice (Verifier)

1. Run the fastest credible repo-defined verification command.
2. Escalate to broader checks only when the risk of the completed work warrants it.
3. If no credible verification command exists, report that limitation explicitly.

### Phase 4: Summarize the task closeout (Reporter)

Report:
- what changed
- whether the plan was completed
- what memory was updated
- what verification ran
- what remains deferred

## Required report structure

Write `.agent-layer/tmp/finish-task.<run-id>.report.md` with:

1. `# Task Closeout Summary`
2. `## Scope and Inputs`
3. `## Plan Alignment`
4. `## Memory Updates`
5. `## Verification`
6. `## Deferred Follow-up`

## Guardrails

- Do not log speculative future work that was not actually observed.
- Do not add routine implementation details to `DECISIONS.md`.
- Do not mark roadmap work complete without evidence in code, docs, or tests.
- Do not skip verification silently.

## Final handoff

After writing the report:
1. Echo the report path.
2. Summarize the task outcome and any memory updates made.
3. State the verification run and result, plus any deferred risk or follow-up.
