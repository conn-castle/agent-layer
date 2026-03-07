---
name: fix-issues
description: >-
  Fix all open issues in `ISSUES.md` by default: plan the full set, batch into
  coherent groups for execution, implement, audit, verify, and keep `ISSUES.md`
  current throughout. Narrow the batch only when the user explicitly limits
  scope.
---

# fix-issues

This is the issue-ledger maintenance workflow.
It should:
- fix all open issues by default
- organize them into coherent execution batches
- plan the work explicitly
- stop only when scope or behavior is not clear enough to proceed safely
- implement, audit, verify, and close each batch until all issues are resolved

## Defaults

- Default mode is plan-first unless the surrounding request clearly includes execution and the selected issue batch is unambiguous.
- Default issue set is all open issues in `ISSUES.md`. Narrow the set only when the user explicitly specifies issue IDs, a count, or a scope limit.
- When the full set is large, organize issues into coherent execution batches (by shared area, shared prerequisites, or clean verification boundary) and work through all batches sequentially.
- Default file-touch budget is reviewable per batch, not exhaustive.
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
- whether this run is plan-only or allowed to execute after readiness gating
- a verification depth preference
- a scope preference such as targeted or all-selected

## Multi-agent pattern

Use subagents liberally when available.

Recommended roles:
1. `Issue triage lead`: selects the issue batch.
2. `Planner`: drafts the plan and task artifacts.
3. `Execution gatekeeper`: decides whether the current batch should `proceed`, `revise`, `escalate`, or `rewrite-because-out-of-scope`.
4. `Implementer`: owns the code changes.
5. `Auditor`: reviews touched areas for regressions and missed fixes.
6. `Verifier`: runs the repo-defined checks.
7. `Reporter`: writes the final resolution report.

## Global constraints

- Keep scope to the selected issue batch plus directly blocking prerequisites.
- Do not invent missing issue details; treat ambiguous issues as blockers to clarify.
- Keep changes reviewable and aligned with repo conventions.
- Remove resolved issues from `ISSUES.md`; log newly discovered out-of-scope problems instead of silently expanding scope.
- If new evidence invalidates the plan, jump back to the earliest affected phase instead of continuing on stale assumptions.
- Treat readiness gating as an internal execution decision, not as a reason to ask the user unless a human checkpoint is actually triggered.

## Human checkpoints

- Required: ask when fixing a selected issue would require a breaking change, broad architectural refactor, or materially larger scope.
- Required: ask when issue wording and repo evidence still leave the intended fix ambiguous after planning and gating.
- Stay autonomous while planning, gating, implementing, auditing, and verifying a clear issue batch.

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

### Phase 1: Select and batch all issues (Issue triage lead)

1. If the user specified issue IDs or a count, use that as the issue set.
2. Otherwise select all open issues from `ISSUES.md`.
3. For each issue, decide whether it is actually an issue or a misplaced backlog feature:
   - If an issue describes a new user-visible capability rather than a bug, defect, debt, or risk, move it to `BACKLOG.md` and remove it from `ISSUES.md`.
   - Record every reclassification in the report.
4. Organize the remaining issue set into coherent execution batches by:
   - shared area or module
   - shared prerequisite work
   - clean verification boundary
5. Order the batches so that prerequisite fixes land first.
6. State the full issue set, any reclassifications, the batch breakdown, and the execution order before proceeding.

### Phase 2: Draft the plan and task list (Planner)

Draft a plan and task list following the `plan-work` skill's artifact format. The plan must also include: selected issues, excluded issues, and rollback or recovery notes.

### Phase 3: Gate the current issue batch (Execution gatekeeper + Reporter)

After writing the artifacts:
1. echo the plan and task paths
2. summarize the selected issues, proposed approach, biggest risk, and verification plan
3. choose exactly one verdict:

- `proceed` (batch ready to implement): continue to Phase 4.
- `revise` (plan or task list needs updates): repeat from Phase 2.
- `escalate` (human checkpoint required): ask the smallest question that unblocks a trustworthy fix.
- `rewrite-because-out-of-scope` (batch too broad): rewrite to the largest still-in-scope subset, record deferred issues, and return to the earliest affected phase.

### Phase 4: Implement the current batch (Implementer)

1. Fix the selected issues in plan order.
2. Keep diffs narrow and explainable.
3. If a selected issue proves materially broader than planned, hand it back to the execution gatekeeper instead of freelancing.

If the touched scope accumulates obvious local complexity or dead scaffolding that can be fixed without broadening scope:
- use the `simplify-code` skill
- then continue to Phase 5

### Phase 5: Audit the touched area (Auditor)

Review:
- whether each selected issue is actually resolved
- nearby regression risks
- standards alignment
- whether any new out-of-scope issue should be logged

Fix small in-scope follow-on problems immediately.
Log larger out-of-scope problems instead of expanding the batch.

### Phase 6: Verify and close the current batch (Verifier + Reporter)

1. Run the best repo-defined verification command for the selected risk level.
2. Remove resolved issues from `ISSUES.md`.
3. Add any genuinely new out-of-scope issue entries.
4. Update the report at `.agent-layer/tmp/fix-issues.<run-id>.report.md` with the batch results.

### Phase 7: Advance to the next batch or close the run (Issue triage lead + Reporter)

If unprocessed batches remain:
1. Select the next batch from Phase 1's ordering.
2. Return to Phase 2 to plan the next batch.

If all batches are complete:
1. Finalize `.agent-layer/tmp/fix-issues.<run-id>.report.md` with:
   - all issues fixed (by batch)
   - issues reclassified and moved to `BACKLOG.md`
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
- Do not treat `rewrite-because-out-of-scope` as permission to silently drop selected issues; record the deferrals explicitly.
- Do not stop after the first batch if unprocessed batches remain.

## Final handoff

After writing the report:
1. Echo the artifact paths.
2. Summarize the resolved issues and any deferred ones.
3. State the verification outcome clearly.
