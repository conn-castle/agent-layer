---
name: improve-codebase
description: >-
  Deep, autonomous audit and improvement of the entire codebase (or user-specified
  subsystems): survey the repository structure, decompose into reviewable chunks,
  run parallel multi-lens reviews, fix findings iteratively, and delegate to
  complementary skills (simplify-code, boost-coverage, audit-tests, fix-issues) where
  appropriate. Use after a release, during a maintenance window, or whenever a
  thorough independent quality sweep is needed.
---

# improve-codebase

This is the whole-repository audit and improvement orchestrator.
It should run a systematic survey that:
- decomposes the codebase into reviewable chunks by module/package/subsystem
- audits each chunk with parallel multi-lens reviewers
- runs cross-cutting reviews (architecture, consistency, security)
- fixes accepted findings iteratively
- delegates to complementary skills where they add value
- populates `ISSUES.md` with anything deferred
- reports each chunk's findings and fixes to the human

Use this skill for deep, independent codebase-wide quality sweeps.
Use `audit-and-fix-uncommitted-changes` instead when the target is working-tree diffs only.

## Scope default

Default scope:
- the entire repository source tree
- all production code, tests, docs, and configuration
- excluding generated files, vendor directories, and build artifacts

The user can narrow scope with:
- explicit paths or directories
- file-type filters (e.g., "only Go files", "only tests")
- subsystem names
- specific audit lenses (e.g., "security only", "architecture only")

## Inputs

Accept any combination of:
- explicit paths or subsystem names
- audit lens filters (all, correctness, architecture, security, quality, coverage)
- a chunk iteration cap
- a findings-per-chunk severity threshold for stopping early
- whether to run complementary skills (simplify-code, boost-coverage, fix-issues)
- whether to operate in report-only mode (no fixes)

## Required artifacts

Use one shared `run-id = YYYYMMDD-HHMMSS-<short-rand>`.

Always create:
- `.agent-layer/tmp/improve-codebase.<run-id>.report.md`

Create the file with `touch` before writing.

Reuse and reference per-chunk artifacts created by delegated skills:
- `.agent-layer/tmp/review-scope.<run-id>.report.md`
- `.agent-layer/tmp/resolve-findings.<run-id>.report.md`
- `.agent-layer/tmp/simplify-code.<run-id>.report.md`
- `.agent-layer/tmp/boost-coverage.<run-id>.report.md`
- `.agent-layer/tmp/audit-tests.<run-id>.report.md`
- `.agent-layer/tmp/fix-issues.<run-id>.report.md`

The master report is the human-facing ledger. It must remain readable without opening child artifacts.

## Required behavior

Use subagents liberally when available.

At minimum, use:
- a survey scout that maps the repository structure
- parallel audit reviewers with different lenses per chunk
- a findings resolver/fixer
- a cross-cutting reviewer for architecture and consistency
- a synthesizer that maintains the master report

Prefer the dedicated skills that already exist:
- `review-scope` for per-chunk auditing
- `resolve-findings` for triaging and fixing findings
- `simplify-code` when complexity warrants it
- `boost-coverage` when test gaps are significant
- `audit-tests` when test suite quality, redundancy, or organization is concerning
- `fix-issues` when existing `ISSUES.md` entries overlap with findings

## Global constraints

- Treat the codebase as the target, not working-tree diffs.
- Do not attempt to review every line of every file. Prioritize by risk, complexity, and staleness.
- Fix all accepted findings regardless of severity.
- Do not stage, commit, or discard changes unless the user explicitly asks.
- Keep each chunk review focused and reviewable.
- If a fix changes the relevant surface area materially, re-audit that chunk.
- Log deferred findings to `ISSUES.md` instead of silently dropping them.
- Do not weaken tests, lower thresholds, or skip checks to clear findings.

## Human checkpoints

- Required: ask when the repository is too large to audit meaningfully in one session and the user has not scoped it down.
- Required: ask when an accepted finding requires a breaking change, broad architectural refactor, or user-visible behavior change.
- Required: ask when a finding cannot be verified with available code, tests, or docs.
- Required: ask before any destructive or irreversible action.
- Stay autonomous during normal survey, audit, fix, and re-audit cycles when findings and fixes are clear.

## Orchestration loop

### Phase 0: Preflight (Repo scout)

1. Confirm baseline with:
   - `git status --porcelain`
   - `git diff --stat`
2. Read in this order when they exist:
   - `COMMANDS.md`
   - `README.md`
   - `DECISIONS.md`
   - `ISSUES.md`
   - `ROADMAP.md`
3. Identify the repository structure:
   - top-level directories and their purposes
   - language(s) and framework(s) in use
   - test locations and conventions
   - generated or vendored paths to exclude
4. Note existing known issues from `ISSUES.md` to avoid reporting them as novel findings.

### Phase 1: Survey and decompose (Survey scout)

1. Map the repository into reviewable chunks by:
   - package, module, or directory boundaries
   - functional subsystem (e.g., "config loading", "sync engine", "CLI commands")
   - test suites as their own review targets when relevant
2. Prioritize chunks by:
   - complexity signals (file size, function count, nesting depth)
   - recent change frequency (`git log --since="3 months ago" --name-only`)
   - test coverage gaps when coverage data is available
   - presence of TODO/FIXME/HACK markers
   - proximity to data boundaries, security surfaces, or reliability-critical paths
3. State the chunk list, priority order, and rationale in the master report.
4. If the total scope is clearly too large for one session, propose the highest-value subset and ask before proceeding.

### Phase 2: Audit chunk N (Audit reviewers)

Use the `review-scope` skill on the current chunk with proactive hotspot mode.

The `review-scope` lenses cover correctness, architecture, and quality. For this orchestrator, also assess:
- **Security**: input validation gaps, injection risks, credential handling
- **Consistency**: naming conventions, error patterns, style drift across the codebase

Copy the high-signal findings summary into the master report under `## Chunk N: <name> Findings`.

### Phase 3: Fix chunk N findings (Fixers)

Use the `resolve-findings` skill on the chunk N review report.

Fix every accepted finding regardless of severity. For findings that cannot be fixed in scope:
- log them to `ISSUES.md` with the severity, location, and next step
- mark them as deferred in the master report

Copy the fix summary into the master report under `## Chunk N: <name> Fixes`.

If a fix exposes obvious local complexity:
- use the `simplify-code` skill on the affected area
- then re-audit the chunk if the changes were material

### Phase 4: Cross-cutting review (Architecture reviewer)

After all chunks have been individually audited, run a cross-cutting review covering:
- **Architectural consistency**: do modules respect their boundaries? Are there layering violations?
- **Pattern consistency**: are similar problems solved differently in different places?
- **Error handling patterns**: consistent approach across the codebase?
- **Naming and convention drift**: inconsistencies that accumulated over time?
- **Dependency health**: outdated, duplicated, or unnecessary dependencies?
- **Documentation alignment**: do README, memory files, and inline docs match the actual code?

Record cross-cutting findings in the master report under `## Cross-Cutting Findings`.

Fix accepted cross-cutting findings using the same resolve-findings workflow.

### Phase 5: Complementary skill delegation (Orchestrator)

Delegate to complementary skills only when the audit surfaces systemic issues in their domain: `boost-coverage` for significant test gaps, `audit-tests` for widespread redundancy/misclassification, `simplify-code` for significant complexity, `fix-issues` for overlapping `ISSUES.md` entries. Skip delegation when no meaningful gaps exist.

Record delegation outcomes in the master report under `## Complementary Skill Results`.

### Phase 6: Close the run (Reporter)

When all chunks and cross-cutting reviews are complete:
1. Add `## Final Summary` to the master report
2. Summarize:
   - chunks audited and their status
   - total findings by severity
   - findings fixed vs. deferred
   - complementary skills invoked and their outcomes
   - overall codebase health assessment
3. Add `## Residual Risk` for any systemic concerns that remain

## Required master report structure

Write `.agent-layer/tmp/improve-codebase.<run-id>.report.md` with:

1. `# Codebase Improvement Summary`
2. `## Repository Overview`
3. `## Chunk Map and Priority Order`
4. For each chunk:
   - `## Chunk N: <name> Findings`
   - `## Chunk N: <name> Fixes`
5. `## Cross-Cutting Findings`
6. `## Cross-Cutting Fixes`
7. `## Complementary Skill Results`
8. `## Final Summary`
9. `## Residual Risk`

## Minimal status protocol

At each major stage, echo the master report path, identify the current chunk, and state one of:
- preflighting the repository
- surveying and decomposing
- auditing chunk N: <name>
- fixing chunk N findings
- running cross-cutting review
- delegating to <skill-name>
- closing the run

## Final handoff

After the run, present the results to the user in chat:

1. Echo the master report path.
2. State total chunks audited, total findings, and fixes applied.
3. Present a **Key findings and fixes** table sorted by Chunk (primary) then Severity (secondary).
4. Below the table, list deferred findings with their `ISSUES.md` entry references.
5. State the overall codebase health assessment.
6. List any complementary skills that were invoked and their outcomes.

## Guardrails

- Do not attempt to review every line. Prioritize by risk.
- Do not silently skip chunks that were in the plan.
- Do not carry unresolved deferred findings without logging them to `ISSUES.md`.
- Do not expand a chunk review into unrelated areas.
- Do not treat the cross-cutting review as optional.
- Do not claim a clean codebase without evidence from the audit rounds.
- Do not modify unrelated code just because it is nearby.
- Keep each chunk review grounded in concrete artifacts.
