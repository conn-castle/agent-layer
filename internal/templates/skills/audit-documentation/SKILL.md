---
name: audit-documentation
description: >-
  Audit Markdown docs for static accuracy and cross-document consistency
  against the repo, fixing what is safe. Excludes memory files by default.
---

# audit-documentation

Audit documentation once against current repository evidence, directly fix
safe inaccuracies, and report material gaps. Do not expand into code or policy
changes.

## Scope and inputs

- Use user-supplied Markdown paths, directories, or diff scope when provided.
- Otherwise audit all tracked `*.md` files.
- Exclude ISSUES.md, BACKLOG.md, ROADMAP.md, DECISIONS.md, COMMANDS.md, and
  CONTEXT.md. Use `/audit-memory` when the user requests memory files.
- Accept an optional maximum reported-finding count. It limits report size, not
  the evidence pass or declared scope coverage.
- If no Markdown files are in scope, report `no-findings` and stop.

## Evidence and edit contract

- Validate claims statically against repository files, configuration, symbols,
  scripts, and documented contracts. Do not present an unexecuted command as
  verified runtime behavior.
- Prioritize claims that could materially mislead a developer, operator, or
  future agent: commands, paths, configuration keys, interfaces,
  architecture, and workflow rules.
- Apply the smallest correction that makes generally aligned documentation
  accurate and complete.
- When implemented behavior is clearly authoritative or better than the stale
  documentation, update the documentation.
- When the documentation describes a better intended behavior than the code,
  leave both unchanged and report the code gap.
- When evidence cannot decide product intent, leave the content unchanged and
  ask the user for the smallest decision that resolves it. Additional reviewer
  agreement is not a substitute for evidence.
- Do not edit source code, tests, or memory files in this workflow.

## Workflow

### 1. Establish the audit target

Record the exact files in scope and read enough surrounding repository evidence
to understand their actionable claims.

### 2. Run one documentation audit pass

Audit a compact scope directly. For a context-heavy scope, give coherent,
non-overlapping document groups to fresh read-only investigators and run
independent groups concurrently when useful. Each returns compact candidates
with exact claims and evidence; the owning agent validates them, resolves
cross-document conflicts, and makes all edits. Do not split work merely to add
agents.

In one pass, check:

- referenced commands, paths, configuration keys, and interface names
- architecture and workflow claims against current code and configuration
- contradictions, renamed concepts, and drift across in-scope documents
- templates or examples that disagree with their canonical source

Mark claims that cannot be established statically as unverified instead of
guessing. Ignore wording preferences unless they affect correctness or use.

### 3. Address findings directly

- Fix evidence-backed documentation inaccuracies in place.
- Report code gaps and user-owned intent decisions without editing either side.
- Do not log findings to memory files or launch another audit.

### 4. Report and yield

For each material finding, record:

- title and severity
- type: command | path | config | interface | architecture | cross-doc
- exact file and section
- evidence checked and why it matters
- outcome: `fixed` | `code-gap` | `needs-user-decision`

The final summary contains:

1. `# Documentation Audit Summary` — scope and verdict
2. `## Fixes Applied`
3. `## Code Gaps`
4. `## Decisions Needed`

Use `None` for empty sections. Mention accurate areas only when that context is
useful; do not create a ceremonial strengths inventory.

Return the summary, decisions, and blockers after every in-scope document has
one evidence pass and each material finding has a terminal outcome.
