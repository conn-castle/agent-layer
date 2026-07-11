---
name: audit-tests
description: >-
  Audit the existing test suite once for redundancy, misleading coverage,
  organization, and material behavioral gaps, directly fixing safe findings.
---

# audit-tests

Audit the health of the existing test suite and directly address clear
negative-value or mechanical findings. Use `/boost-coverage` when the primary
goal is to add tests to reach a coverage target, and `/clean-and-fix-code` for
tests added in the current uncommitted diff.

## Scope and inputs

- Default scope is every repository test file.
- Accept explicit paths, modules, or discovered test-tier filters; an optional
  maximum finding count; and whether coverage evidence may be gathered.
- Derive test tiers from repository configuration and conventions. Do not
  invent unit, integration, end-to-end, or other categories the repository
  does not use.
- If conventions are too ambiguous for a trustworthy classification, report
  the limitation and ask only if it blocks the requested audit.

## Required artifact

Write `.agent-layer/tmp/audit-tests.<run-id>.report.md`, where `run-id` is
`YYYYMMDD-HHMMSS-<short-rand>`.

## Finding and edit contract

- Findings must identify concrete tests, behavior, and evidence. Coverage
  percentage alone is not a quality finding.
- Delete tests that are clearly tautological, self-confirming, dead,
  rubber-stamp, or duplicative. Preserve the strongest behavioral coverage.
- Strengthen an assertion or correct a misleading name or classification only
  when the change is mechanical and the intended behavior is established.
- Do not delete a partially valuable test or change production code for
  testability without a user decision.
- Do not replace removed false coverage with another test unless a meaningful
  behavior gap and expected contract are already clear.
- Do not turn framework conventions or style preferences into findings.

## Workflow

### 1. Establish the test contract

Read COMMANDS.md before selecting commands. Identify the test runner,
configuration, directory and naming conventions, fixtures, helpers, and
repository-defined tiers. Build a grouped inventory sufficient to account for
the scope; do not produce per-file rationale when a directory or convention
establishes the classification.

### 2. Run one test-suite audit pass

Review the scope through complementary concerns:

- duplicate scenarios, setup, assertions, helpers, and fixtures
- tautological, self-confirming, rubber-stamp, fragile, dead, or misleading
  tests
- tests placed in the wrong repository-defined tier
- unexpected I/O, network access, sleeps, or other tier violations
- material behavioral gaps in error handling, boundaries, component
  interaction, or critical user workflows

For every discovered tier, state either material gaps found, no material gaps
found with evidence, or not applicable with architectural justification. Do
not require tiers that the repository does not define.

Run coverage at most once as audit evidence when it is documented, available,
and useful. It informs gap analysis but does not replace behavioral review.

### 3. Address safe findings directly

Apply all clear deletions, consolidations, and mechanical corrections in one
repair stage. Leave judgment-dependent changes untouched and record the
decision required. Recommend `/boost-coverage` for material missing behavior
that belongs in a dedicated coverage implementation, but do not launch it.

If files changed, run one credible repository-defined verification lane that
covers the edits. A failure is concrete evidence: directly repair an in-scope
mistake or return the blocker; do not start another audit round.

### 4. Report and yield

The report contains:

1. `# Test Audit Summary` — scope, conventions, and verdict
2. `## Inventory` — grouped count by discovered tier
3. `## Fixes Applied`
4. `## Material Findings` — category, tier, location, evidence, and outcome
5. `## Gap Findings` — one concise conclusion per discovered tier
6. `## Decisions Needed`
7. `## Verification`

Finding outcomes are `fixed`, `needs-user-decision`, or
`recommend-boost-coverage`. Use `None` for empty sections.

## Definition of done

- The full declared scope received one purposeful audit pass.
- Every reported finding is evidence-backed and materially affects confidence,
  speed, correctness, or maintenance cost.
- Safe findings were addressed once, and changed tests received one credible
  verification lane.
- The skill returns the report path, fixes, residual findings, and verification
  outcome, then yields.
