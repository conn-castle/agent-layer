---
name: audit-documentation
description: >-
  Audit Markdown docs for static accuracy and cross-document consistency
  against the repo, fixing what is safe. Excludes memory files by default.
---

# audit-documentation

Audit Markdown against repository evidence; fix safe inaccuracies.

## Scope

- Use supplied scope; otherwise audit all tracked `*.md` files.
- Exclude ISSUES.md, BACKLOG.md, ROADMAP.md, DECISIONS.md, COMMANDS.md, and
  CONTEXT.md. Use `/audit-memory` when the user requests memory files.
- A finding limit caps reporting, not coverage. Empty scope returns
  `no-findings`.

## Contract

- Validate actionable claims against files, configuration, symbols, scripts,
  and contracts. An unexecuted command is not verified behavior.
- Fix the smallest evidence-backed error that could materially mislead. If docs
  express better intended behavior than code, report a code gap. Preserve
  ambiguous content and name the missing decision.
- Do not edit source, tests, memory files, or product policy.

## Workflow

Record scope and inspect enough context to establish its claims. For large
scopes, parallelize non-overlapping read-only investigations; the owning agent
validates candidates, resolves conflicts, and edits.

Check:

- referenced commands, paths, configuration keys, and interface names
- architecture and workflow claims against current code and configuration
- contradictions, renamed concepts, and drift across in-scope documents
- templates or examples that disagree with their canonical source

Mark statically indeterminate claims unverified. Ignore style, fix supported
inaccuracies, and leave code gaps or unresolved intent unchanged. Do not log the
audit to memory.

Write a concise summary with:

1. `# Documentation Audit Summary` — scope and verdict
2. `## Fixes Applied`
3. `## Code Gaps`
4. `## Decisions Needed`

Findings include severity, location, evidence, impact, outcome (`fixed`,
`code-gap`, or `needs-user-decision`), and type (`command`, `path`, `config`,
`interface`, `architecture`, or `cross-doc`). Use `None` for empty sections.
Finish when every document is checked and every material finding has an outcome.
