---
name: full-workflow
description: >-
  Align a feature specification, produce a reviewed plan, complete the local
  work, and ship the pull request.
---

# full-workflow

Run the delivery workflow as the root-owned state machine. Dispatch only bounded
leaf judgment; do not dispatch a workflow coordinator or relay another agent's
checkpoints.

## Required inputs

- the user's requested work
- `implementer`: one dispatch target for the implementation leaf
- `code_reviewer`: one semantic code-review target whose provider differs from
  both `implementer` and the root session; reject a target that does not satisfy
  this required diversity
- `fixer`: one dispatch target for bounded repair leaves
- `plan_reviewers`: exactly three dispatch target specifications

Require these inputs before side effects. Do not infer roles or target lists.

## Delivery context

Before issue selection or planning:

1. Resolve the repository identity and remote default branch from authoritative
   local Git or GitHub evidence. Use that default branch unless the user
   explicitly requested a stacked or non-default pull request. Ask only when
   authoritative evidence cannot identify the default.
2. Use the current resolved repository-root checkout by default. Create and use
   a linked Git worktree only when the user explicitly requests worktree
   isolation. Before mutation, record the active checkout's branch, head, tree,
   staged diff, unstaged diff, and deterministic untracked state covering each
   path, file kind, and content. Preserve this initial inventory as the boundary
   between delivery changes and unrelated user work.
3. When shipping requires a topic branch, create one unique workflow-owned
   topic branch in the active checkout. Do not change branches while the
   checkout has state that Git cannot carry safely. Stop only when attempted
   path-specific staging and exact diff boundaries still cannot separate
   delivery changes from unrelated user work, and name the overlapping paths.
4. Write `.agent-layer/tmp/full-workflow.<run-id>.manifest.json` atomically and
   validate it against `assets/facts-manifest.schema.json` before forwarding.
   This is the sole cross-stage state packet. Record repository/base/current
   checkout facts, exact commit and tree identifiers, artifact paths and hashes,
   selected issue source hashes, command/timestamp/exit/output evidence,
   provider capability facts, and phase events.
5. Reject manifest keys or values that encode recommendations, risk labels,
   readiness, confidence, materiality, verdicts, summaries, or prior reviewer
   conclusions. Pass primary artifacts and mechanically verifiable facts only.
6. Serialize every working-tree mutator. Before reusing evidence or starting a
   mutator, compare the current head, tree, staged-diff hash, unstaged-diff
   hash, and untracked-state hash with the recorded boundary or latest evidence.
   Any changed value invalidates affected evidence and must be reconciled and
   recorded before work continues.

For an explicitly requested linked worktree, record its path and workflow
ownership in a factual JSON artifact referenced by the manifest's `artifacts`
array with `kind: explicit_worktree_facts`; `repository.root` remains the
canonical checkout location.

## Scope admission

For a requested multi-issue workflow, evaluate candidates against the delivery
base before planning. Already-fixed entries and tracker-only cleanup do not
count. Require at least two live, compatible, substantive issues. Reselect only
within the authorized issue set; if fewer than two remain, stop before planning
and report the scope collapse. Keep required ISSUES.md, BACKLOG.md, ROADMAP.md,
and other aligned memory changes in the delivery diff through shipping.

## Workflow

### 1. Align and plan locally

Write `.agent-layer/tmp/full-workflow.<run-id>.spec.md` with objective, scope,
non-goals, material decisions, constraints, acceptance criteria, shipping
expectations, and unresolved user decisions. Resolve repository facts directly.
Ask only about genuine alternatives with materially different behavior,
architecture, scope, risk, cost, or irreversible effects.

Run `/plan-work` locally with the spec and `plan_reviewers`. Consume its returned
artifact paths and continue only when its status is `implementation-ready`. For
`blocked-for-user-decision`, ask its named question and resume the same planning
stage with the answer. Missing artifacts or any other result block delivery.

### 2. Implement and establish focused evidence

Dispatch `implementer` once with `/implement-plan` and the complete reviewed
artifacts. Continue only when its report says ready for verification and no
required planned work remains. Ask a named user question and resume this phase
when that is the reported blocker; stop on a missing report or any other
blocker. Before semantic review, choose deterministic checks proportionate to
the changed scope, consequential risks, repository guidance, and evidence
needed to avoid wasting review. Do not use time budgets, historical durations,
mandatory tiers, or a universal full-lane rule.

Record each command, start/end timestamp, exit status, retained output path and
hash, and exact head/tree fingerprint. When focused equivalents do not exist,
use judgment about whether broader validation belongs here or at shipping.

### 3. Review, verify, and repair

After the deterministic gate passes, start `/verify-work` as a fresh built-in
subagent and dispatch `code_reviewer` once with `/review-uncommitted-code`, the
delivery diff boundary, and the authoritative contract, both against the same
exact head. Run them concurrently when the host supports a background leaf and
give each only primary artifacts, not the other's findings. Accumulate all
supported failures, validate and deduplicate them, then send one bounded repair
package to `fixer` only when confirmed open work exists. Accept review readiness
`proceed` or `proceed-after-fixes`. For `revise-first`, ask its named question
and resume this phase with the answer, or stop on unavailable evidence. Accept
verification `complete`, or `complete-with-follow-up` only when the follow-up is
outside the contract; treat `incomplete` items as repair candidates. Stop on a
failed leaf or missing report.

Give `fixer` the authoritative artifacts, delivery boundary, open findings,
current evidence, required checks, and a unique
`.agent-layer/tmp/full-workflow.<run-id>.repair.<repair-id>.report.md`. Require
finding dispositions, focused checks, and the final fingerprint. Ask any named
user question and resume this phase; stop on a failed or missing repair report.

After mutation, invalidate evidence by changed files and contracts. Rerun
affected focused checks and one targeted contract verification. Repeat a full
semantic review, through a fresh `code_reviewer` dispatch, only when repair
changed production design, architecture, or contract scope. Stop only when a
confirmed in-scope finding remains open with no newly evidenced repair path.

### 4. Ship through the canonical contract

Continue with `/ship-pr` locally. Pass the delivery boundary, authoritative
artifacts, current tree fingerprint, review and verification reports, finding
ledger, check evidence, remaining shipping obligations, and initial checkout
inventory. Include the explicit worktree facts artifact when one exists.
Consume its merge-authorization, merged, or blocker result without
reimplementing its procedure.
Return an authorization request to the user and resume the same `/ship-pr`
continuation only with the exact answer; stop on a blocker.

## Completion contract

Complete means the final tree satisfies the approved artifacts and `/ship-pr`
has returned a merged result with verified cleanup. Return artifact paths,
factual manifest path, `/ship-pr` result, and any concrete blocker.
