---
name: verify-work
description: >-
  Verify completed work against its authoritative contract and report coverage,
  working-code evidence, material gaps, and the completion verdict.
---

# verify-work

Run one read-only pass to decide whether the final tree delivers its contract
with sufficient evidence. Do not fix findings.

## Required inputs

Require either exact plan/task paths with optional context, or an explicit user
request/scope. Do not discover artifacts from `.agent-layer/tmp/`.

The caller may also provide supplemental obligations, such as accepted cleanup
findings. Verify them separately; they do not replace or reinterpret the
authoritative contract.

Implementation reports, summaries, PR descriptions, and issue bodies are
evidence only; they cannot redefine or complete the contract.

## Output artifact

Write `.agent-layer/tmp/verify-work.<run-id>.report.md` using
`run-id = YYYYMMDD-HHMMSS-<short-rand>`.

## Rules

- Use `contract-verification-rubric.md` as the fixed comparison rubric.
- Verify the current working tree and the files touched for the supplied
  contract.
- Use the smallest credible evidence set for the changed behavior and its risk.
  Do not seek exhaustive certainty or run broad checks for confidence alone.
- Reuse existing command evidence only when the command, result, and covered
  repository state are known and still current.
- Report only gaps that materially affect contract completion, working behavior,
  safety, scope, or required documentation and memory.
- Do not modify code, documentation, memory, or planning artifacts.

## Workflow

### 1. Establish the verification target

Read the authoritative contract and optional context, then inspect the relevant
working-tree changes and post-implementation files. Use implementation reports
only to understand declared deviations, skipped work, or prior evidence.

If a required artifact is missing or the contract is too ambiguous to judge,
stop and request the smallest missing input or clarification.

### 2. Compare contract and implementation

Load `contract-verification-rubric.md` and apply it once. Record each contract
item as complete, partial, missing, or unverified. Evaluate supplemental
obligations separately. Identify material scope drift and undocumented
deviations without expanding into unrelated code review.

### 3. Gather working-code evidence

Read `COMMANDS.md` before selecting repository workflow commands. Run the
narrowest checks that credibly cover the contract and touched behavior,
including broader checks only when the contract or risk requires them.

For each command, record the command, result, relevant output or artifact, and
the repository state it covered. If a necessary check cannot run, record the
reason and residual risk. Do not repeat a current trustworthy check.

Direct inspection may serve as evidence when command output is not the right
proof, but absence of evidence is not completion evidence.

### 4. Report the verdict

Write:

1. `# Completion Verdict`
2. `## Inputs`
3. `## Contract Coverage`
4. `## Supplemental Obligation Coverage`
5. `## Material Findings`
6. `## Working-Code Evidence`
7. `## Docs and Memory Assessment`
8. `## Recommended Next Step`

For each material finding, include the affected contract item or location,
evidence, impact, and smallest corrective action.

Use exactly one verdict:

- `complete`
- `complete-with-follow-up`: the contract is complete and remaining work is
  explicitly outside it
- `incomplete`

## Completion contract

Return the report path and one verdict after accounting for every contract item,
supplemental obligation, and evidence covering the final tree. When incomplete,
name the next exact correction.
