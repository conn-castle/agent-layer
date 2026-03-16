---
name: audit-documentation
description: >-
  Audit Markdown documentation for static accuracy and cross-document
  consistency against the repository, fix what can be fixed, and produce a
  report. Excludes agent memory files by default (use `audit-memory` for those).
---

# audit-documentation

Audit documentation and fix inaccuracies. Do not freelance into code or policy changes.

## Defaults

- Default scope is all tracked `*.md` files unless the user gives paths or a diff-based scope.
- Exclude agent memory files (ISSUES.md, BACKLOG.md, ROADMAP.md, DECISIONS.md, COMMANDS.md, CONTEXT.md) from the default scope. Use the `audit-memory` skill for those.
- Validate claims statically against the repository. Do not treat unexecuted commands as verified runtime behavior.
- Prioritize findings that would mislead a developer, operator, or future agent.

## Inputs

Accept any combination of:
- explicit Markdown paths or directories
- a git ref or range for changed-doc scope
- a maximum finding count
- whether to include short excerpts

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
4. `Fixer`: applies mechanical corrections.
5. `Reporter`: writes the report.

## Global constraints

- Keep this workflow static. Do not run product code or infer runtime behavior from docs alone.
- Keep findings tied to concrete evidence: file, section, and check performed.
- Prefer the smallest credible correction over broad documentation rewrites.

## Human checkpoints

- Required: ask before applying corrections that change meaning or interpretation.
- Optional: ask when the requested scope is ambiguous enough that the audit target itself is unclear.
- When a checkpoint involves a genuine tradeoff between substantive alternatives, present at least two options with brief pros and cons, state which you recommend and why, and let the human decide.
- Stay autonomous for mechanical corrections (stale paths, incorrect commands, outdated config references, wrong version numbers). These do not need confirmation.

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

### Phase 4: Fix findings (Fixer)

1. Apply mechanical corrections immediately: update stale paths, fix incorrect commands, correct config references, update version numbers.
2. For corrections that change meaning or interpretation, ask the user before applying.
3. Log findings that cannot be fixed in docs to `ISSUES.md`.

### Phase 5: Write the report (Reporter)

Each finding must include:
- `Title`
- `Severity`: High | Medium | Low
- `Type`: command | path | config | interface | architecture | cross-doc
- `Location`: exact file and section
- `Evidence`
- `Why it matters`
- `What was done`: fixed | logged to ISSUES.md | needs human decision

## Required report structure

The report must contain:

1. `# Documentation Audit Summary`
   - audited scope
   - documents scanned
   - short outcome summary
2. `## Fixes Applied`
   - what was changed and where
3. `## Findings Requiring Human Decision`
   - corrections that change meaning — present options, not just the problem
4. `## Open Questions`
   - only when unresolved ambiguity blocks confidence
5. `## Strengths`
   - concise list of docs that are accurate or well-aligned

## Guardrails

- Do not turn wording preferences into findings unless they materially affect correctness or usability.
- Do not invent policy changes while fixing stale docs.
- Do not widen a doc audit into a code audit.
- If memory file issues are found during the audit, note them and recommend `audit-memory` rather than auditing memory files in this workflow.

## Final handoff

After writing the report:
1. Echo the report path.
2. Summarize fixes applied and any findings that need human decision.
3. If memory file issues were noticed, recommend running `audit-memory`.
