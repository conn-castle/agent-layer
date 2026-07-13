---
name: fully-implement-plan
description: >-
  Implement supplied plan/task/context artifacts, review the changes, verify
  the contract, and repair material findings.
---

# fully-implement-plan

Implement an artifact contract, review and verify the delivery, and repair
material findings. Leave shipping and unrelated full-lane checks to callers.

## Inputs and boundaries

Require exact plan, task, and context artifact paths. Accept optional
implementer, reviewer, and fixer roles; otherwise work locally or delegate as
useful. Treat artifacts as the contract and validate delegated evidence against
the latest tree.

Write `.agent-layer/tmp/fully-implement-plan.<run-id>.report.md` and track its
contract obligations, findings, and evidence. Serialize mutations; do not
stage, commit, weaken checks, or destructively rewrite user changes.

Recover, replace, or complete incomplete delegated work. Agent failure alone is
not a reason to escalate.

## Workflow

1. Run `/implement-plan` with the supplied artifacts. Record its report,
   deviations, checks, remaining work, and readiness.
2. Run checks proportionate to changed scope and risk. Use focused checks by
   default and the documented full lane when it is the credible evidence.
3. Against the same tree, run independent `/verify-work` and
   `/review-uncommitted-code` passes, concurrently when practical. Treat
   `complete-with-follow-up` as complete only for out-of-contract follow-up.
4. Validate and deduplicate findings. Mark each `open`, `resolved`,
   `invalid-with-evidence`, or `blocked`. Repair open in-scope findings,
   including required tests, documentation, and memory.
5. Rerun invalidated checks and targeted contract verification after changes.
   Repeat semantic review only when a repair changed design, architecture, or
   contract scope.

Continue through safe in-scope repairs. Return `blocked` only when recovery is
exhausted and the remaining constraint is external, missing authoritative
contract input, unsafe overlap with user work, or a genuine user decision.

## Completion contract

Return:

- `complete` when the final tree satisfies the contract and all confirmed
  in-scope findings are resolved or disproven with evidence
- `complete-with-follow-up` when only explicit out-of-contract work remains
- `blocked` for a named unresolved constraint

Include artifact paths, implementation and deviations, final checks, review,
verification, repairs, shipping obligations, report path, and residual risk.
