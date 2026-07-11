---
name: review-uncommitted-code
description: >-
  Targeted, report-only code review of files, directories, diffs, git ranges,
  uncommitted changes, or proactive hotspots for correctness, gaps, risks,
  architecture, tests, docs, performance, reliability, and maintainability.
---

# review-uncommitted-code

Review one concrete target and write a findings report. Do not modify reviewed
files.

## Target selection

Resolve the target in this order:

1. User-specified files, directories, diffs, or ranges.
2. Proactive hotspots when the user requested a codebase audit without exact
   targets.
3. Otherwise all staged, unstaged, and untracked working-tree changes.

Review the last commit only when explicitly requested. If no credible target
exists, ask for the smallest scope decision and stop.

For proactive hotspots, select a bounded target using concrete signals such as
change frequency, size or complexity, weak behavioral coverage, temporary
scaffolding, data or reliability boundaries, and drift from authoritative
project contracts. Record why each hotspot was selected.

## Required artifact

Write `.agent-layer/tmp/review-uncommitted-code.<run-id>.report.md`, where
`run-id` is `YYYYMMDD-HHMMSS-<short-rand>`.

Read `assets/finding-verdict-classification.md` during synthesis. It is the
single verdict rubric; it is not another review or classifier stage.

## Review contract

- Review concrete code, diffs, tests, and observable contracts rather than
  hypothetical alternatives.
- Use complementary perspectives on distinct concerns. For a non-trivial
  target, bounded reviewers may examine correctness, architecture, and
  quality/operability once in parallel. Do not run an outer review loop or ask
  another agent to classify the same evidence.
- Treat reviewer output as candidates until the synthesizer validates it
  against current repository evidence.
- Report only findings that materially affect correctness, safety, scope,
  reliability, performance, test integrity, or meaningful maintainability.
- Do not report style preferences, speculative edge cases, unsupported claims,
  or unrelated known issues as current findings.

## Workflow

### 1. Establish scope and evidence

Record the target, review mode, report path, and hotspot signals when
applicable. Read the minimum surrounding code, tests, docs, and memory needed
to establish intended behavior.

### 2. Run one purposeful review pass

Examine complementary concerns once:

- correctness, error handling, boundary inputs, and failure modes
- ownership, interfaces, coupling, and unnecessary complexity
- tests, documentation, performance, concurrency, data safety, and operational
  supportability where relevant

Assign concerns to distinct reviewers when using parallel review. A narrow
target may be reviewed directly without manufacturing extra roles.

### 3. Synthesize once

Validate candidates against the current tree, merge duplicates, and apply the
verdict rubric. Only evidence-backed candidates that survive this synthesis
become report entries.

Each reported finding includes:

- title, severity, and confidence
- exact location and reviewed scope
- recommended verdict: Accept | Defer
- evidence, why it matters, and a concrete recommendation

`Accept` means valid, current, in scope, and actionable without a new user
decision. `Defer` is reserved for a valid finding blocked by a user-owned
decision or explicit scope boundary. Candidates absent from the final reviewed
state are not findings and are omitted.

### 4. Write the report and yield

The report contains:

1. `# Review Summary` — target, mode, and readiness verdict
2. `## Recommended Accept`
3. `## Recommended Defer`

Use `None` for empty groups. The readiness verdict is `proceed`,
`proceed-after-fixes`, or `revise-first`.

## Definition of done

- The target received one evidence-backed review pass through the relevant
  complementary concerns.
- Every reported finding survived current-tree validation and has one verdict.
- The report exists with a readiness verdict and the skill yields without
  editing reviewed files.
