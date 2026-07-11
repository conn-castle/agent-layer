# Interface Audit Report Structure

Artifact shape for `/interface-audit` reports. Execution rules live in
`SKILL.md`.

## Report Filename

```text
.agent-layer/tmp/interface-audit.<run-id>.md
```

Run id format: `YYYYMMDD-HHMMSS-<short-rand>`.

## Required Report Sections

### `# Interface Audit Report`

One-line purpose statement.

### `## Metadata`

- `Report path:`
- `Mode:` fresh or update
- `Created UTC:`
- `Last updated UTC:`
- `Repository:`
- `Commit:`
- `Working tree:` clean or dirty, with brief status
- `Scope:`
- `Source evidence:` code, tests, docs, investigator evidence, PRs for updates

`Last updated UTC` is the update boundary.

### `## Product Requirements`

Bullets with requirement, evidence, and status.

### `## Scoring Rubric`

Score direction:

```text
1 = clean/simple, 5 = complex/over-engineered/fragile
```

Axes:
- `Complexity`: how hard the interface is to understand and reason about.
- `Over-engineering`: premature abstraction, unused layers, or machinery solving
  non-problems.
- `Debt`: fragility, awkward coupling, weak tests, unclear ownership, or failure
  modes that create future cost.

`Avg`: one decimal place.

### `## Interface Map`

Product interface chain as Mermaid or list, including scored rows,
directionality, and out-of-scope markers.

### `## Scores`

```md
| # | Interface | Boundary | Fns | State | LOC | Tests | Dir | Complexity | Over-Eng | Debt | Avg | Confidence | Evidence |
|---|-----------|:--------:|:---:|:-----:|:---:|:-----:|:---:|:----------:|:--------:|:----:|:---:|:----------:|----------|
```

Columns:
- `#`: stable row id. Use numbers with letters for parallel chains, such as
  `4a`.
- `Interface`: component boundary, not a vague area.
- `Boundary`: `typed`, `schema`, `protocol`, `event`, `concrete`, `partial`, or
  another explicit contract type.
- `Fns`: count of callable operations, protocol messages, event kinds, or public
  methods that define the boundary.
- `State`: approximate count or named list of state carried because of the
  interface.
- `LOC`: lines for primary implementation files only. Use `partial` if exact
  accounting is not worth the cost.
- `Tests`: `dedicated`, `partial`, `indirect`, or `none found`.
- `Dir`: `->`, `<-`, or `<->`.
- `Complexity`, `Over-Eng`, `Debt`: integers 1-5.
- `Avg`: one decimal place.
- `Confidence`: High, Medium, or Low.
- `Evidence`: terse references to files, tests, docs, or investigator evidence.

### `## Row Details`

```md
### [<row id>] <Interface name>

- Contract:
- Primary files:
- State and lifecycle:
- Why these scores:
- Evidence:
- Cleanup opportunity:
- Constraints:
```

Optional; use only when needed to defend the score.

### `## Score Calibration Notes`

- why the highest scores are higher than neighboring rows
- why low scores are low rather than merely under-investigated
- which rows remain Medium or Low confidence and what evidence is missing

### `## Improvement Candidates`

- affected row ids
- expected score movement or qualitative benefit
- whether it requires behavior change
- whether it requires major architecture
- why it is or is not the highest-value next item

### `## Proposed Next Spec`

- `Title:`
- `Type:` major architecture or interface improvement
- `Target rows:`
- `Problem:`
- `Proposed outcome:`
- `Non-goals:`
- `Behavior changes:` none, or explicit approval required
- `Risks and constraints:`
- `Why this next:`
- `Plan-work question:` ask whether to run `/plan-work` or search for another
  item

### `## Update Log`

```md
- <UTC timestamp>: <fresh audit|update>. Evidence reviewed: <summary>. Rows changed: <summary>.
```

For updates, include merged PR numbers and local change categories.
