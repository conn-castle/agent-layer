---
name: fully-implement-plan
description: >-
  Explicit-only.
  Implement supplied plan/task/context artifacts, review the changes, verify
  the contract, and repair material findings.
---

# fully-implement-plan

Implement an artifact contract, review and verify the delivery, and repair
material findings. Leave shipping and unrelated full-lane checks to callers.

## Inputs and boundaries

Require exact plan, task, and context artifact paths plus one explicit,
self-contained dispatch target specification for each of `implementer`,
`code_reviewer`, and `fixer`. Treat artifacts as the contract and validate
delegated evidence against the latest tree.

Write `.agent-layer/tmp/fully-implement-plan.<run-id>.report.md` and track its
contract obligations, findings, and evidence. Serialize mutations; do not
stage, commit, weaken checks, or destructively rewrite user changes.

If follow-up dispatches are required, pass artifact and report paths, changed
files, and unresolved finding IDs. Do not inline complete artifacts, reports,
command output, or resolved findings.

## Workflow

1. Dispatch `implementer` with `/implement-plan` and the supplied artifacts.
2. Dispatch `code_reviewer` with `/review-uncommitted-code` against the latest
   tree and supplied artifacts.
3. Send confirmed material in-scope findings to `fixer` in one consolidated
   batch with the plan artifacts. Require every finding to be resolved or
   disproven with evidence, including any required tests, documentation, and
   memory updates.
4. Rerun invalidated checks and targeted contract verification after changes.
   Repeat semantic review through a new dispatch to the supplied `code_reviewer`
   target only when a repair changed design, architecture, or contract scope.
5. Iterate as needed until the plan is fully implemented.

Return `blocked` only when recovery is exhausted and the remaining constraint
is external, missing authoritative contract input, unsafe overlap with user
work, or a genuine user decision.

## Completion contract

Return:

- `complete` when the final tree satisfies the contract and all confirmed
  in-scope findings are resolved or disproven with evidence
- `complete-with-follow-up` when only explicit out-of-contract work remains
- `blocked` for a named unresolved constraint

Include artifact paths, implementation and deviations, final checks, review,
verification, repairs, shipping obligations, report path, and residual risk.
