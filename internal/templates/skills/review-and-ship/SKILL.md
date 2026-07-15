---
name: review-and-ship
description: >-
  Explicit-only.
  Review and verify an existing local delivery, repair accepted findings, and
  ship it as a pull request without requiring implementation-plan artifacts.
---

# review-and-ship

Turn an existing local delivery into a reviewed, verified pull request. The
delivery may be working-tree changes or unpublished commits and does not need
plan artifacts when the user has supplied an authoritative contract.

Invoking this workflow authorizes the staging, commits, pushes, and pull-request
writes required by `/ship-pr`. Merge still requires explicit authorization for
the exact ready PR.

## Inputs and state

Require:

- a delivery target: working tree, named paths, commit range, or unpublished
  current-branch commits
- an authoritative contract: the explicit request or user-designated source
- `code_reviewer`: one explicit, self-contained semantic-review dispatch target

Before any side effect, show the user the exact `code_reviewer` target and ask
for it when missing; do not infer the role or target specification. Do not infer
intent from temporary artifacts. Supporting reports, commits, issues, and PR
text are not authoritative unless the user says so.

Maintain `.agent-layer/tmp/review-and-ship.<run-id>.report.md` with the contract,
delivery boundary and tree, checks, review and verification evidence, finding
dispositions, repairs, shipping handoff, and outcome. Reconcile delegated output
with the current tree, preserve usable evidence, and stop on a missing required
review verdict rather than substituting local work or an unspecified agent.

## Workflow

### 1. Establish the boundary and checks

Resolve the repository root, default base, branch and upstream, working-tree
state, and unpublished commits. Use the current checkout unless the user
requested isolation. Separate unrelated work with exact diff, path, or hunk
boundaries; never include or rewrite it. Block only when the delivery cannot be
separated safely or a material contract disagreement needs a user choice.

Run deterministic checks proportionate to the changed scope, risk, repository
guidance, and evidence needed for review. Accumulate independent failures before
mutation.

### 2. Review and verify

Dispatch `code_reviewer` with `/review-uncommitted-code` and run `/verify-work`
in a fresh built-in subagent against the same exact tree, concurrently when
useful. Give both the complete delivery and authoritative contract, but not each
other's findings. Validate and deduplicate results in one ledger: open,
resolved, rejected with evidence, deferred outside contract, or blocked.

### 3. Repair

If supported findings remain, repair compatible items in one bounded batch.
Continue independent repairs before asking about a genuine substantive choice.
Rerun checks and contract evidence invalidated by changed files. Repeat full
semantic review through a new dispatch to the supplied `code_reviewer` target
only when the repair changed design, architecture, or contract scope. Shipping
requires complete verification and no unresolved in-scope finding.

### 4. Ship

Run `/ship-pr` locally with the boundary, contract, current tree, check evidence,
review and verification reports, ledger, and remaining obligations. Record any
shipping mutation and return its exact merge-authorization request. Resume only
with the user's answer for that PR and head.

## Completion

Return the merge-authorization request, the merged PR with verified cleanup, or
a concrete blocker with the preserved delivery, evidence, and smallest next
decision. Never report success while required review, verification, repair,
feedback, shipping evidence, or merge authorization remains open.
