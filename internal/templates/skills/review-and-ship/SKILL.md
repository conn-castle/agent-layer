---
name: review-and-ship
description: >-
  Review and verify an existing local delivery, repair accepted findings, and
  ship it as a pull request without requiring implementation-plan artifacts.
---

# review-and-ship

Turn an existing local delivery into a reviewed, verified pull request. The
changes may come from manual work, another agent, unpublished branch commits,
or an implementation workflow. Do not require plan/task/context artifacts when
the authoritative contract is available from the explicit user request or
another user-designated source.

This is a root-owned procedure, not a relay skill. Run review, verification,
repair, and `/ship-pr` locally. Never dispatch another coordinator. Invoking
this skill authorizes the staging, commits, push, and pull-request operations
normally required by `/ship-pr`; merge still requires explicit authorization
for the exact ready pull request.

## Required inputs and artifacts

Require both:

- a concrete delivery target: the current working tree, a named file/directory
  scope, a commit range, or unpublished commits on the current branch
- an authoritative contract: the explicit user request/scope, exact artifacts
  named by the user, or another user-designated source

Do not discover planning artifacts from `.agent-layer/tmp/`. Existing reports,
commit messages, issue bodies, and pull-request descriptions are supporting
evidence unless the user explicitly designates one as the contract. If intent
cannot be established without choosing behavior or scope, stop for the
smallest user decision.

Write `.agent-layer/tmp/review-and-ship.<run-id>.report.md` with the contract,
delivery scope, exact reviewed tree fingerprint, check evidence, review and
verification reports, finding dispositions, repairs, invalidated evidence,
shipping handoff, and final outcome.

## Workflow

### 1. Establish the delivery boundary

Resolve the repository root, default base, branch/upstream, worktrees, staged,
unstaged, untracked, and unpublished-commit state. Map the intended delivery to
the authoritative contract and identify unrelated user work without modifying
it. Do not silently include unrelated changes, rewrite user commits, or create
an empty delivery. Stop when the delivery cannot be isolated safely or when
the contract and diff materially disagree.

Read repository guidance and select deterministic checks proportionate to the
changed scope, consequential risks, and evidence needed before semantic review.
Run relevant formatting/generation, compile/typecheck, focused lint, and
focused tests against one exact tree. Do not impose time budgets, mandatory
check tiers, or an automatic full-lane rule. Accumulate independent safe
failures before mutation.

### 2. Review and verify concurrently

After the deterministic gate can support meaningful review, start
`/review-uncommitted-code` and `/verify-work` concurrently in fresh built-in
subagents against the same exact tree fingerprint:

- give `/review-uncommitted-code` the complete delivery target and authoritative
  contract
- give `/verify-work` the explicit contract/scope and any supplemental shipping
  obligations; do not ask it to discover temporary artifacts

Let both read-only stages finish. Validate every candidate against the current
tree, deduplicate overlap, and maintain one disposition ledger with `open`,
`resolved`, `invalid-with-evidence`, `deferred`, or `blocked`. A review
`Recommended Accept` or material incomplete verification item is open only
when current evidence supports it and no user-owned decision is required.

### 3. Repair one bounded batch

If no finding remains open, do not create a confidence repair. Otherwise apply
all compatible accepted findings in one root-owned repair batch while
serializing working-tree mutations. Stop before changing behavior, architecture,
scope, or data semantics that require a user decision.

After mutation, determine which evidence was invalidated by the changed files
and contracts. Rerun affected focused checks and one targeted contract
verification. Repeat a full independent semantic review only when the repair
changed production design, architecture, or contract scope. Continue only when
every in-scope finding is resolved or invalid with evidence and verification
reports `complete` or `complete-with-follow-up` whose follow-up is outside the
delivery contract.

### 4. Ship the reviewed delivery

Continue with `/ship-pr` as the local root-owned shipping procedure. Pass the
delivery boundary, authoritative contract, current tree fingerprint, review and
verification reports, finding ledger, check evidence, and remaining shipping
obligations. Do not re-run current evidence for confidence; let `/ship-pr`
acquire the full-lane and remote evidence required for the pushed head.

Retain `/ship-pr`'s comment, continuous-integration, monitoring, repair-batch,
and atomic merge-continuation contracts. If pull-request feedback or checks
mutate the delivery, update this skill's ledger with the resulting evidence and
outcome. Do not merge without `/ship-pr`'s explicit authorization gate.

## Completion contract

Return exactly one of:

- the exact merge-authorization request and evidence for the reviewed pull
  request
- the merged pull request and verified cleanup after authorized continuation
- a concrete blocker with delivery scope, current tree/head, open ledger rows,
  preserved evidence, and the smallest next decision

Never report success when review, verification, required repairs, shipping
evidence, eligible feedback, or merge authorization remains unresolved.
