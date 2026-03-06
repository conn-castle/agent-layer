---
name: audit-documentation
description: >-
  Audit Markdown documentation for static accuracy and cross-document
  consistency against the repository, then produce a report and use
  judgment-based human checkpoints before any non-routine doc edits or issue
  logging.
---

# audit-documentation

Run a documentation audit without freelancing into code or policy changes.
Default behavior is report-first:
- no code edits
- no documentation edits
- no automatic issue logging

## Defaults

- Default scope is all tracked `*.md` files unless the user gives paths or a diff-based scope.
- Default mode is audit-only.
- Validate claims statically against the repository. Do not treat unexecuted commands as verified runtime behavior.
- Prioritize findings that would mislead a developer, operator, or future agent.

## Inputs

Accept any combination of:
- explicit Markdown paths or directories
- a git ref or range for changed-doc scope
- a maximum finding count
- whether to include short excerpts
- whether to prepare fix proposals

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
- Do not modify files after the report unless the request already includes applying clear mechanical fixes or the user directs which findings to act on.

## Final handoff

After writing the report:
1. Echo the report path.
2. Summarize the highest-value findings in chat.
3. Tell the user to choose which findings to fix, log, do both, or ignore.
