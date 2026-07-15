---
name: audit-memory
description: >-
  Explicit-only.
  Audit agent memory files (ISSUES, BACKLOG, ROADMAP, DECISIONS, COMMANDS,
  CONTEXT) for structure, staleness, placement, consistency, and DECISIONS.md
  bloat; fix accepted findings.
---

# audit-memory

Audit memory files and fix evidence-backed problems.

## Scope

- Default scope is ISSUES.md, BACKLOG.md, ROADMAP.md, DECISIONS.md,
  COMMANDS.md, and CONTEXT.md.
- Accept a subset, audit-only mode, documentation cross-checks, and a report
  limit that does not reduce coverage.
- Do not create a missing memory file. Report it and follow repository policy.
- Do not modify source code, tests, or repository documentation.

Write `.agent-layer/tmp/audit-memory.<run-id>.report.md`, using
`YYYYMMDD-HHMMSS-<short-rand>` for `run-id`.

## Contract

- Read each file's purpose, format, and insertion-marker rules before editing.
- Validate staleness against current evidence; do not remove or complete entries
  by inference.
- Fix clear format, duplication, placement, staleness, and supersession issues.
- Preserve uncertain entries and name missing evidence or decisions without
  blocking independent work.
- Preserve future-guiding constraints and tradeoffs. Historical completeness is
  not a reason to retain an entry.

## Workflow

Inspect scope as one coherent set, optionally with a read-only investigator. The
owning agent validates evidence and edits. Check:

- required sections, markers, and entry formats
- stale, completed, duplicate, or misplaced ISSUES.md and BACKLOG.md entries
- ROADMAP.md status/references and COMMANDS.md commands against current evidence
- CONTEXT.md facts and cross-file contradictions or duplication
- DECISIONS.md entries that are superseded, duplicated, now self-evident, or no
  longer constrain future work

Consolidate superseded decision chains while retaining future-guiding rationale.
Remove proven stale/completed entries, move entries only when both files are in
scope, and update roadmap status only with sufficient evidence. Audit-only mode
records the same outcomes without edits.

The report contains:

1. `# Memory Audit Summary` — files and verdict
2. `## Fixes Applied` — grouped by file
3. `## Material Findings` — evidence and affected file
4. `## Decisions Needed` — the smallest unresolved questions
5. `## Residual Risk`

Use `None` for empty sections. Do not inventory unaffected entries or log the
audit in memory. Finish after structural, content, and cross-file coverage, with
every finding fixed, preserved with a reason, or recorded in audit-only mode.
