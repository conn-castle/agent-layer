# Mode Contract

Each mode uses `Purpose`, `Required roles`, `Initialize`, `Select`, `Execute`,
`Reconcile`, and `Exhaustion`. The core follows those instructions without
branching on mode names.

## Selection

Inspect as much of the source as useful to choose the next work, including the
whole source when inexpensive. Plan only that work. Give its source references
and grouping reason to a fresh executor, which rereads the authoritative source.
For a large source, use native filters and paging and note where the current pass
should continue. Prior work may prevent duplication but never broadens the new
scope. Revisit blocked work when its condition changes; exhaustion requires a
complete current pass.

## Common plan execution

For a plan-based mode, dispatch `planner` to run `/plan-work`, passing the
caller's exactly three `plan_reviewers` targets unchanged; `/plan-work` owns all
`/review-plan` dispatch. Dispatch `implementer` with `/implement-plan`, then
dispatch `code_reviewer` fresh with `/review-uncommitted-code` and again in a
separate fresh context with `/verify-work` against the same implemented tree.
Dispatch `planner` fresh to validate and deduplicate both result sets, then
dispatch `implementer` fresh with the combined accepted in-scope repairs. Rerun
only invalidated checks and verification; repeat semantic review only when a
repair materially changes the reviewed design or contract surface. Final
verification always covers the final tree. Pass every target unchanged under
the fixed role mapping in `SKILL.md`.

## Continuation

Selection must distinguish selected work from a complete pass that is exhausted
or blocked on every remaining candidate. Execution must distinguish completed
work, a supported no-change result, preserved blocked work, and a failure with a
supported retry path. Include enough evidence for the orchestrator to choose the
next transition. Stop immediately only when authority or repository state
cannot be recovered safely.

Reconcile the actual delivery result with its source. Never mark an external
source complete before its delivery is authoritative.
