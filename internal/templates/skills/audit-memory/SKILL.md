---
name: audit-memory
description: >-
  Audit agent memory files (ISSUES, BACKLOG, ROADMAP, DECISIONS, COMMANDS,
  CONTEXT) for structure, staleness, placement, consistency, and DECISIONS.md
  bloat; fix accepted findings.
---

# audit-memory

Audit the authoritative memory files once, directly fix evidence-backed
problems, and report only material unresolved findings.

## Scope and inputs

- Default scope is ISSUES.md, BACKLOG.md, ROADMAP.md, DECISIONS.md,
  COMMANDS.md, and CONTEXT.md.
- Accept an explicit subset, audit-only mode, optional repository-documentation
  cross-checks, and a maximum finding count.
- Do not create a missing memory file. Report it and follow repository policy.
- Do not modify source code, tests, or repository documentation.

## Required artifact

Write `.agent-layer/tmp/audit-memory.<run-id>.report.md`, where `run-id` is
`YYYYMMDD-HHMMSS-<short-rand>`.

## Agent boundary

Use one fresh built-in investigator subagent to examine all in-scope memory
files together and return a compact candidate ledger with exact entries,
cross-file conflicts, and repository evidence. Keep the investigation holistic;
do not split individual memory files among agents. The owning agent validates
the candidates, makes every edit, and decides which ambiguity belongs to the
user.

## Evidence and decision contract

- Read each in-scope file's own purpose, format, and insertion-marker rules
  before editing it.
- Validate staleness against current repository evidence. Do not remove or mark
  an entry complete based on inference alone.
- Apply clear format, deduplication, placement, staleness, and supersession
  fixes directly.
- Ask only when repository evidence cannot settle whether information remains
  true or future-guiding, or when an external-state claim cannot be verified.
- Preserve future-guiding constraints and tradeoffs. Historical completeness is
  not a reason to retain an entry.

## Workflow

### 1. Establish current memory state

Give the investigator the in-scope memory files and the minimum repository
questions needed to evaluate them. Record missing files and the DECISIONS.md
entry count before edits, then validate the returned candidate ledger against
the cited evidence before changing a memory file.

### 2. Run one memory audit pass

Check complementary concerns in the same pass:

- required sections, markers, and entry formats
- stale, completed, duplicate, or misplaced ISSUES.md and BACKLOG.md entries
- ROADMAP.md status and references against completed work
- COMMANDS.md commands against current scripts and tooling definitions
- CONTEXT.md facts against current repository state and other memory files
- contradictions and duplication across all in-scope files
- DECISIONS.md entries that are superseded, duplicated, now self-evident, or no
  longer constrain future work

When DECISIONS.md exceeds 25 entries, assess every entry once, but record
individual classifications only for entries that require consolidation,
removal, or a user decision. Group related decisions by subsystem before
editing so a historical chain becomes one current constraint where appropriate.

### 3. Address findings directly

- Fix clear format violations and duplicates.
- Remove entries proven stale or complete.
- Move misplaced entries without duplicating them when both source and
  destination files are in scope; otherwise report the placement finding.
- Consolidate superseded decisions while retaining only future-guiding
  rationale.
- Update roadmap status only when code, docs, or tests provide sufficient
  evidence.
- Leave ambiguous findings unchanged and record the exact user decision needed.

Audit-only mode records the same outcomes without editing.

### 4. Report and yield

The report contains:

1. `# Memory Audit Summary` — files, verdict, and DECISIONS.md count before and
   after
2. `## Fixes Applied` — grouped by file
3. `## Material Findings` — evidence and affected file
4. `## Decisions Needed` — the smallest unresolved questions
5. `## Residual Risk`

Use `None` for empty sections. Do not inventory unaffected entries or add new
memory entries merely to record this audit.

## Definition of done

- Every in-scope memory file received one structural, content, and cross-file
  evidence pass.
- Clear findings were addressed once; ambiguous entries were preserved with an
  explicit decision request.
- DECISIONS.md counts before and after are recorded.
- Only the in-scope memory files and required report were modified.
- The skill returns the report path and terminal outcome, then yields.
