---
name: verify-work
description: >-
  Verify completed work against its authoritative contract and report coverage,
  working-code evidence, material gaps, and the completion verdict.
---

# verify-work

Verify the final tree against its contract without fixes.

## Required inputs

Require exact plan/task paths with optional context, or an explicit request and
scope. Never discover contracts from `.agent-layer/tmp/`.

Supplemental obligations are additive and reported separately. Implementation
reports, summaries, pull-request descriptions, and issues are evidence, not
contract substitutes. A required lane becomes a shipping obligation only when
its sole blocker is a clean-revision requirement.

## Output artifact

Write `.agent-layer/tmp/verify-work.<run-id>.report.md`, using
`YYYYMMDD-HHMMSS-<short-rand>` for `run-id`.

## Rules

- Apply `contract-verification-rubric.md` to the current tree and touched files
  with evidence proportional to behavior and risk.
- Reuse command evidence only when its command, result, covered state, and
  relevance remain known.
- Report only material completion, behavior, safety, scope, docs, or memory gaps.
- Do not modify code, documentation, memory, or planning artifacts.

## Workflow

Read the contract/context, relevant diff, and final touched files. Use
implementation reports only to locate deviations, skipped work, or evidence.

Use a replacement for a missing artifact only when the caller designated the
same contract. Otherwise mark it `unverified`, return `incomplete`, and name the
missing input.

Record each contract item as `complete`, `partial`, `missing`, or `unverified`.
Assess supplements separately and identify material scope drift or undocumented
deviations without general code review.

Read COMMANDS.md, then run the narrowest credible checks; broaden only for
contract or risk.

For a clean-revision-blocked lane, run every independent substantive component.
If any cannot run or the lane adds untested behavior, report `incomplete`;
otherwise record the full lane as unpassed `/ship-pr` work.

Record commands, results, relevant output/artifacts, and covered state. For
checks that cannot run, record cause and risk. Direct inspection may be evidence
when command output is not the right proof, but absence of evidence is not
completion evidence.

Write:

1. `# Completion Verdict`
2. `## Inputs`
3. `## Contract Coverage`
4. `## Supplemental Obligation Coverage`
5. `## Material Findings`
6. `## Working-Code Evidence`
7. `## Shipping Obligations`
8. `## Docs and Memory Assessment`
9. `## Recommended Next Step`

Each finding includes contract item/location, evidence, impact, and smallest
correction.

Use exactly one verdict:

- `complete`
- `complete-with-follow-up`: the contract is complete and remaining work is
  explicitly outside it
- `incomplete`

Account for every contract item, supplement, shipping obligation, and final-tree
evidence. Return report path and verdict; for `incomplete`, name the next exact
correction.
