---
name: fully-implement-plan
description: >-
  Implement a supplied plan, clean the resulting working tree, verify the
  original contract once, and directly address material verification findings.
---

# fully-implement-plan

Coordinate implementation, cleanup, verification, and any required verification
repairs without opening or shipping a pull request or running an unrelated
full-repository test lane.

## Required inputs

- plan artifact path
- task artifact path
- context artifact path
- `implementer`: dispatch agent role for implementation and repair
- `fixer`: dispatch agent role for `/loop-clean-and-fix`

Require every input before side effects. Do not infer roles or artifact paths.

## Output artifact

Write `.agent-layer/tmp/fully-implement-plan.<run-id>.report.md` using
`run-id = YYYYMMDD-HHMMSS-<short-rand>`.

## Rules

- Dispatch external roles through `/agent-dispatch`.
- Treat the supplied plan, task, and context as the authoritative contract.
  Delegated reports are evidence; cleanup findings are supplemental verification
  obligations.
- Run implementation, cleanup, and repair mutations sequentially against the
  latest working tree.
- Stop and report a concrete blocker when a delegated call fails or omits its
  required report or verdict.
- Ask the user only when a repair requires a choice that materially affects
  behavior, architecture, scope, risk, or cost.
- Do not stage, commit, discard, or destructively rewrite changes without the
  user's explicit request.
- Do not weaken or skip required checks.

## Workflow

### 1. Implement the supplied plan

Dispatch `implementer` with `/implement-plan` and the supplied plan, task, and
context paths. Record its report, deviations, task-local checks, remaining work,
and readiness for verification.

### 2. Clean and fix the working tree

Dispatch `fixer` with `/loop-clean-and-fix`. Record its outcome, reports,
`resolved_findings`, focused evidence, and residual risk.

### 3. Verify once

Run `/verify-work` once in a fresh built-in subagent against the original plan,
task, and context. Pass cleanup `resolved_findings` as supplemental obligations.
Record the report path, verdict, material findings, evidence, and recommended
next step.

Treat `complete-with-follow-up` as complete only when every follow-up is outside
the supplied contract. Otherwise address it as an incomplete result.

### 4. Address verification findings

When verification is incomplete, validate every material in-scope finding
against the contract and current tree. Discard a finding only when repository
evidence shows it is invalid, and record that evidence.

If no user-owned decision blocks repair, dispatch `implementer` once with:

- the original artifact paths
- the verification report
- every confirmed in-scope finding
- the requirement to repair each root cause directly
- any directly required test, documentation, or memory updates
- the narrowest credible checks for the repaired behavior
- `.agent-layer/tmp/fully-implement-plan.<run-id>.verification-repair.report.md`
  as the required report path

The repair report must account for every verification finding as `resolved`,
`invalid-with-evidence`, or `blocked`, and include focused check results plus a
final diff assessment. Do not create another plan, rerun `/implement-plan`, run
cleanup again, or rerun `/verify-work` merely to gain confidence.

### 5. Report

Write the final report with:

1. `# Fully Implement Plan Summary`
2. `## Inputs`
3. `## Implementation Result`
4. `## Cleanup Result`
5. `## Verification Result`
6. `## Verification Finding Repairs`
7. `## Final Status`
8. `## Residual Risk`

Use one final status:

- `complete`: verification passed, or all material in-scope verification
  findings were subsequently resolved with focused evidence
- `complete-with-follow-up`: only explicitly out-of-contract work remains
- `blocked`: name the concrete failure or user-owned decision

## Completion contract

The report must account for the original implementation, cleanup obligations,
verification verdict, every material verification finding, repair evidence, and
remaining risk. Return the report path, final status, key fixes, checks, and any
blocker or follow-up.
