---
name: audit-documentation
description: >-
  Audit Markdown docs for static accuracy and cross-document consistency
  against the repo, fixing what is safe. Excludes memory files by default; use
  `audit-memory` for those.
---

# audit-documentation

Audit documentation and fix inaccuracies. Do not freelance into code or policy changes.

## Defaults

- Default scope is all tracked `*.md` files unless the user gives paths or a diff-based scope.
- Exclude agent memory files (ISSUES.md, BACKLOG.md, ROADMAP.md, DECISIONS.md, COMMANDS.md, CONTEXT.md) from the default scope. Use the `audit-memory` skill for those.
- Validate claims statically against the repository. Do not treat unexecuted commands as verified runtime behavior.
- When docs and code differ, decide which side describes the better behavior based on repo evidence, project rules, safety, and maintainability.
- Prioritize findings that would mislead a developer, operator, or future agent.

## Inputs

Accept any combination of:
- explicit Markdown paths or directories
- a git ref or range for changed-doc scope
- a maximum finding count
- whether to include short excerpts

## Multi-agent pattern

Recommended roles:
1. `Doc inventory`: selects Markdown files in scope.
2. `Claim validator`: extracts commands, paths, config keys, and architecture claims.
3. `Consistency reviewer`: checks contradictions and drift across documents.
4. `Fixer`: applies mechanical corrections.
5. `Reporter`: summarizes findings and fixes.

## Global constraints

- Keep this workflow static. Do not run product code or infer runtime behavior from docs alone.
- Keep findings tied to concrete evidence: file, section, and check performed.
- Prefer the smallest credible correction over broad documentation rewrites.
- Do not edit code in this workflow. When code should change to match better documented behavior, surface that as a human decision.

## Drift resolution policy

When docs and code differ:
- If docs and code are generally aligned but the docs are incomplete, outdated, or imprecise, update the docs in place to accurately describe the implemented behavior.
- If the implemented behavior is better than the documented behavior, update the docs in place without asking.
- If the documented behavior is better than the implemented behavior, leave docs and code unchanged and surface the code gap to the user.
- If evidence does not clearly show whether docs or code should win, use a subagent to adjudicate. If it cannot, leave the content unchanged and ask for a decision.

## Human checkpoints

- Required: ask before choosing between plausible product behaviors or when code should change to match better docs.
- Optional: ask when the requested scope is ambiguous enough that the audit target itself is unclear.
- When a checkpoint involves a genuine tradeoff between substantive alternatives, present at least two options with brief pros and cons, state which you recommend and why, and let the human decide.
- Stay autonomous for doc corrections that make docs accurate and complete, including stale paths, incorrect commands, outdated config references, wrong version numbers, missing implementation details, and implementation claims contradicted by the code.

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

1. Apply doc corrections immediately when docs are incomplete, outdated, imprecise, or when the implemented behavior is better than the documented behavior.
2. Surface cases where docs describe better behavior than the code implements; do not rewrite those docs to match weaker code.
3. Ask before changing docs when the evidence is ambiguous or the correction would choose product intent.
4. Report findings that cannot be safely fixed in docs; do not log them to memory files in this workflow.

### Phase 5: Summarize findings and fixes (Reporter)

Each finding must include:
- `Title`
- `Severity`: High | Medium | Low
- `Type`: command | path | config | interface | architecture | cross-doc
- `Location`: exact file and section
- `Evidence`
- `Why it matters`
- `What was done`: fixed | logged to ISSUES.md | needs human decision

## Final summary structure

The final summary must contain:

1. `# Documentation Audit Summary`
   - audited scope
   - documents scanned
   - short outcome summary
2. `## Fixes Applied`
   - what was changed and where
3. `## Findings Requiring Human Decision`
   - ambiguous corrections and cases where code should change to match better docs — present options, not just the problem
4. `## Open Questions`
   - only when unresolved ambiguity blocks confidence
5. `## Strengths`
   - concise list of docs that are accurate or well-aligned

## Guardrails

- Do not turn wording preferences into findings unless they materially affect correctness or usability.
- Do not invent policy changes while fixing stale docs.
- Do not widen a doc audit into a code audit.
- If memory file issues are found during the audit, note them and recommend `audit-memory` rather than auditing memory files in this workflow.

## Definition of done

- The final summary includes the required sections (`Summary`, `Fixes Applied`, `Findings Requiring Human Decision`, and `Strengths`).
- Every finding in the final summary names its file, section, check performed, and `What was done` verdict (fixed / logged to ISSUES.md / needs human decision).
- Mechanical corrections are applied in-place; no meaning-changing edits were made without a human checkpoint.
- Incomplete, outdated, or imprecise docs are updated in-place when they are generally aligned with the implementation.
- Docs-code drift is fixed in-place when the implemented behavior is better than the documented behavior.
- Cases where docs describe better behavior than the code are surfaced to the user without editing docs or code.
- Memory files (ISSUES, BACKLOG, ROADMAP, DECISIONS, COMMANDS, CONTEXT) were not modified by this run.

## Final handoff

After the audit:
1. Summarize fixes applied and any findings that need human decision.
2. If memory file issues were noticed, recommend running `audit-memory`.
