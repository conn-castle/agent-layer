# Interface Audit Update Workflow

Update one existing report in place from a complete incremental evidence set,
then stop with the inputs needed for a final recommendation.

## Select the report

Use an explicit report path when supplied; stop if that path does not exist.
Otherwise select the newest matching interface-audit report under
`.agent-layer/tmp` using deterministic local ordering. If no report exists,
stop and ask whether to run a fresh audit. Do not create a parallel update
report.

Read the selected report before editing so its identifiers, requirements,
scope, and structure remain coherent.

## Establish the evidence boundary

Require a valid `Last updated UTC:` value and use its full timestamp as the
incremental history boundary. If it is missing or malformed, do not perform a
partial update. Report the missing boundary and recommend a fresh audit; ask
only if the user must choose between that fresh audit and an explicitly limited
update.

Enumerate both:

- local working-tree changes that affect existing or potential interface rows
- the complete set of merged pull requests strictly after the full update
  boundary

Use change history to locate affected interfaces; current code, tests, and
contracts remain authoritative for the report body. Label uncommitted evidence
as local changes and ignore unrelated local changes except for the metadata
summary.

Choose repository and GitHub commands from the available tooling rather than
requiring one command sequence. Detect pagination, result limits, and truncated
responses; never treat a capped result as the complete history. If merged
pull-request evidence cannot be retrieved or the full interval cannot be
enumerated, stop before editing and report the exact evidence failure. A local
history substitute is acceptable only when it can establish the complete
interval and the report does not require unavailable pull-request metadata;
otherwise ask whether to run a fresh audit or explicitly accept a limited
update.

Do not impose an arbitrary pull-request count cutoff or inspect every pull
request at equal depth. First collect the complete interval's identifiers,
timestamps, and changed paths. Inspect bodies, diffs, tests, and current files
only where they can affect interface boundaries, scores, product requirements,
or the proposed next specification. For a large interval, aggregate affected
paths and refresh the corresponding interface chain against the current tree.
If complete coverage is not credible within update mode, do not silently cap
the interval; stop and recommend a fresh audit.

## Update the report

Use `report-structure.md` as the artifact contract. Refresh metadata, every
affected row, relevant neighboring rows needed for score calibration, the
interface map and supporting sections, the proposed next specification, and
the update log. Preserve stable row identifiers and retire rather than reuse
removed identifiers. The report body describes current code; concise historical
context and merged pull-request numbers belong in the update log.

Set `Last updated UTC:` only after the complete update succeeds. Do not advance
the boundary after a partial or blocked update. An explicitly authorized
limited update must name the uncovered evidence, remain marked incomplete, and
leave the prior boundary unchanged.

Before handoff, re-read changed claims against current evidence and confirm the
report remains internally calibrated. Do not continue into planning or
implementation.

Return the updated report path, material rows or sections changed, the
highest-value remaining issue, whether major architecture appears necessary,
the smallest coherent next improvement, and any behavior change.
