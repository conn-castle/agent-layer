---
name: interface-audit
description: >-
  Explicit-only.
  Audit product interfaces as component boundaries, score complexity,
  over-engineering, and debt, maintain .agent-layer/tmp interface-audit reports,
  and finish at the final recommendation gate. Use for fresh interface cleanup
  audits or --update refreshes; not for implementation.
---

# interface-audit

Produce one evidence-backed audit of product interface boundaries. Do not
implement or launch planning.

## Inputs and references

Run a fresh audit by default. `--update [report-path]` refreshes an existing
report. Read `references/report-structure.md`; for an update also read
`references/update-workflow.md`.

A fresh audit uses only current code, tests, docs, command output, and evidence
created for this run. Do not inspect prior audit artifacts unless the user asks
for an update.

## Evidence contract

- Score concrete component boundaries, not vague subsystems.
- Verify names and numeric claims; use `partial` when exact measurement adds
  little value.
- Ground complexity, over-engineering, debt, confidence, and recommendations in
  current evidence. Current code and tests outrank stale documentation.
- Preserve row identifiers during updates and never reuse retired identifiers.
- Protect discovered product requirements unless the user approves a behavior
  change. Do not preserve stale scores for continuity.

## Workflow

1. Establish fresh or update mode, repository baseline, report path, and scope.
2. Trace the interface chain, contracts, ownership, state, tests, failure modes,
   and meaningful cleanup opportunities. Investigate directly unless coherent,
   independent boundary groups benefit from read-only parallel investigation.
3. Calibrate neighboring rows, update the report, and select the highest-value
   coherent improvement. Revisit only evidence gaps or inconsistencies. If no
   candidate justifies its cost, record `no-material-improvement`.
4. For a material candidate, decide whether it requires broad ownership,
   protocol, data-model, cross-language, or user-workflow redesign. Recommend
   that architecture only when a smaller interface improvement is insufficient;
   otherwise recommend the smallest coherent improvement. State any behavior
   change and require approval before it enters a plan. Include exact
   `/plan-work` input as a handoff, but do not run it.

Do not edit production code, tests, docs, or memory files, widen beyond product
interfaces, or create parallel reports. Return the report path and either the
recommendation or `no-material-improvement`, with current evidence for every
material score and conclusion.
