# Interface Audit Update Workflow

Update one report in place from complete incremental evidence.

## Select the report

Use an explicit valid path when supplied; correct only an unambiguous typo.
Otherwise select the newest matching report under `.agent-layer/tmp` by a
deterministic local order. If no valid report exists, preserve any old artifact
and run a fresh audit rather than substituting or partially updating a report.

Read the selected report before editing so identifiers, requirements, scope,
and scoring remain coherent.

## Establish the evidence boundary

Require a valid `Last updated UTC`, baseline commit, and enough recorded local
state to reconstruct any dirty prior boundary. Establish all changes that may
affect interface rows from:

- the current working tree
- repository history from the recorded commit through the audited commit
- merged PRs strictly after the recorded timestamp

Use history to locate affected interfaces; current code, tests, and contracts
remain authoritative. Fetch complete result sets and account for pagination or
truncation. A complete local-history substitute is acceptable when PR metadata
is unnecessary. If the interval or prior dirty state cannot be established,
leave the old report unchanged and run a fresh audit. Never silently cap or
label incomplete evidence as a complete update.

Scale inspection to relevance: collect the full interval's identifiers,
timestamps, and paths, then inspect details only where they may change a
boundary, requirement, score, or recommendation.

## Update and hand off

Refresh metadata, affected and neighboring calibration rows, interface map,
requirements, candidates, proposed spec, and update log. Preserve stable row
IDs and retire rather than reuse removed IDs. Keep historical detail concise in
the update log; the body describes current code.

Advance `Last updated UTC` only after the complete update succeeds. Recheck
changed claims against current evidence and internal score calibration. Do not
continue into planning or implementation.

Return the report path, material changes, highest-value remaining issue,
architecture and behavior impact, and smallest coherent next improvement; use
`no-material-improvement` when none justifies its cost.
