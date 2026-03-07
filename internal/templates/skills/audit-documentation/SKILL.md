---
name: audit-documentation
description: >-
  Audit Markdown documentation for static accuracy and cross-document
  consistency against the repository, then produce a report and optionally
  apply fixes for accepted findings. Excludes agent memory files by default
  (use `audit-memory` for those).
---

# audit-documentation

Run a documentation audit without freelancing into code or policy changes.
Default behavior is report-first:
- no code edits
- no documentation edits
- no automatic issue logging

## Defaults

- Default scope is all tracked `*.md` files unless the user gives paths or a diff-based scope.
- Exclude agent memory files (ISSUES.md, BACKLOG.md, ROADMAP.md, DECISIONS.md, COMMANDS.md, CONTEXT.md) from the default scope. Use the `audit-memory` skill for those.
- Default mode is audit-only. Fix mode is available when requested.
- Validate claims statically against the repository. Do not treat unexecuted commands as verified runtime behavior.
- Prioritize findings that would mislead a developer, operator, or future agent.

## Inputs

Accept any combination of:
- explicit Markdown paths or directories
- a git ref or range for changed-doc scope
- a maximum finding count
- whether to include short excerpts
- whether to prepare fix proposals
- whether to apply fixes for accepted findings (default: report-only)

## Required artifact

Write the audit report to:
- `.agent-layer/tmp/audit-documentation.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create the file with `touch` before writing.

## Multi-agent pattern

Use subagents liberally when available.

Recommended roles:
1. `Doc inventory`: selects Markdown files in scope.
2. `Claim validator`: extracts commands, paths, config keys, and architecture claims.
3. `Consistency reviewer`: checks contradictions and drift across documents.
4. `Reporter`: writes the report and disposition guidance.

## Global constraints

- Keep this workflow static. Do not run product code or infer runtime behavior from docs alone.
- Do not edit docs or memory files during the audit pass.
- Keep findings tied to concrete evidence: file, section, and check performed.
- Prefer the smallest credible correction over broad documentation rewrites.

## Human checkpoints

- Optional: ask only when the requested scope is ambiguous enough that the audit target itself is unclear.
- Stay autonomous during the audit itself. Do not interrupt just to share intermediate findings.

## Audit workflow

### Phase 0: Preflight (Doc inventory)

1. Determine the document scope:
   - explicit user paths first
   - otherwise tracked `*.md` files
   - otherwise changed Markdown files when the user asked for diff-based scope
2. Record the actual scope that will be audited.
3. If no Markdown files are in scope, stop and report that explicitly.

### Phase 1: Extract claims (Claim validator)

From each document in scope, extract only actionable claims such as:
- runnable commands
- file and directory paths
- environment variables and config keys
- API, CLI, or interface names
- architecture or workflow rules

### Phase 2: Validate claims against the repo (Claim validator)

Use static checks only:
- file existence
- command definition presence in repo docs/tooling
- targeted symbol or term searches
- current memory file formats and markers

If a claim cannot be validated statically, mark that limitation explicitly instead of guessing.

### Phase 3: Check cross-document consistency (Consistency reviewer)

Look for:
- contradictory commands or workflows
- renamed files or paths that drifted
- docs that conflict with project rules or memory formats
- template docs that no longer match canonical docs

### Phase 4: Write the report (Reporter)

Each finding must include:
- `Title`
- `Severity`: High | Medium | Low
- `Type`: command | path | config | interface | architecture | cross-doc
- `Location`: exact file and section
- `Evidence`
- `Why it matters`
- `Recommendation`
- `Suggested disposition`: fix-docs | log-issue | both | ignore

## Required report structure

The report must contain:

1. `# Documentation Audit Summary`
   - audited scope
   - documents scanned
   - short outcome summary
2. `## Findings`
   - ordered by severity
3. `## Open Questions`
   - only when unresolved ambiguity blocks confidence
4. `## Strengths`
   - concise list of docs that are accurate or well-aligned
5. `## Recommended Disposition`
   - say which findings are best fixed in docs, logged as issues, or both

## Guardrails

- Do not turn wording preferences into findings unless they materially affect correctness or usability.
- Do not invent policy changes while “fixing” stale docs.
- Do not widen a doc audit into a code audit.
- Do not apply fixes when running in report-only mode (the default).
- Do not modify files after the report unless fix mode was requested or the user directs which findings to act on.
- If memory file issues are found during the audit, note them and recommend `audit-memory` rather than auditing memory files in this workflow.

## Fix mode

When fix mode is requested (via inputs or user direction after the report):

1. Apply clear mechanical corrections: update stale paths, fix incorrect commands, correct config references, update version numbers.
2. Ask before applying corrections that change meaning or interpretation.
3. Record each fix in the report under a `## Fixes Applied` section.

When fix mode is not requested, skip fixes and present recommendations in the report.

## Final handoff

After writing the report:
1. Echo the report path.
2. Summarize the highest-value findings in chat.
3. If fixes were applied, summarize what was changed.
4. Tell the user to choose which remaining findings to fix, log, do both, or ignore.
5. If memory file issues were noticed, recommend running `audit-memory`.
