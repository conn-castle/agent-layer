---
name: full-workflow
description: >-
  Align a feature specification, produce a reviewed plan, complete the local
  work, and ship the pull request.
---

# full-workflow

Own a delivery from clarified intent through a merged pull request. Delegate
bounded leaves to the supplied role targets, but keep workflow state and user
checkpoints in the owning context.

## Inputs

Require:

- the user's requested work
- `implementer`: one explicit, self-contained dispatch target for implementation
- `code_reviewer`: one explicit, self-contained semantic-review dispatch target
- `fixer`: one explicit, self-contained dispatch target for bounded repairs
- `plan_reviewers`: exactly three self-contained dispatch target specifications

Before any side effect, show the user the exact role-to-target mapping and every
plan-reviewer target specification. Ask for any missing target; do not infer
roles or target specifications.

Invoking this workflow authorizes the normal branch, commit, push, and PR writes
needed by `/ship-pr`. Merging still requires `/ship-pr`'s exact authorization
gate.

## Delivery boundary

Resolve the repository root, remote default branch, current branch, and working
tree from local Git or GitHub evidence. Use the current checkout and default
base unless the user requested another base or an isolated worktree. Preserve
unrelated work with explicit diff boundaries and path- or hunk-specific staging;
block only when overlapping work cannot be separated safely.

For multi-issue work, select live, compatible items from the authorized set.
Already-fixed or tracker-only cleanup does not count, but do not impose a
minimum batch size. Keep required memory-file alignment in the delivery.

Treat delegated reports as evidence. Validate them against the artifacts and
tree and preserve valid work. Retry the same supplied target when evidence
supports it; otherwise report the concrete delegation blocker rather than
substituting local work or an unspecified agent.

## Workflow

### 1. Align and plan

Write `.agent-layer/tmp/full-workflow.<run-id>.spec.md` with objective, scope,
non-goals, constraints, acceptance criteria, shipping expectations, settled
decisions, and any remaining user-owned choice. Resolve factual unknowns before
planning, then run `/plan-work` with the spec and `plan_reviewers`. Continue
with its implementation-ready artifacts; validate any reported user blocker
against repository escalation rules.

### 2. Implement and establish evidence

Dispatch `implementer` with `/implement-plan` and the complete reviewed
artifacts. Reconcile its result with the plan and redispatch only for remaining
in-scope implementation work. Choose deterministic checks from repository
guidance, changed scope, and consequential risk; record the commands, results,
and tree they cover.

### 3. Review, verify, and repair

After focused checks pass, run `/verify-work` in a fresh built-in subagent and
dispatch `code_reviewer` with `/review-uncommitted-code` against the same
delivery tree, concurrently when useful. Give each the authoritative contract,
not the other's conclusions. Validate and deduplicate supported findings.

Dispatch `fixer` with compatible open findings in one bounded batch. Record
dispositions and rerun checks or contract verification invalidated by the
repair. Repeat a full semantic review through a new dispatch to the supplied
`code_reviewer` target only when the repair materially changed design,
architecture, or contract scope. Continue until verification is complete and
every in-scope finding is resolved or rejected with evidence; ask the user only
for a genuine substantive decision with no safe in-scope resolution.

### 4. Ship

Continue locally with `/ship-pr`, passing the delivery boundary, authoritative
artifacts, current tree, review and verification evidence, finding dispositions,
and remaining obligations. Return its exact merge-authorization request when
required, then resume only with the user's answer.

## Completion

Complete only when the approved contract is satisfied and `/ship-pr` reports a
merged PR with verified cleanup. Return the artifact paths, shipping result, or
a concrete blocker and smallest required decision.
