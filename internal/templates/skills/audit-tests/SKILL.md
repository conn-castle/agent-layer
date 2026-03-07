---
name: audit-tests
description: >-
  Audit the test suite for redundancy, quality gaps, and organizational health
  across unit, integration, and e2e tiers. Discovers test conventions from the
  project, classifies tests by tier, identifies duplicative or low-value tests,
  finds coverage gaps that metrics miss, and reports findings with actionable
  recommendations. Use `boost-coverage` to fill gaps; use this skill to assess
  whether the existing tests are worth keeping and well-organized.
---

# audit-tests

Audit the health of the existing test suite, not just coverage numbers.
Default behavior is:
- discover the project's test conventions and runner configuration
- classify existing tests by tier (unit, integration, e2e)
- identify redundant, duplicative, or low-value tests
- identify meaningful coverage gaps that line-coverage metrics miss
- identify misclassified tests (e.g., integration tests labeled as unit tests)
- report findings without modifying tests

Use `boost-coverage` when the goal is to write new tests to raise coverage.
Use this skill when the goal is to assess whether the existing test suite is
healthy, well-organized, and non-redundant.

## Defaults

- Default scope is all test files in the repository.
- Default mode is report-only. Fix mode is available when requested.
- Classify tests into tiers based on project conventions, not assumptions.
- If the project has no clear tier separation, note that as a finding rather
  than inventing a classification scheme.
- Prioritize findings that affect developer confidence, suite speed, or
  maintenance burden.

## Inputs

Accept any combination of:
- explicit paths, directories, or modules
- tier filters (unit, integration, e2e, or all)
- a maximum finding count
- whether to operate in fix mode (remove/refactor tests)
- whether to run coverage commands during gap analysis (default: yes if available)

## Required artifact

Write the report to:
- `.agent-layer/tmp/audit-tests.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create the file with `touch` before writing.

## Multi-agent pattern

Use subagents liberally when available.

Recommended roles:
1. `Convention scout`: discovers test runner, directory layout, naming
   patterns, and tier conventions.
2. `Redundancy analyst`: identifies duplicative and overlapping tests.
3. `Quality analyst`: identifies low-value tests, weak assertions, and
   misclassified tests.
4. `Gap analyst`: identifies meaningful coverage gaps by comparing test
   targets against production code structure.
5. `Reporter`: writes the final report.

## Global constraints

- Do not modify test files or production code in report-only mode.
- Do not assume test tier conventions; discover them from the project's
  configuration, directory structure, and naming patterns.
- Do not treat line-coverage metrics as the sole measure of test health.
- Do not flag tests as redundant without concrete evidence (shared setup,
  identical assertions on the same code path, duplicated scenarios).
- Keep findings tied to specific test files and functions.
- Do not run tests or coverage commands unless required for gap analysis
  and the user has not opted out.

## Human checkpoints

- Required: ask when the project's test conventions are ambiguous enough
  that tier classification would be unreliable.
- Required: ask before deleting or significantly refactoring tests in fix mode.
- Required: ask when a finding would require changes to production code
  for testability.
- Stay autonomous during the audit itself.

## Audit workflow

### Phase 0: Preflight (Convention scout)

1. Confirm baseline with `git status --porcelain`.
2. Read `COMMANDS.md` before choosing any test or coverage commands.
3. Discover test conventions: runner and configuration, directory structure and naming patterns, tier separation (directories, suffixes, tags, markers), fixture/helper patterns, and any categorization system.
4. If tier conventions are unclear, note this and proceed with best-effort classification. Do not invent conventions.

### Phase 1: Inventory and classify (Convention scout)

1. Build an inventory of all test files in scope.
2. Classify each test file by tier:
   - **Unit**: tests a single function/method/class in isolation, mocks
     external dependencies
   - **Integration**: tests interaction between multiple components, may use
     real databases or services
   - **E2E**: tests complete user-facing workflows or API paths
   - **Unclassified**: does not clearly fit a single tier
3. Record the classification rationale for each file or group.
4. Note any tests that appear misclassified relative to their actual behavior
   (e.g., a "unit" test that starts a database).

### Phase 2: Redundancy analysis (Redundancy analyst)

Identify tests that are duplicative or overlapping:
- tests that exercise the same code path with the same inputs and equivalent
  assertions
- test functions that are copy-pasted with trivial variations
- multiple test files covering the same module with substantial overlap
- test helpers or fixtures that duplicate production code behavior

For each redundancy finding, state:
- which tests overlap
- what they share (code path, setup, assertions)
- which test is the more complete or maintainable version

### Phase 3: Quality analysis (Quality analyst)

Identify tests with quality concerns:
- **Weak assertions**: tests that assert only on happy paths, check only
  truthiness, or assert on implementation details rather than behavior
- **Missing edge cases**: tests that cover the happy path but skip error
  paths, boundary conditions, or guard clauses for tested functions
- **Fragile tests**: tests tightly coupled to implementation details that
  would break on safe refactors
- **Misleading names**: test names that do not match what the test actually
  verifies
- **Dead tests**: tests that are skipped, commented out, or unreachable
- **Slow unit tests**: tests classified as unit tests that perform I/O,
  network calls, or sleep

### Phase 4: Gap analysis (Gap analyst)

Analyze gaps separately for each tier. Every tier must have its own
dedicated section in the findings. No tier may be silently omitted.

For each tier, the conclusion must be one of:
- gaps exist (list them)
- no gaps found (state the evidence)
- not applicable (with genuine architectural justification — e.g., a pure library with no running services has no meaningful integration tier)
- tier does not exist yet but the project would benefit from it (this is itself a gap finding)

"Not applicable" requires genuine architectural justification, not merely "the project doesn't have these tests yet."

**Unit test gaps:** untested production functions/methods/modules; uncovered error paths, guard clauses, and boundary conditions; complex branching tested only at higher tiers; recently changed code (last 3 months) with stale or missing unit tests.

**Integration test gaps:** untested component interactions and interface boundaries; data-layer operations tested only via mocks; configuration/wiring never tested with real components.

**E2E test gaps:** untested user-facing workflows or API paths; critical business flows relying solely on lower-tier coverage; deployment-sensitive paths (migrations, startup, health checks) with no e2e coverage.

**Additional tiers** (when discovered): apply the same gap analysis to any project-specific tier (contract, smoke, performance, etc.) and note when a tier is expected by conventions but has no tests.

Focus on gaps that represent real risk, not low line-coverage numbers. If coverage commands are available and the user has not opted out, run them to inform the analysis.

### Phase 5: Synthesize findings (Reporter)

Each finding across all phases must include:
- `Title`
- `Severity`: High | Medium | Low
- `Category`: redundancy | quality | gap | misclassification
- `Tier`: unit | integration | e2e | cross-tier (for redundancy/quality findings that span tiers)
- `Location`: test file(s) and function(s)
- `Evidence`: concrete observation
- `Recommendation`

## Required report structure

Write `.agent-layer/tmp/audit-tests.<run-id>.report.md` with:

1. `# Test Audit Summary`
   - scope audited
   - test conventions discovered
   - short outcome summary
2. `## Test Inventory`
   - total test count by tier
   - tier classification rationale
   - misclassified tests
3. `## Redundancy Findings`
   - ordered by impact (most duplicative first)
4. `## Quality Findings`
   - ordered by severity
5. `## Gap Findings`
   - one subsection per tier (`### Unit Test Gaps`, `### Integration Test Gaps`, `### E2E Test Gaps`, plus each additional tier discovered)
   - ordered by risk; every tier must appear with an explicit conclusion
6. `## Strengths`
   - well-tested areas, good patterns worth preserving
7. `## Recommended Actions`
   - prioritized list: what to remove, what to fix, what to add
   - distinguish between actions for this skill (fix mode) and actions
     for `boost-coverage` (writing new tests)

## Guardrails

- Do not turn test style preferences into findings unless they affect
  correctness, maintainability, or developer confidence.
- Do not recommend removing tests without evidence of redundancy or
  negative value.
- Do not conflate low coverage with poor test quality; they are separate
  concerns.
- Do not flag framework-generated or conventional boilerplate as redundant.
- Do not apply fixes in report-only mode (the default).
- Do not widen a test audit into a production code audit.

## Fix mode

When fix mode is requested:

1. Remove clearly dead tests (skipped, commented out, unreachable).
2. Consolidate duplicative tests, keeping the more complete version.
3. Strengthen weak assertions where the fix is mechanical and unambiguous.
4. Ask before removing or consolidating tests that are borderline.
5. Record each fix in the report under a `## Fixes Applied` section.
6. Run the test suite after fixes to confirm nothing broke.

## Final handoff

After writing the report:
1. Echo the report path.
2. Summarize the highest-value findings in chat and state the test inventory by tier.
3. If fixes were applied, summarize changes and test suite result.
4. If significant gaps were found, recommend `boost-coverage` on the identified areas.
