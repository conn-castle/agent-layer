---
name: audit-memory
description: >-
  Audit the agent memory files (ISSUES, BACKLOG, ROADMAP, DECISIONS, COMMANDS,
  CONTEXT) for structural compliance, staleness, misplacement, cross-file
  consistency, and DECISIONS.md bloat, then fix accepted findings. Use
  `audit-documentation` for repo documentation accuracy.
---

# audit-memory

Audit and fix the agent memory files. Default behavior is audit-and-fix:
- audit all 6 memory files for structural and content health
- fix mechanical issues (stale entries, format violations, misplaced items, superseded decision chains)
- ask before judgment calls (is this issue actually fixed? is this decision still relevant?)

## Defaults

- Default scope is all 6 memory files: ISSUES.md, BACKLOG.md, ROADMAP.md, DECISIONS.md, COMMANDS.md, CONTEXT.md.
- Default mode is audit-and-fix for mechanical corrections.
- Validate content against the repository (code, tests, docs) to detect staleness and drift.
- DECISIONS.md receives focused scrutiny for bloat and consolidation opportunities.
- Do not audit repo documentation (README, docs/, etc.) — use `audit-documentation` for that.

## Inputs

Accept any combination of:
- explicit memory file names to audit (subset of the 6)
- whether to run audit-only (no fixes)
- whether to include repo documentation cross-checks
- a maximum finding count

## Required artifact

Write the audit report to:
- `.agent-layer/tmp/audit-memory.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create the file with `touch` before writing.

## Multi-agent pattern

Use subagents liberally when available.

Recommended roles:
1. `Structure auditor`: checks format compliance, markers, and entry templates for each file.
2. `Content auditor`: checks staleness, misplacement, and deduplication within each file.
3. `Decisions auditor`: focused audit of DECISIONS.md for bloat, superseded chains, and entries now obvious from code.
4. `Cross-file auditor`: checks consistency across the 6 memory files and between memory files and repo state.
5. `Fixer`: applies accepted mechanical corrections.
6. `Reporter`: writes the final report.

## Global constraints

- Validate claims against the actual repository (search code, check file existence, verify commands).
- Do not guess whether an issue is fixed — verify against the code or ask.
- Do not remove entries without evidence (code search, file existence check, or user confirmation).
- Do not modify repo documentation, source code, or test files in this workflow.
- Prefer consolidating entries over removing them when the underlying information is still valuable.
- Treat DECISIONS.md consolidation as a first-class audit task, not an afterthought.

## Human checkpoints

- Required: ask when staleness evidence is ambiguous (e.g., an issue might be partially fixed).
- Required: ask when a DECISIONS.md entry removal or consolidation would lose unique tradeoff information.
- Required: ask when a memory file entry references external state that cannot be verified from the repository.
- Stay autonomous for clear mechanical fixes: format corrections, obvious duplicates, entries that are definitively stale by code evidence, and straightforward misplacements.

## Audit workflow

### Phase 0: Preflight

1. Read all 6 memory files. Record which exist and which are absent.
2. Read `README.md` for project context.
3. If no memory files exist, stop and report that explicitly.

### Phase 1: Structural audit (Structure auditor)

For each memory file, check:
- Presence of the expected sections and markers (`<!-- ENTRIES START -->`, `<!-- PHASES START -->`)
- Entry format compliance against the file's documented template
- Consistent indentation and spacing between entries
- Proper use of entry IDs (date format, short identifier)

### Phase 2: Content audit per file (Content auditor)

**ISSUES.md:**
- For each issue, search the codebase to determine if it has been fixed
- Check whether any entry is actually a feature request (belongs in BACKLOG.md)
- Check for near-duplicate entries

**BACKLOG.md:**
- Check whether any item has already been implemented (search code, check for related changes)
- Check whether any item has been scheduled into ROADMAP.md (should be removed from BACKLOG.md)
- Check whether any entry is actually a bug or tech debt (belongs in ISSUES.md)
- Check for near-duplicate entries

**ROADMAP.md:**
- Check whether completed phases are properly marked with the completed format
- Check whether tasks in incomplete phases have actually been completed
- Check for orphaned references to issues or backlog items that no longer exist

**DECISIONS.md** (receives focused scrutiny):
- Count total entries and flag when the log is growing large (approaching ~25 entries is a signal to investigate, not a hard limit)
- Identify superseded chains: decisions that were later changed by newer decisions (these should be consolidated to the final decision)
- Identify entries that are now self-evident from the codebase (the decision is embodied in code, tests, or docs in a way that makes the log entry redundant)
- Identify entries that duplicate information already in CONTEXT.md, COMMANDS.md, or README.md
- Identify entries that record routine best-practice adherence rather than non-obvious tradeoffs
- For each flagged entry, recommend: consolidate (fold into a newer entry), remove (self-evident or duplicated), or keep (unique tradeoff information)

**COMMANDS.md:**
- Verify each command is still valid (check referenced files, scripts, and tool availability)
- Check for duplicate or near-duplicate entries
- Check for commands that reference removed files or scripts

**CONTEXT.md:**
- Check for facts that are outdated or contradicted by current code
- Check for information that duplicates other memory files
- Check for entries that belong in a more specific memory file

### Phase 3: Cross-file consistency (Cross-file auditor)

Check for:
- Issues referenced in ROADMAP.md that don't exist in ISSUES.md
- Backlog items scheduled in ROADMAP.md but still present in BACKLOG.md
- Decisions that contradict current roadmap direction or completed work
- Commands that reference workflows no longer described in documentation
- CONTEXT.md entries that duplicate or contradict DECISIONS.md

### Phase 4: Fix accepted findings (Fixer)

For findings with clear mechanical fixes:
1. Remove definitively stale entries (backed by code evidence)
2. Move misplaced entries to the correct file (features from ISSUES.md to BACKLOG.md and vice versa)
3. Fix format violations (indentation, spacing, missing fields)
4. Consolidate superseded DECISIONS.md chains: keep the final decision, fold valuable tradeoff context from earlier entries into it, remove the superseded entries
5. Remove obvious duplicates (keep the more complete version)
6. Update phase/task status in ROADMAP.md when evidence is clear
7. Remove BACKLOG.md entries that are already scheduled in ROADMAP.md

For findings requiring judgment:
- Present the finding and evidence at a human checkpoint
- Record the question and defer if the user is unavailable

When running in audit-only mode, skip all fixes and report recommendations instead.

### Phase 5: Write the report (Reporter)

## Required report structure

Write `.agent-layer/tmp/audit-memory.<run-id>.report.md` with:

1. `# Memory Audit Summary`
   - files audited
   - short outcome summary
   - DECISIONS.md entry count (before and after if fixes were applied)
2. `## Structural Findings`
3. `## Content Findings`
   - organized by file
   - DECISIONS.md findings in a dedicated subsection
4. `## Cross-File Findings`
5. `## Fixes Applied`
   - what was changed and why
6. `## Deferred Findings`
   - findings that required human judgment and were skipped
7. `## Recommendations`
   - remaining actions the user should consider

## Guardrails

- Do not turn memory file cleanup into a policy change.
- Do not remove DECISIONS.md entries that still contain unique tradeoff information, even if the decision itself is now embodied in code.
- Do not widen the audit into a code audit or documentation audit (point to `audit-documentation` for repo docs).
- Do not add new memory entries during the audit (that would conflict with the audit's own findings).
- Do not consolidate DECISIONS.md entries in a way that loses the reason or tradeoff information.
- Do not modify files outside of the 6 memory files.

## Final handoff

After writing the report:
1. Echo the report path.
2. Summarize the highest-value findings, especially DECISIONS.md bloat status.
3. State what was fixed, what was deferred, and what needs user input.
4. If repo documentation issues were noticed during the audit, recommend running `audit-documentation`.
