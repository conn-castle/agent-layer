---
name: fully-implement-plan
description: >-
  Implement supplied plan/task/context artifacts, review the changes, verify
  the contract, and repair material findings.
---

# fully-implement-plan

Run implementation, code review, contract verification, and required repairs
as a root-owned local procedure. External dispatch is limited to the bounded
implementer and fixer leaves. Do not open a PR or run an unrelated full lane.

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

Before semantic review, choose deterministic checks proportionate to changed
scope, consequential risks, repository guidance, and the evidence needed to
avoid wasting review. Do not use time budgets, historical duration rules,
universal cutoffs, or mandatory tiers. The full lane normally remains a final
shipping-head obligation, but may run here when it is the sensible evidence.

## Workflow

### 1. Implement

Dispatch `implementer` with `/implement-plan` and the artifacts. Record its
report, deviations, checks, remaining work, and readiness.

### 2. Establish completion evidence

After the focused deterministic gate passes, start
`/review-uncommitted-code` and `/verify-work` concurrently in fresh built-in
subagents against the same exact head. Let independent safe checks complete so
all failures can be accumulated before repair. Record each report, reviewed
head, findings, evidence, shipping obligations, and verdict. Treat
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

After mutation, identify evidence invalidated by changed files and contracts.
Rerun affected focused checks and one targeted contract verification. Repeat a
full independent semantic review only when the repair changed production
design, architecture, or contract scope. Dispatch another repair set only for
newly evidenced open findings; do not repeat unchanged work for confidence.
Record phase timings and flag a quality stage over twice initial implementation
for investigation without weakening gates.

## Completion contract

Report inputs, implementation, review, verification, repairs, final evidence,
shipping obligations, and residual risk. Return:

- `complete`: current evidence verifies the final tree against the contract and
  every confirmed in-scope finding is resolved or invalid with evidence
- `complete-with-follow-up`: only explicit out-of-contract work remains
- `blocked`: name the concrete failure or genuine user decision
