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
- Accept an optional maximum finding count.
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

For a broad or context-heavy scope, form enough coherent document groups to
give materially different areas independent attention without overloading an
investigator's context. Do not combine groups when that would obscure
cross-document relationships, and do not split them merely to increase agent
count. Give each group to a fresh built-in investigator subagent. Run
substantial independent groups concurrently when the wall-clock benefit
warrants the extra agent cost; otherwise run them sequentially. Investigators
are read-only and return compact candidate findings with exact claims,
repository evidence, and cross-document references. The owning agent validates
candidates, resolves relationships across groups, and owns all edits. A compact
scope may be audited directly.

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

## Definition of done

- Every in-scope document received one purposeful evidence pass.
- Safe corrections were applied once and every remaining finding has concrete
  evidence and a terminal outcome.
- No source code, tests, or memory files were modified.
- The skill returns its summary, decisions, and blockers, then yields.
