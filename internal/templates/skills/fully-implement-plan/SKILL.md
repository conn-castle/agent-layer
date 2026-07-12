---
name: fully-implement-plan
description: >-
  Implement supplied plan/task/context artifacts, review the changes, verify
  the contract, and repair material findings.
---

# fully-implement-plan

Coordinate implementation, code review, contract verification, and required
repairs. Do not open a PR or run an unrelated full-repository lane.

## Inputs and artifact

Require exact plan, task, and context artifact paths plus `implementer` and
`fixer` dispatch roles. Do not infer them. Write
`.agent-layer/tmp/fully-implement-plan.<run-id>.report.md`.

Dispatch external roles through `/agent-dispatch`. Treat the supplied artifacts
as the contract and delegated reports as evidence. Serialize mutations against
the latest tree and stop on a failed delegation or missing required verdict.
Keep a findings and evidence ledger in the workflow report, tied to the contract
version and covered tree.

Continue through reversible in-scope repairs and non-destructive checks. Ask
only for external writes not already authorized, destructive actions,
substantive product/architecture choices, or material scope expansion. Do not
stage, commit, weaken checks, or destructively rewrite changes.

Run risk-appropriate checks, including the full lane when it supports the
uncommitted tree. When an exact required lane is clean-revision-only, run its
independently runnable substantive components and pass the exact lane to
`/ship-pr` as a shipping obligation.

## Workflow

### 1. Implement

Dispatch `implementer` with `/implement-plan` and the artifacts. Record its
report, deviations, checks, remaining work, and readiness.

### 2. Establish completion evidence

Run `/review-uncommitted-code` in a fresh built-in subagent against the complete
change. Record its report, readiness, and accepted or deferred findings.

Then run `/verify-work` in another fresh built-in subagent against the
original artifacts. Pass accepted review findings as supplemental obligations
and any clean-revision-only lane as a shipping obligation. Record its verdict,
material findings, evidence, shipping obligations, and next step. Treat
`complete-with-follow-up` as complete only when all follow-up is outside the
contract.

### 3. Reconcile findings and evidence

Validate and deduplicate accepted review findings and material verification
findings against the contract and current tree. Record each as `open`,
`resolved`, `invalid-with-evidence`, or `blocked`. Do not dispatch `fixer` when
no confirmed finding remains open. If no genuine user decision blocks repair,
dispatch `fixer` with the original artifacts, current evidence, open findings,
required checks, and a unique
`.agent-layer/tmp/fully-implement-plan.<run-id>.repair.<repair-id>.report.md`.

The fixer owns that repair set and returns finding dispositions, focused checks,
and a final diff assessment. The orchestrator validates its evidence and owns
the durable ledger and completion verdict.

After a mutation, reassess the findings ledger and evidence invalidated by the
changed tree. Run focused checks for the affected behavior and reacquire review
or contract coverage only where the mutation invalidated it. Dispatch another
repair set when confirmed open findings remain and evidence supports a safe
repair; do not repeat unchanged work for confidence. On an evidence-equivalent
failure, revise the causal model or instrument it. Stop when no safe in-scope
repair remains or a genuine user decision is required.

## Completion contract

Report inputs, implementation, review, verification, repairs, final evidence,
shipping obligations, and residual risk. Return:

- `complete`: current evidence verifies the final tree against the contract and
  every confirmed in-scope finding is resolved or invalid with evidence
- `complete-with-follow-up`: only explicit out-of-contract work remains
- `blocked`: name the concrete failure or genuine user decision
