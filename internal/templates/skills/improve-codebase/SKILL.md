---
name: improve-codebase
description: >-
  Run one bounded, evidence-led quality sweep over a repository or named scope:
  select high-value chunks, review each once, directly fix accepted findings,
  and perform one fresh-context post-fix review.
---

# improve-codebase

Improve a repository through one bounded sweep of its highest-value current
risks. This skill is broader than working-tree cleanup but is not an obligation
to reconsider every line or eliminate every conceivable issue.

## Scope and inputs

Accept explicit paths, subsystems, audit lenses, a maximum chunk count, and
report-only mode. Otherwise use repository-wide evidence to select a coherent,
reviewable set of high-value chunks for this invocation.

- Exclude generated, vendored, and build output.
- Prefer correctness, data safety, security, concurrency, cancellation,
  input robustness, test integrity, dependency boundaries, and material
  maintainability risks.
- Do not prioritize recent change, file size, low coverage, or TODO markers
  without a credible risk or maintenance consequence.
- A later invocation may choose a different scope or lens; this invocation does
  not loop merely because additional code exists.

Write `.agent-layer/tmp/improve-codebase.<run-id>.report.md`, where `run-id` is
`YYYYMMDD-HHMMSS-<short-rand>`. The report is the run ledger and handoff.

## Required agent boundaries

- Use a fresh built-in scout subagent to map risk signals and propose the
  bounded chunk set.
- Use `/review-uncommitted-code` for each selected chunk so its complementary
  review lenses and fresh-context subagents remain authoritative.
- Directly fix accepted findings in the current orchestration context or a
  bounded built-in fixer subagent when the chunks are independent.
- After fixes, use `reviewer-prompt.md` once in a fresh-context built-in
  subagent. This is the concrete post-fix review, not the start of a loop.
- Use one fresh built-in cross-cutting reviewer after chunk work to examine
  relationships that no individual chunk review could establish.

Do not replace these fresh-context boundaries with same-context reconsideration.

## Finding and repair contract

- Validate every candidate against current code, tests, specifications, and
  documented contracts. Evidence outranks reviewer agreement.
- Fix every `Recommended Accept` finding that remains within the declared
  scope. Group tightly coupled findings; keep unrelated mutations sequential.
- A finding may be deferred only for an explicit scope boundary or user-owned
  behavior, architecture, risk, cost, or migration decision. Record durable
  engineering debt in ISSUES.md when repository policy requires it.
- Apply directly required tests, documentation, and memory updates with the
  repair. Run focused evidence that demonstrates the finding is resolved.
- Do not re-audit a chunk after repair except for the single fresh-context
  post-fix review defined below.

## Workflow

### 1. Survey and select once

Read COMMANDS.md and the minimum repository context needed to understand known
constraints and existing issues. Have the scout identify concrete risk signals,
candidate boundaries, and exclusions.

Select a coherent chunk set that can be reviewed, repaired, and verified well
in one run. Record why each chunk is included. If an explicitly requested scope
cannot be handled safely in one run, ask for the smallest scope decision rather
than silently reducing it.

If the scout finds no evidence-backed chunk worth changing or reporting, write
`no-material-findings` and yield without manufacturing review work.

### 2. Review each selected chunk once

Run `/review-uncommitted-code` once per selected chunk in proactive-hotspot mode.
Use distinct chunks or lenses to gain complementary evidence; do not send the
same artifact through additional reviewers for confidence.

Copy only `Recommended Accept` and `Recommended Defer` findings into the master
report. Omit rejected candidates and unaffected-area inventories.

### 3. Address accepted findings directly

Validate and repair accepted findings, then run the narrowest credible affected
checks. Report a concrete blocker rather than starting a planning,
implementation, verification, simplification, coverage, test-audit, or
issue-fixing sub-workflow.

In report-only mode, record the same validated findings without editing.

### 4. Review the concrete result once

When fixes were made, pass `reviewer-prompt.md`, the post-fix content of every
changed chunk file, and the originating finding stable identifiers, titles,
severities, and locations to one fresh-context built-in subagent.

Directly address every material in-scope post-fix finding with focused evidence.
Do not invoke the reviewer again. A concrete failed check may return to the
responsible repair; the possibility of another finding may not.

### 5. Cross-cutting synthesis and yield

Have the fresh cross-cutting reviewer inspect the selected chunks together once
for material boundary, consistency, error-handling, dependency, and
documentation issues that cannot be seen within one chunk. Directly address
accepted in-scope findings under the same repair contract.

Recommend a complementary skill only when a concrete remaining concern belongs
to that skill's distinct responsibility. Do not invoke complementary skills
from this workflow.

## Report structure

1. `# Codebase Improvement Summary` — scope, lenses, and terminal outcome
2. `## Selected Chunks and Evidence`
3. `## Findings and Repairs` — accepted, deferred, and post-fix findings
4. `## Cross-Cutting Result`
5. `## Focused Verification`
6. `## Recommended Next Skill` — only when justified; otherwise `None`
7. `## Residual Risk`

Use one outcome: `improved`, `report-only`, `no-material-findings`, or
`blocked-user-decision`.

## Completion contract

- The scout proposed one bounded, evidence-led chunk set.
- Every selected chunk received one review and every accepted finding received
  a terminal repair, deferral, or blocker outcome.
- Changed chunks received one fresh-context post-fix review, followed by direct
  resolution rather than another review round.
- Cross-cutting synthesis examined the selected work once.
- The skill returns the report path, outcome, changes, evidence, deferred
  decisions, and any justified next-skill recommendation, then yields.
