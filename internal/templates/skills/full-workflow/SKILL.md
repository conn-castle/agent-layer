---
name: full-workflow
description: >-
  Align a feature specification, produce a reviewed plan, complete the local
  work, and ship the pull request.
---

# full-workflow

Run the delivery workflow as the root-owned state machine. External dispatch is
reserved for bounded plan-review, implementation, and repair judgment. Do not
dispatch a workflow coordinator or relay another agent's checkpoints.

## Required inputs

- the user's requested work
- `implementer`: one dispatch target for the implementation leaf
- `fixer`: one dispatch target for bounded repair leaves
- `plan_reviewers`: exactly three dispatch target specifications

Require these inputs before side effects. Do not infer roles or target lists.

## Delivery context

Before issue selection or planning:

1. Resolve the repository identity and remote default branch from authoritative
   local Git or GitHub evidence. Use that default branch unless the user
   explicitly requested a stacked or non-default pull request. Ask only when
   authoritative evidence cannot identify the default.
2. Create one unique workflow-owned topic branch and worktree from the exact
   default-branch commit. Leave the caller checkout byte-for-byte and
   state-for-state unchanged.
3. Write `.agent-layer/tmp/full-workflow.<run-id>.manifest.json` atomically and
   validate it against `assets/facts-manifest.schema.json` before forwarding.
   This is the sole cross-stage state packet. Record repository/base/worktree,
   exact commit and tree identifiers, artifact paths and hashes, selected issue
   source hashes, command/timestamp/exit/output evidence, provider capability
   facts, and phase events.
4. Reject manifest keys or values that encode recommendations, risk labels,
   readiness, confidence, materiality, verdicts, summaries, or prior reviewer
   conclusions. Pass primary artifacts and mechanically verifiable facts only.
5. Retain the worktree while a pull request is open or merge authorization is
   pending. Remove only workflow-owned state after terminal shipping or an
   explicit abandonment instruction.

All subsequent repository work runs in the delivery worktree.

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
artifacts. Before semantic review, choose deterministic checks proportionate to
the changed scope, consequential risks, repository guidance, and evidence
needed to avoid wasting review. Do not use time budgets, historical durations,
mandatory tiers, or a universal full-lane rule.

Record each command, start/end timestamp, exit status, retained output path and
hash, and exact head/tree fingerprint. When focused equivalents do not exist,
use judgment about whether broader validation belongs here or at shipping.

### 3. Review, verify, and repair

After the deterministic gate passes, launch `/review-uncommitted-code` and
`/verify-work` concurrently as fresh built-in subagents against the same exact
head. Let independent safe checks finish and accumulate all supported failures.
Validate and deduplicate findings, then send one bounded repair package to
`fixer` only when confirmed open work exists.

After mutation, invalidate evidence by changed files and contracts. Rerun
affected focused checks and one targeted contract verification. Repeat a full
semantic review only when repair changed production design, architecture, or
contract scope. Record separate phase timing for implementation, deterministic
gate, review, verification, repair, and final verification; a quality stage
over twice implementation records an efficiency investigation signal without
weakening gates.

### 4. Ship through the canonical contract

Continue with `/ship-pr` locally. Pass the delivery boundary, authoritative
artifacts, current tree fingerprint, review and verification reports, finding
ledger, check evidence, remaining shipping obligations, and the caller-checkout
invariant. Consume its merge-authorization, merged, or blocker result without
reimplementing its shipping, monitoring, feedback, merge, or cleanup procedure.
Return an authorization request to the user and resume the same `/ship-pr`
continuation only with the exact answer; stop on a blocker.

## Independence and waiting contract

Use one synchronous `al dispatch fanout` for the three plan reviewers and one
blocking invocation for each ordinary leaf. Do not poll `al dispatch inspect`
to wait. A chat host may yield a long-running terminal process; that host-level
wake-up behavior is outside the command-line interface contract.

Never forward another reviewer's findings, planner recommendations, inferred
risk/readiness judgments, scores, or synthesis into an independent review.
Only root review-plan synthesizes across external reports.

## Completion contract

Complete means the final tree satisfies the approved artifacts and `/ship-pr`
has returned a merged result with verified cleanup. Return artifact paths,
factual manifest path, `/ship-pr` result, phase timing, and any concrete
blocker.
