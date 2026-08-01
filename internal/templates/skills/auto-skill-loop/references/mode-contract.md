# Mode Contract

Each mode uses `Purpose`, `Required roles`, `Initialize`, `Select`, `Execute`,
`Reconcile`, and `Exhaustion`. The core follows those instructions without
branching on mode names.

## Selection

Inspect as much of the source as useful to choose the next work. Before
selecting, compare enough eligible candidates to identify natural bundles.
Prefer a delivery that amortizes
implementation, review, and verification while telling one coherent reviewer
story. Shared cause, implementation area, dependency chain, outcome, or
verification are strong grouping signals. A single-item delivery is appropriate
when it is independently substantial or the candidate survey reveals no
coherent companion.

Choose and plan one coherent PR delivery. Give its source references and
grouping reason to a fresh executor. For a large source, use native filters and
paging and note where the current pass should continue. Once execution begins,
keep the selected scope stable. Include
corrections needed to complete it and preserve adjacent work in its
authoritative source. Prior work may prevent duplication but never broadens the
new scope. Revisit blocked work when its condition changes; exhaustion requires
a complete current pass.

## Delivery substance

A mode may define additional substantive-delivery criteria in its `Execute`
section. It supplements the core line and file thresholds.

## Direct repair execution

Use direct repair for established work with concrete acceptance behavior, no
unresolved human decision, and a localized change boundary. Dispatch
`implementer` fresh with the selected source item, objective, boundary, and
required checks. The implementer baselines relevant behavior, implements the
repair and diagnoses and addresses concrete failures.
Then dispatch `code_reviewer` once with `/review-uncommitted-code` against the
explicit request and final tree. Route material review findings to a fresh
`implementer` and rerun invalidated checks. Escalate to common plan execution
when evidence reveals a substantive architecture, public-contract, migration,
or cross-cutting risk decision.

## Common plan execution

For a plan-based mode, dispatch `planner` to run `/plan-work`, passing the
caller's complete non-empty `plan_reviewers` list unchanged. Then run
`/fully-implement-plan` with the finalized plan and context artifacts and the
caller's `implementer`, `fixer`, and `code_reviewer` targets.

## Continuation

Selection must distinguish selected work from a complete pass that is exhausted
or blocked on every remaining candidate. Preserve useful work when an external
condition prevents immediate progress.

Reconcile the actual delivery result with its source. Never mark an external
source complete before its delivery is authoritative.
