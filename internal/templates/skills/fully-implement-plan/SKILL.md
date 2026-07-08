---
name: fully-implement-plan
description: >-
  Implement a supplied plan, iteratively review and fix until diminishing
  returns, verify completion, and run the repository's documented checks. Use
  when the caller provides plan, task, and context artifacts and wants the local
  work finished without opening or shipping a PR.
---

# fully-implement-plan

Orchestrate lower-level skills and keep one skill-level report. Do not do
delegated skill work yourself.

## Required inputs

Fail before side effects unless all are present:

- plan artifact path
- task artifact path
- context artifact path
- `review_agents`: one or more dispatch agent roles to pass to
  `/loop-clean-and-fix`, `/run-and-fix-checks`, and any `/plan-work` retry for
  verification gaps

If any required input is missing, ask for it before starting. Do not invent
defaults or auto-select artifacts from `.agent-layer/tmp/`.

## Required artifacts

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`. Write:

- `.agent-layer/tmp/fully-implement-plan.<run-id>.report.md`

Create the report before writing. It is the skill-level ledger for all delegated skill
results.

## Context Discipline

You are the orchestrator for this skill. Do not do work that belongs to
subagents or delegated skills in the orchestration context. Preserve your
context to make strategic decisions, enforce gates, reconcile returned outputs,
and continue this skill's workflow after every delegation returns.

## Rules

- Do not ask the user to confirm target files before starting; required
  artifact paths and `review_agents` are enough.
- Treat delegated skill returns as intermediate until this workflow reaches its
  final report.
- If `/implement-plan`, `/loop-clean-and-fix`, `/verify-work`, or
  `/run-and-fix-checks` fails, stops at its own checkpoint, emits unusable
  output, or cannot provide the report path or verdict this workflow needs,
  stop this workflow, record the stop reason, and surface it to the user.
- If `/verify-work` returns `incomplete`, run `/plan-work` with the
  verification report as the task source and the original plan, task, and
  context paths as source evidence. Then run `/implement-plan` with the new
  remaining-work plan, task, and context artifacts. Repeat cleanup and verify
  again against the original plan, task, and context. If the same gap recurs
  after two remaining-work implementation attempts, stop and ask.
- Accept `complete-with-follow-up` only when every follow-up is clearly outside
  the supplied plan and task list.
- Do not stage, commit, discard, or destructively rewrite changes unless the
  user explicitly asks.
- Do not skip, disable, weaken, or lower thresholds for checks or tests.

## Workflow

1. Run `/implement-plan` with the plan, task, and context artifact paths.
   Record its report path, deviations, checks, and remaining follow-up.
2. Run `/loop-clean-and-fix` with `review_agents`. Its input is the full
   uncommitted working tree; record its report path, round count, stop reason,
   issue ledger, `resolved_findings`, and any blocker or residual risk.
3. Run `/verify-work` with the plan, task, and context artifact paths. Treat
   delegated skill reports as evidence, not contract artifacts. Record its report path,
   verdict, findings, and recommended next step. If the verdict is
   `incomplete`, follow the retry rule in `Rules` and record the remaining-work
   plan, task, context, implementation report, and verification report paths.
4. Run `/run-and-fix-checks` with `review_agents`. Record the checks report
   path, commands, round count, repair cycle count, stop reason, and final
   passing evidence or blocker.
5. Write the final report and prepare the final message for the user.

## Required master report structure

Write `.agent-layer/tmp/fully-implement-plan.<run-id>.report.md` with:

1. `# Fully Implement Plan Summary`
2. `## Inputs`
3. `## Implementation Attempts`
4. `## Cleanup Rounds`
5. `## Issue Ledger`
6. `## Resolved Findings`
7. `## Verification Results`
8. `## Check Results`
9. `## Stop Reason`
10. `## Residual Risk`

In `## Issue Ledger`, include one Markdown table row for every issue reported by
`/loop-clean-and-fix` and `/run-and-fix-checks` repair cycles when available.

Required columns:

`| Source | Round | Severity | Classification | Issue | Location | Outcome |`

Use the delegated skill's classification when available, such as accepted,
resolved, rejected, deferred, already-resolved, blocker, or unclassified. If no
issues were reported, include a single `No issues reported` row.

## Definition of done

- The skill-level report exists and records every delegated report path, stop reason,
  issue ledger, and residual risk needed to understand the outcome.
- `/implement-plan` and `/loop-clean-and-fix` ran, or the report names the
  delegated-skill blocker.
- `/verify-work` reached `complete` or acceptable `complete-with-follow-up`, or
  the report names the verification blocker.
- `/run-and-fix-checks` ran the full documented check lane and either passed or
  reported a real blocker.

## Final handoff

After the run, present the results to the user in chat so that implementation,
cleanup, verification, and check outcomes are clearly attributed to the step
that produced them.

Required chat output:

1. Echo the fully-implement-plan report path.
2. State total implementation attempts, cleanup rounds, verification verdict,
   check stop reason, and final status.
3. Present a **Key fixes applied** table sorted by source, round, then
   severity. Example columns: `| Source | Round | Severity | Fix | Files |`.
   If no fixes were applied, still print the table with a single
   `No fixes applied` row.
4. List rejected, deferred, blocking, or repeated findings with their source and
   round numbers.
5. State which cleanup round or check round stopped the loop and whether final
   checks passed.
6. Name any blocker or residual risk.
