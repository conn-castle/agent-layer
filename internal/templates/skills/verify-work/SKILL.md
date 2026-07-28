---
name: verify-work
description: >-
  Explicit-only.
  Verify completed work against its authoritative contract and report coverage,
  working-code evidence, material gaps, and the completion verdict.
---

# verify-work

Verify the final tree against its contract without fixes.

## Required inputs

Require exact plan/task/context, or an explicit request and scope. Never
discover contracts from `.agent-layer/tmp/`.

## Output artifact

Write `.agent-layer/tmp/verify-work.<run-id>.report.md`, using
`YYYYMMDD-HHMMSS-<short-rand>` for `run-id`.

## Rules

- Apply `references/contract-verification-rubric.md`
  to the current tree and touched files with evidence proportional to behavior
  and risk.
- Report only material completion, behavior, safety, scope, docs, or memory
  gaps.
- Do not modify code, documentation, memory, or planning artifacts.

## Workflow

Read the contract/context, relevant diff, and final touched files. Use
implementation reports only to locate deviations, skipped work, or evidence.

Record each contract item as `complete`, `partial`, `missing`, or `unverified`.
Assess supplements separately and identify material scope drift or undocumented
deviations without general code review.

Read `COMMANDS.md`, then run the narrowest credible checks; broaden only for
the contract. If any cannot run, report `incomplete`.

Direct inspection may be evidence when command output is not the right proof,
but absence of evidence is not completion evidence.

Write:

1. `# Completion Verdict`
2. `## Inputs`
3. `## Contract Coverage`
4. `## Material Findings`
5. `## Working-Code Evidence`
6. `## Shipping Obligations`
7. `## Docs and Memory Assessment`
8. `## Recommended Next Step`

Each finding includes contract item/location, evidence, impact, and smallest
correction.

Document and return exactly one final verdict:

- `complete`
- `complete-with-follow-up`: the contract is complete and remaining work is
  explicitly outside it
- `incomplete`

Account for every contract item, supplement, shipping obligation, and final-tree
evidence. Return report path and verdict; for `incomplete`, name the next exact
correction.
