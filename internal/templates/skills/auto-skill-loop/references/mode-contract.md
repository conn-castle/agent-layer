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

## Direct repair execution

Use direct repair for established work with concrete acceptance behavior, no
unresolved human decision, and a localized change boundary. Dispatch
`implementer` fresh with the selected source item, objective, boundary, and
required checks. The implementer baselines relevant behavior, implements the
repair, diagnoses and addresses concrete failures, and self-reviews the final
diff.
Then dispatch `code_reviewer` once with `/verify-work` against the explicit
request and final tree. Route material verification findings to a fresh
`implementer`, rerun only invalidated checks, and obtain final independent
verification. Escalate to common plan execution when evidence reveals a
substantive architecture, public-contract, migration, or cross-cutting risk
decision. A failed hypothesis, larger-than-expected repair, or tool or
delegation failure is evidence to diagnose, retry, or reroute; it is not a stop
condition. When evidence shows every safe execution path is exhausted, preserve
useful work, record a supported still-blocked disposition, and return to
selection until the blocking condition changes.

## Common plan execution

For a plan-based mode, dispatch `planner` to run `/plan-work`, passing the
caller's complete non-empty `plan_reviewers` list unchanged; `/plan-work` owns
all `/review-plan` dispatch. Dispatch `implementer` with `/implement-plan`, then
dispatch `code_reviewer` fresh with `/review-uncommitted-code` and again in a
separate fresh context with `/verify-work` against the same implemented tree.
Dispatch `planner` fresh to validate and deduplicate both result sets, then
dispatch `implementer` fresh with the combined accepted repairs. Rerun
only invalidated checks and verification; repeat semantic review only when a
repair materially changes the reviewed design or contract surface. Final
verification always covers the final tree. Pass every target unchanged under
the fixed role mapping in `SKILL.md`.

## Continuation

Selection must distinguish selected work from a complete pass that is exhausted
or blocked on every remaining candidate. Execution continues through failures
by diagnosing, retrying, repairing, or rerouting them. Preserve useful work when
an external condition prevents immediate progress and continue independent
work. Only the human-input conditions in `blocker-classification.md` can pause
selected work, and they stop the loop only after a complete pass finds no
independent work. Exhausted safe execution paths instead produce a supported
still-blocked disposition and return the loop to independent selection. Include
enough evidence for the orchestrator to choose the next transition.

Reconcile the actual delivery result with its source. Never mark an external
source complete before its delivery is authoritative.
