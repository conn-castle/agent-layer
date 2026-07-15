---
name: audit-tests
description: >-
  Explicit-only.
  Audit the existing test suite once for redundancy, misleading coverage,
  organization, and material behavioral gaps, directly fixing safe findings.
---

# audit-tests

Audit tests and fix clear negative-value or mechanical problems. Use
`/boost-coverage` for coverage targets and `/clean-and-fix-code` for tests added
in the current diff.

## Scope

- Default to all tests; accept paths, modules, repository-defined tiers,
  coverage permission, and a report limit that does not reduce coverage.
- Derive tiers from repository configuration. If ambiguous, audit concrete
  tests without tiers and report the limitation.

Write `.agent-layer/tmp/audit-tests.<run-id>.report.md`, using
`YYYYMMDD-HHMMSS-<short-rand>` for `run-id`.

## Contract

- Findings identify concrete tests, behavior, and evidence; coverage percentage
  alone is not a finding.
- Delete tautological, self-confirming, dead, rubber-stamp, or duplicate tests,
  preserving the strongest behavioral coverage.
- Make mechanical assertion, naming, tier, helper, and fixture repairs when the
  intended behavior is established.
- Do not delete partially valuable tests or change production for testability
  without a user decision. Replace false coverage only for a clear behavior
  contract.
- Ignore framework conventions and style preferences that do not affect value.

## Workflow

Read COMMANDS.md before selecting commands. Identify the test runner,
configuration, conventions, fixtures, helpers, and tiers. Inventory the scope.
For large scopes, parallelize non-overlapping read-only investigations; the
owning agent validates candidates, reconciles gaps/duplication, and edits.

Review:

- duplicate scenarios, setup, assertions, helpers, and fixtures
- false, fragile, dead, misleading, or misplaced tests
- unexpected I/O, network, sleeps, or other tier violations
- material gaps in failures, boundaries, interactions, or critical workflows

Use coverage when documented and useful, reusing results until edits invalidate
them. Apply safe deletions, consolidation, and mechanical fixes. Leave
judgment-dependent work untouched; recommend `/boost-coverage` for material
missing behavior.

If files changed, run a credible repository lane covering them. Diagnose and
repair in-scope failures, then rerun invalidated checks.

The report contains:

1. `# Test Audit Summary` — scope, conventions, and verdict
2. `## Inventory` — grouped count by discovered tier
3. `## Fixes Applied`
4. `## Material Findings` — category, tier, location, evidence, and outcome
5. `## Gap Findings` — material behavioral gaps and their affected scope
6. `## Decisions Needed`
7. `## Verification`

Outcomes are `fixed`, `needs-user-decision`, or `recommend-boost-coverage`; use
`None` for empty sections. Finish after full evidence coverage, terminal finding
outcomes, and a passing lane for changed tests. Return the report path, fixes,
residuals, and verification.
