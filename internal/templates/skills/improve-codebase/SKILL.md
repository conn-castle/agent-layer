---
name: improve-codebase
description: >-
  Run a deep autonomous quality sweep of the whole codebase or named
  subsystems: survey, split into reviewable chunks, run multi-lens reviews, fix
  findings iteratively, and coordinate follow-up cleanup or test work when
  needed.
---

# improve-codebase

This is the whole-repository audit and improvement orchestrator.
It should run a systematic survey that:
- decomposes the codebase into reviewable chunks by module/package/subsystem
- audits each chunk with parallel multi-lens review agents
- runs cross-cutting reviews (architecture, consistency, security)
- fixes accepted findings iteratively
- delegates to complementary skills where they add value
- populates `ISSUES.md` with anything deferred
- reports each chunk's findings and fixes to the user

Use this for deep codebase-wide sweeps; use `/clean-and-fix-code` when the target is working-tree diffs only.

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
- whether to run complementary skills (/simplify-codebase, /boost-coverage, /fix-issues)
- whether to operate in report-only mode (no fixes)
- `review_agents` for `/fix-issues` delegation

## Required artifacts

Use one shared `run-id = YYYYMMDD-HHMMSS-<short-rand>`.

Always create:
- `.agent-layer/tmp/improve-codebase.<run-id>.report.md`

Create the file with `touch` before writing.

The master report is the human-readable ledger and the single place to preserve orchestrator state.

Delegated skill outputs are handled one way:
- Use `/review-uncommitted-code` report artifacts as the findings input for the
  current chunk or cross-cutting review.
- Fix only findings classified under `Recommended Accept` after verifying them
  against the current repo state.
- Copy `/simplify-codebase`, `/boost-coverage`, `/audit-tests`, and
  `/fix-issues` outcomes from their final handoffs into the master report.
- Do not require, open, echo, or cross-reference delegated report artifacts from
  `/simplify-codebase`, `/boost-coverage`, `/audit-tests`, or `/fix-issues`.

## Required behavior

At minimum, use:
- a survey scout that maps the repository structure
- parallel audit review agents with different lenses per chunk
- an accepted-finding fixer
- a cross-cutting reviewer for architecture and consistency
- a synthesizer that maintains the master report

Prefer the dedicated skills that already exist:
- `/review-uncommitted-code` for per-chunk auditing
- `/simplify-codebase` when complexity warrants it
- `/boost-coverage` when test gaps are significant
- `/audit-tests` when test suite quality, redundancy, or organization is concerning
- `/fix-issues` when existing `ISSUES.md` entries overlap with findings

## Context Discipline

You are the orchestrator for this skill. Do not do work that belongs to
subagents or delegated skills in the orchestration context. Preserve your
context to make strategic decisions, enforce gates, reconcile returned outputs,
and continue this skill's workflow after every delegation returns.

When compaction is needed, retain this section verbatim and also preserve: current phase, active artifact paths, selected scope, pending gate verdict, delegated skills and subagents already run, their outcomes, unresolved blockers, and the next exact step.

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
- Required: ask when an accepted finding requires a breaking change, broad architectural refactor, or end-user-visible behavior change.
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

### Phase 2: Audit chunk N (Audit review agents)

Use the `/review-uncommitted-code` skill on the current chunk with proactive hotspot mode.

The `/review-uncommitted-code` lenses cover correctness, architecture, and quality. For this orchestrator, also assess:
- **Security**: input validation gaps, injection risks, credential handling
- **Consistency**: naming conventions, error patterns, style drift across the codebase

Copy the high-signal findings summary into the master report under `## Chunk N: <name> Findings`.

### Phase 3: Fix chunk N findings and re-audit (Fixers + Auditors)

Use the chunk N review report as input. Fix every `Recommended Accept` finding
regardless of severity.

For accepted findings:
- verify the finding against the current repo state before editing
- group duplicate or tightly coupled findings into one bounded fix target
- keep unrelated findings separate when they can be fixed independently
- diagnose the root cause
- implement the scoped fix and directly required test, doc, or memory updates
- run focused verification
- audit the final diff against the accepted finding

For accepted findings that cannot be fixed in scope:
- log them to `ISSUES.md` with the severity, location, and next step
- mark them as deferred in the master report

Do not defer merely because the fix might be broad when it stays within scope
and does not need a human checkpoint.

Copy the fix summary into the master report under `## Chunk N: <name> Fixes`.

If a fix exposes obvious local complexity:
- use the `/simplify-codebase` skill on the affected area

After fixes are applied, run one explicit re-audit pass on the chunk using a **fresh-context reviewer subagent**, not a continuation of the fixer's context. The reviewer sees only the post-fix chunk and originating findings, never the fix narrative.

Pass the contents of [`reviewer-prompt.md`](reviewer-prompt.md) to the reviewer subagent verbatim — do not paraphrase, summarize, or modify the rubric.

Inputs the reviewer receives alongside the prompt:
- The post-fix content of every file in the chunk.
- The originating findings list (titles, severities, locations only).
- Nothing else. No fixer narrative, no chat history, no "we decided to ___"
  rationalizations.

Loop rules:
- If the fresh-context re-audit returns new Critical or High findings, fix them and re-invoke the reviewer (fresh context again, fresh inputs each time).
- Stop the per-chunk loop when the fresh-context re-audit returns no Critical or High findings.
- If the loop does not converge after 3 iterations, escalate to the user.

### Phase 4: Cross-cutting review (Architecture reviewer)

After all chunks have been individually audited, run a cross-cutting review covering:
- **Architectural consistency**: do modules respect their boundaries? Are there layering violations?
- **Pattern consistency**: are similar problems solved differently in different places?
- **Error handling patterns**: consistent approach across the codebase?
- **Naming and convention drift**: inconsistencies that accumulated over time?
- **Dependency health**: outdated, duplicated, or unnecessary dependencies?
- **Documentation alignment**: do README, memory files, and inline docs match the actual code?

Record cross-cutting findings in the master report under `## Cross-Cutting Findings`.

Fix accepted cross-cutting findings using the same accepted-finding fix
contract from Phase 3.

### Phase 5: Complementary skill delegation (Orchestrator)

Delegate to complementary skills only when the audit surfaces systemic issues
in their domain: `/boost-coverage` for significant test gaps, `/audit-tests`
for widespread redundancy/misclassification, `/simplify-codebase` for
significant complexity, or `/fix-issues` for overlapping `ISSUES.md` entries:

```text
/fix-issues
review_agents are {review agent 1, review agent 2, ...}
```

Skip delegation when no meaningful gaps exist.

Record delegation outcomes in the master report under `## Complementary Skill Results`.

### Phase 6: Close the run (Reporter)

When all chunks and cross-cutting reviews are complete, add `## Final Summary`
with chunks audited, finding counts by severity, fixes vs. deferrals,
complementary skill outcomes, and overall health. Add `## Residual Risk` for
any systemic concerns that remain.

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

At each major stage, echo the master report path, current chunk when relevant,
and current stage: preflight, survey/decompose, audit/fix chunk N,
cross-cutting review, delegation, or closeout.

## Guardrails

- Do not silently skip chunks that were in the plan.
- Do not expand a chunk review into unrelated areas.
- Do not treat the cross-cutting review as optional.
- Do not claim a clean codebase without evidence from the audit rounds.
- Do not modify unrelated code just because it is nearby.
- Keep each chunk review grounded in concrete reviewed code, /review-uncommitted-code findings, and observed verification.

## Definition of done

- The master report exists at `.agent-layer/tmp/improve-codebase.<run-id>.report.md` with every required section, including one `## Chunk N: <name> Findings` + `## Chunk N: <name> Fixes` pair per chunk in the plan and a populated `## Cross-Cutting Findings` section.
- Every chunk in the planned chunk map was audited (no silent skips); per-chunk re-audit loops used a fresh-context reviewer subagent (not the fixer's context) and ended with no new Critical or High findings or with an explicit 3-iteration escalation recorded.
- Every deferred finding has a matching entry in `ISSUES.md` cited by the master report.
- The `## Final Summary` states overall codebase health with evidence, and `## Residual Risk` names any systemic concerns that remain.

## Final handoff

After the run, present the results to the user in chat:

1. Echo the master report path.
2. State total chunks audited, total findings, and fixes applied.
3. Present a **Key findings and fixes** table sorted by Chunk (primary) then Severity (secondary).
4. Below the table, list deferred findings with their `ISSUES.md` entry references.
5. State the overall codebase health assessment.
6. List any complementary skills that were invoked and their outcomes.
