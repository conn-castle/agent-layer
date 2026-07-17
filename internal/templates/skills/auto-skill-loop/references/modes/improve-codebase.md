# Improve Codebase

## Purpose

Improve selected repository scopes and lenses until a fresh pass finds no
material work.

## Required roles

Require `planner`, `implementer`, `code_reviewer`, and `rote_worker`. Do not
require `plan_reviewers` or the common plan flow.

## Initialize

Map repository-native components, cross-cutting boundaries, and relevant quality
lenses incrementally. Rank them using concrete risk signals such as data,
security, or reliability criticality; complex state or ownership; recurring
failures; concentrated change or staleness; and weak tests or verification.
Retain enough progress to cover all of them before declaring exhaustion.

## Select

Select the highest-value remaining evidence-supported scope and its relevant
lenses. Use the whole repository when practical; otherwise choose one coherent
component and its necessary cross-cutting boundaries. Prioritize credible
opportunities for substantive root-cause repairs: incorrect behavior, data loss,
security exposure, parser or input failures, lifecycle, concurrency, or
cancellation failures, broken interfaces or ownership, systemic reliability,
dependency, or performance problems, false test confidence, or material
architectural or maintenance cost. Examine local, cross-boundary, and
architectural causes.

Do not select a candidate because it is easiest, smallest, or immediately
actionable. While meaningful scopes or lenses remain without fresh coverage,
reject isolated guardrails, bookkeeping, cosmetic cleanup, speculative
abstraction, unjustified rewrites, and minor defensive changes. A small change
qualifies only when concrete evidence shows independently material impact;
otherwise continue searching for a substantive repair or prove exhaustion.

## Execute

Dispatch `implementer` to run `/improve-codebase` exactly once on the selected
scope and lenses. Do not invoke another skill or the common plan flow.

## Reconcile

Record completed scope, lens, and boundary coverage and preserve open blockers.
Do not repeat completed coverage merely for confidence or because its own
repairs changed the scope. Revisit only after relevant repository changes, for
a materially distinct uncovered lens or boundary, or to resume accepted work
after its concrete blocker changes.

## Exhaustion

All current components and relevant cross-cutting lenses have fresh coverage,
and a final wide pass reports no material finding.
