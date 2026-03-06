---
name: fix-issues
description: >-
  Triage a coherent subset of `ISSUES.md`, write a plan and task list,
  implement the fixes when the scope is clear enough, audit the touched area,
  verify the results, and keep `ISSUES.md` current throughout.
---

# fix-issues

This is the issue-ledger maintenance workflow.
It should:
- choose a reviewable subset of open issues
- plan the work explicitly
- stop only when scope or behavior is not clear enough to proceed safely
- implement, audit, verify, and close the selected issues

## Defaults

- Default mode is plan-first unless the surrounding request clearly includes execution and the selected issue batch is unambiguous.
- Default issue batch is the smallest coherent subset, capped at 3 issues unless the user says otherwise.
- Default file-touch budget is reviewable, not exhaustive.
- Default verification depth is the fastest credible repo-defined check, escalating when risk warrants it.

## Required artifacts

Use one shared `run-id = YYYYMMDD-HHMMSS-<short-rand>`.

Create:
- `.agent-layer/tmp/fix-issues.<run-id>.plan.md`
- `.agent-layer/tmp/fix-issues.<run-id>.task.md`
- `.agent-layer/tmp/fix-issues.<run-id>.report.md`

Create files with `touch` before writing.

## Inputs

Accept any combination of:
- an issue count or explicit issue identifiers
- whether this run is plan-only or execute-after-approval
- a verification depth preference
- a scope preference such as targeted or all-selected

## Multi-agent pattern

Use subagents liberally when available.

Recommended roles:
1. `Issue triage lead`: selects the issue batch.
2. `Planner`: drafts the plan and task artifacts.
3. `Implementer`: owns the code changes.
4. `Auditor`: reviews touched areas for regressions and missed fixes.
5. `Verifier`: runs the repo-defined checks.
6. `Reporter`: writes the final resolution report.

## Global constraints

- Keep scope to the selected issue batch plus directly blocking prerequisites.
- Do not invent missing issue details; treat ambiguous issues as blockers to clarify.
- Keep changes reviewable and aligned with repo conventions.
- Remove resolved issues from `ISSUES.md`; log newly discovered out-of-scope problems instead of silently expanding scope.
- If new evidence invalidates the plan, jump back to the earliest affected phase instead of continuing on stale assumptions.

## Issue workflow

### Phase 0: Preflight (Issue triage lead)

1. Confirm baseline with:
   - `git status --porcelain`
   - `git diff --stat`
2. Read, in order, when they exist:
   - `ISSUES.md`
   - `ROADMAP.md`
   - `DECISIONS.md`
   - `COMMANDS.md`
   - `README.md`

### Phase 1: Select the issue batch (Issue triage lead)

1. Respect any user-specified issue IDs or count.
2. Otherwise choose the smallest coherent subset by:
   - shared area
   - shared prerequisite work
   - clean verification boundary
3. Record deferred issues explicitly.

### Phase 2: Draft the plan and task list (Planner)

The plan must include:
- objective
- selected issues
- excluded issues
- scope and non-goals
- risks
- verification
- rollback or recovery notes

The task list must:
- be ordered
- name the likely target files or modules
- include tests, docs, and memory work when needed

### Phase 3: Readiness checkpoint (Reporter)

After writing the artifacts:
1. echo the plan and task paths
2. summarize:
   - selected issues
   - proposed approach
   - biggest risk
   - verification plan

If the selected issue batch and plan are clear enough, continue.
If not, stop and resolve the blocker before continuing.

### Phase 4: Implement the approved batch (Implementer)

1. Fix the selected issues in plan order.
2. Keep diffs narrow and explainable.
3. If a selected issue proves materially broader than planned, escalate instead of freelancing.

If the touched scope accumulates obvious local mechanical complexity or dead scaffolding that can be fixed without broadening scope:
- use the `mechanical-cleanup` skill
- then continue to Phase 5

### Phase 5: Audit the touched area (Auditor)

Review:
- whether each selected issue is actually resolved
- nearby regression risks
- standards alignment
- whether any new out-of-scope issue should be logged

Fix small in-scope follow-on problems immediately.
Log larger out-of-scope problems instead of expanding the batch.

### Phase 6: Verify and close (Verifier + Reporter)

1. Run the best repo-defined verification command for the approved risk level.
2. Remove resolved issues from `ISSUES.md`.
3. Add any genuinely new out-of-scope issue entries.
4. Write `.agent-layer/tmp/fix-issues.<run-id>.report.md` with:
   - issues fixed
   - issues deferred or rejected
   - verification performed
   - remaining follow-up

When no broader orchestrator already owns closeout, use the `finish-task` skill here.
If it reveals incomplete issue resolution or stale memory/docs, jump back to the earliest affected phase.

## Guardrails

- Do not convert this into a general cleanup pass.
- Do not leave issue dispositions implicit.
- Do not weaken checks or lower thresholds to “finish” an issue batch.
- Do not close issues that were only partially addressed.

## Final handoff

After writing the report:
1. Echo the artifact paths.
2. Summarize the resolved issues and any deferred ones.
3. State the verification outcome clearly.
