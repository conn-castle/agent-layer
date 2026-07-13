# Interface Audit Report Structure

Write `.agent-layer/tmp/interface-audit.<run-id>.md`, using
`YYYYMMDD-HHMMSS-<short-rand>` for the run id.

## Required sections

### `# Interface Audit Report`

State the report's purpose.

### `## Metadata`

Record report path, mode, created and last-updated UTC timestamps, repository,
commit, scope, source evidence, and whether the working tree is clean. For a
dirty tree, include changed paths or another compact source reference that a
later update can reconstruct. `Last updated UTC` is the update boundary.

### `## Product Requirements`

List each relevant requirement with evidence and status.

### `## Scoring Rubric`

Use integers from 1 (clean/simple) to 5 (complex, over-engineered, or fragile):

- `Complexity`: difficulty understanding or reasoning about the boundary
- `Over-engineering`: unused or premature machinery
- `Debt`: coupling, weak tests, unclear ownership, fragility, or costly failure
  modes

Average the three scores to one decimal place.

### `## Interface Map`

Show the product interface chain, direction, scored row IDs, and out-of-scope
boundaries as a compact list or Mermaid diagram.

### `## Scores`

```md
| # | Interface | Boundary | Dir | Complexity | Over-Eng | Debt | Avg | Confidence | Evidence |
|---|-----------|----------|-----|------------|----------|------|-----|------------|----------|
```

Use stable IDs; letters may distinguish parallel chains. Name the contract type
(for example typed, schema, protocol, event, concrete, or partial), direction,
High/Medium/Low confidence, and terse current evidence.

### `## Row Details`

Optional details for rows whose scores need more support: contract, primary
files, state/lifecycle, score rationale, evidence, cleanup opportunity, and
constraints.

### `## Calibration Notes`

Explain material score differences and any remaining evidence gap. Do not use
low scores to hide under-investigation.

### `## Improvement Candidates`

For each credible candidate, name affected rows, expected benefit, behavior or
architecture impact, and relative value. Use
`None — no material improvement` when appropriate.

### `## Proposed Next Spec`

For the selected candidate record title, type, target rows, problem, outcome,
non-goals, behavior changes, risks, rationale, and exact `/plan-work` input. Use
`None — no material improvement` when appropriate.

### `## Update Log`

Append a UTC entry summarizing evidence and changed rows. For updates, include
relevant merged PRs and local-change categories.
